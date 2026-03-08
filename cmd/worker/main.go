package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/elcinzorlu/notification-system/internal/config"
	"github.com/elcinzorlu/notification-system/internal/metrics"
	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/elcinzorlu/notification-system/internal/notification/queue"
	"github.com/elcinzorlu/notification-system/internal/notification/repository"
	"github.com/elcinzorlu/notification-system/internal/notification/service"
	"github.com/elcinzorlu/notification-system/internal/provider"
	"github.com/elcinzorlu/notification-system/internal/provider/email"
	"github.com/elcinzorlu/notification-system/internal/provider/push"
	"github.com/elcinzorlu/notification-system/internal/provider/sms"
	ratelimiter "github.com/elcinzorlu/notification-system/internal/rate_limiter"
	ws "github.com/elcinzorlu/notification-system/internal/websocket"
	"github.com/elcinzorlu/notification-system/pkg/database"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func main() {
	// --- Logger ---
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// --- Config ---
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	// --- PostgreSQL ---
	db, err := database.Connect(cfg.DB, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	// --- Redis ---
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	logger.Info("Connected to Redis", zap.String("addr", cfg.Redis.Addr()))

	// --- RabbitMQ ---
	rabbitConn, err := amqp.Dial(cfg.RabbitMQ.URL())
	if err != nil {
		logger.Fatal("Failed to connect to RabbitMQ", zap.Error(err))
	}
	defer rabbitConn.Close()
	logger.Info("Connected to RabbitMQ")

	// --- Queue ---
	q, err := queue.New(rabbitConn, redisClient, logger)
	if err != nil {
		logger.Fatal("Failed to setup queue", zap.Error(err))
	}
	defer q.Close()

	// --- Dependencies ---
	repo := repository.New(db)
	rl := ratelimiter.New(redisClient, cfg.Worker.RateLimitPerSec)
	m := metrics.New(redisClient)
	m.SyncFromDB(db, logger)

	// Providers
	providers := map[model.Channel]provider.Provider{
		model.ChannelSMS:   sms.New(cfg.Webhook.URL, logger),
		model.ChannelEmail: email.New(cfg.Webhook.URL, logger),
		model.ChannelPush:  push.New(cfg.Webhook.URL, logger),
	}

	// WebSocket hub (nil for broadcast in worker-only mode)
	hub := ws.NewHub(logger)
	go hub.Run()

	// Service
	svc := service.New(repo, q, rl, providers, m, hub, logger, cfg.Worker.MaxRetries)

	// --- Graceful Shutdown ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	// --- Start Workers ---
	concurrency := cfg.Worker.Concurrency
	logger.Info("Starting worker pool",
		zap.Int("concurrency", concurrency),
		zap.Int("max_retries", cfg.Worker.MaxRetries),
		zap.Int("rate_limit_per_sec", cfg.Worker.RateLimitPerSec),
	)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerLogger := logger.With(zap.Int("worker_id", workerID))
			workerLogger.Info("Worker started")

			err := q.Consume(ctx, func(handlerCtx context.Context, msg queue.QueueMessage, delivery amqp.Delivery) error {
				workerLogger.Info("Processing notification",
					zap.String("notification_id", msg.NotificationID.String()),
					zap.String("correlation_id", msg.CorrelationID),
					zap.Int("attempt", msg.Attempt),
				)

				processErr := svc.Process(handlerCtx, msg.NotificationID, msg.CorrelationID, msg.Attempt)
				if processErr != nil {
					workerLogger.Error("Processing failed",
						zap.String("notification_id", msg.NotificationID.String()),
						zap.Error(processErr),
					)
					// Ack even on processing failure (retry is handled by the service via re-enqueue)
				}

				return delivery.Ack(false)
			})

			if err != nil && ctx.Err() == nil {
				workerLogger.Error("Consumer exited with error", zap.Error(err))
			}
			workerLogger.Info("Worker stopped")
		}(i)
	}

	// --- Scheduled Notification Promoter ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		logger.Info("Scheduled notification promoter started")
		for {
			select {
			case <-ctx.Done():
				logger.Info("Scheduled notification promoter stopping")
				return
			case <-ticker.C:
				promoted, err := q.PromoteScheduled(ctx)
				if err != nil {
					logger.Error("Failed to promote scheduled notifications", zap.Error(err))
				}
				if promoted > 0 {
					logger.Info("Promoted scheduled notifications", zap.Int("count", promoted))
				}
			}
		}
	}()

	// --- Wait for shutdown signal ---
	sig := <-sigChan
	logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Cancel context to stop consumers
	cancel()

	// Wait for all workers to finish in-flight messages
	logger.Info("Waiting for workers to finish in-flight messages...")
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All workers stopped gracefully")
	case <-time.After(30 * time.Second):
		logger.Warn("Shutdown timed out, forcing exit")
	}

	logger.Info("Worker shutdown complete")
}
