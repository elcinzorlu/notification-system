package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	ExchangeName   = "notifications"
	MainQueue      = "notifications.main"
	RetryQueue     = "notifications.retry"
	DLQQueue       = "notifications.dlq"
	RoutingMain    = "notification.main"
	RoutingRetry   = "notification.retry"
	RoutingDLQ     = "notification.dlq"
	ScheduledSetKey = "notifications:scheduled"
	MaxPriority    = 10
)

// QueueMessage represents a message in the queue.
type QueueMessage struct {
	NotificationID uuid.UUID `json:"notification_id"`
	CorrelationID  string    `json:"correlation_id"`
	Attempt        int       `json:"attempt"`
}

// Queue handles RabbitMQ message publishing and consuming.
type Queue struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	redis   *redis.Client
	logger  *zap.Logger
	mu      sync.RWMutex
}

// New creates a new Queue with RabbitMQ topology.
func New(conn *amqp.Connection, redisClient *redis.Client, logger *zap.Logger) (*Queue, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	q := &Queue{
		conn:    conn,
		channel: ch,
		redis:   redisClient,
		logger:  logger,
	}

	if err := q.setupTopology(); err != nil {
		return nil, fmt.Errorf("failed to setup topology: %w", err)
	}

	return q, nil
}

func (q *Queue) setupTopology() error {
	// Declare the direct exchange
	if err := q.channel.ExchangeDeclare(
		ExchangeName,
		"direct",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	// Main queue with priority support and dead-letter config
	mainArgs := amqp.Table{
		"x-max-priority":            int32(MaxPriority),
		"x-dead-letter-exchange":    ExchangeName,
		"x-dead-letter-routing-key": RoutingDLQ,
	}
	if _, err := q.channel.QueueDeclare(MainQueue, true, false, false, false, mainArgs); err != nil {
		return fmt.Errorf("failed to declare main queue: %w", err)
	}
	if err := q.channel.QueueBind(MainQueue, RoutingMain, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind main queue: %w", err)
	}

	// Retry queue with TTL and re-routing to main
	retryArgs := amqp.Table{
		"x-dead-letter-exchange":    ExchangeName,
		"x-dead-letter-routing-key": RoutingMain,
		"x-message-ttl":             int32(5000), // 5 second base delay, actual delay via per-message TTL
	}
	if _, err := q.channel.QueueDeclare(RetryQueue, true, false, false, false, retryArgs); err != nil {
		return fmt.Errorf("failed to declare retry queue: %w", err)
	}
	if err := q.channel.QueueBind(RetryQueue, RoutingRetry, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind retry queue: %w", err)
	}

	// Dead-letter queue (terminal)
	if _, err := q.channel.QueueDeclare(DLQQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare DLQ: %w", err)
	}
	if err := q.channel.QueueBind(DLQQueue, RoutingDLQ, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind DLQ: %w", err)
	}

	// Set prefetch count
	if err := q.channel.Qos(10, 0, false); err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	return nil
}

// Enqueue publishes a notification to the main queue.
func (q *Queue) Enqueue(ctx context.Context, notificationID uuid.UUID, priority model.Priority, correlationID string) error {
	msg := QueueMessage{
		NotificationID: notificationID,
		CorrelationID:  correlationID,
		Attempt:        0,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.channel.PublishWithContext(ctx,
		ExchangeName,
		RoutingMain,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			DeliveryMode:  amqp.Persistent,
			Priority:      priority.RabbitMQPriority(),
			Body:          body,
			MessageId:     notificationID.String(),
			CorrelationId: correlationID,
			Headers: amqp.Table{
				"correlation_id":  correlationID,
				"notification_id": notificationID.String(),
			},
		},
	)
}

// EnqueueRetry publishes a notification to the retry queue with exponential backoff delay.
func (q *Queue) EnqueueRetry(ctx context.Context, notificationID uuid.UUID, priority model.Priority, correlationID string, attempt int) error {
	msg := QueueMessage{
		NotificationID: notificationID,
		CorrelationID:  correlationID,
		Attempt:        attempt,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal retry message: %w", err)
	}

	// Exponential backoff: 1s, 2s, 4s, 8s, 16s
	delay := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
	expiration := fmt.Sprintf("%d", delay.Milliseconds())

	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.channel.PublishWithContext(ctx,
		ExchangeName,
		RoutingRetry,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			DeliveryMode:  amqp.Persistent,
			Priority:      priority.RabbitMQPriority(),
			Body:          body,
			Expiration:    expiration,
			MessageId:     notificationID.String(),
			CorrelationId: correlationID,
			Headers: amqp.Table{
				"correlation_id":  correlationID,
				"notification_id": notificationID.String(),
				"attempt":         int32(attempt),
			},
		},
	)
}

// EnqueueDLQ publishes a notification to the dead-letter queue.
func (q *Queue) EnqueueDLQ(ctx context.Context, notificationID uuid.UUID, correlationID string, errorMsg string) error {
	msg := QueueMessage{
		NotificationID: notificationID,
		CorrelationID:  correlationID,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ message: %w", err)
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.channel.PublishWithContext(ctx,
		ExchangeName,
		RoutingDLQ,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			DeliveryMode:  amqp.Persistent,
			Body:          body,
			MessageId:     notificationID.String(),
			CorrelationId: correlationID,
			Headers: amqp.Table{
				"correlation_id": correlationID,
				"error":          errorMsg,
			},
		},
	)
}

// Consume starts consuming messages from the main queue and calls the handler.
func (q *Queue) Consume(ctx context.Context, handler func(ctx context.Context, msg QueueMessage, delivery amqp.Delivery) error) error {
	q.mu.RLock()
	deliveries, err := q.channel.Consume(
		MainQueue,
		"",    // consumer tag (auto-generated)
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	q.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to start consuming: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			q.logger.Info("Consumer context cancelled, stopping consumption")
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				q.logger.Warn("Delivery channel closed")
				return fmt.Errorf("delivery channel closed")
			}

			var msg QueueMessage
			if err := json.Unmarshal(delivery.Body, &msg); err != nil {
				q.logger.Error("Failed to unmarshal message", zap.Error(err))
				_ = delivery.Nack(false, false)
				continue
			}

			if err := handler(ctx, msg, delivery); err != nil {
				q.logger.Error("Handler error",
					zap.String("notification_id", msg.NotificationID.String()),
					zap.String("correlation_id", msg.CorrelationID),
					zap.Error(err),
				)
			}
		}
	}
}

// ScheduleNotification adds a notification to the scheduled set in Redis.
func (q *Queue) ScheduleNotification(ctx context.Context, notificationID uuid.UUID, priority model.Priority, correlationID string, scheduledAt time.Time) error {
	msg := QueueMessage{
		NotificationID: notificationID,
		CorrelationID:  correlationID,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// Store priority alongside the message
	member := fmt.Sprintf("%s|%s", string(priority), string(data))
	return q.redis.ZAdd(ctx, ScheduledSetKey, redis.Z{
		Score:  float64(scheduledAt.Unix()),
		Member: member,
	}).Err()
}

// PromoteScheduled moves due scheduled notifications to the main queue.
func (q *Queue) PromoteScheduled(ctx context.Context) (int, error) {
	now := float64(time.Now().Unix())

	results, err := q.redis.ZRangeByScore(ctx, ScheduledSetKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil {
		return 0, err
	}

	promoted := 0
	for _, member := range results {
		// Parse priority|json
		var priority model.Priority
		var msgJSON string
		for i, c := range member {
			if c == '|' {
				priority = model.Priority(member[:i])
				msgJSON = member[i+1:]
				break
			}
		}

		var msg QueueMessage
		if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
			q.logger.Error("Failed to parse scheduled message", zap.Error(err))
			continue
		}

		if err := q.Enqueue(ctx, msg.NotificationID, priority, msg.CorrelationID); err != nil {
			q.logger.Error("Failed to enqueue scheduled notification",
				zap.String("notification_id", msg.NotificationID.String()),
				zap.Error(err),
			)
			continue
		}

		// Remove from scheduled set
		q.redis.ZRem(ctx, ScheduledSetKey, member)
		promoted++
	}

	return promoted, nil
}

// QueueDepth returns the message count for each queue.
func (q *Queue) QueueDepth() (map[string]int64, error) {
	depths := make(map[string]int64)

	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, queueName := range []string{MainQueue, RetryQueue, DLQQueue} {
		qi, err := q.channel.QueueDeclarePassive(queueName, true, false, false, false, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect queue %s: %w", queueName, err)
		}
		depths[queueName] = int64(qi.Messages)
	}

	return depths, nil
}

// Close closes the channel and connection.
func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.channel != nil {
		if err := q.channel.Close(); err != nil {
			return err
		}
	}
	return nil
}
