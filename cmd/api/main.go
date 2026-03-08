package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elcinzorlu/notification-system/internal/config"
	"github.com/elcinzorlu/notification-system/internal/metrics"
	"github.com/elcinzorlu/notification-system/internal/notification/handler"
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
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// @title Notification System API
// @version 1.0
// @description Event-driven notification system for SMS, Email, and Push channels
// @BasePath /api/v1
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

	// Auto-migrate
	if err := db.AutoMigrate(&model.Notification{}, &model.Template{}); err != nil {
		logger.Fatal("Failed to auto-migrate", zap.Error(err))
	}
	logger.Info("Database migration completed")

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

	// WebSocket Hub
	hub := ws.NewHub(logger)
	go hub.Run()

	// Service
	svc := service.New(repo, q, rl, providers, m, hub, logger, cfg.Worker.MaxRetries)

	// Handler
	h := handler.New(svc, hub, logger, db, redisClient, rabbitConn)

	// --- Fiber App ---
	app := fiber.New(fiber.Config{
		AppName:      "Notification System API",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})

	app.Use(recover.New())
	app.Use(cors.New())

	h.RegisterRoutes(app)

	// --- Graceful Shutdown ---
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logger.Info("Shutting down API server...")
		if err := app.Shutdown(); err != nil {
			logger.Error("Server shutdown error", zap.Error(err))
		}
	}()

	// --- Start ---
	addr := fmt.Sprintf(":%s", cfg.App.Port)
	logger.Info("Starting API server", zap.String("addr", addr))
	if err := app.Listen(addr); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}
