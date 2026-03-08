package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	"github.com/elcinzorlu/notification-system/internal/metrics"
	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/elcinzorlu/notification-system/internal/notification/queue"
	"github.com/elcinzorlu/notification-system/internal/notification/repository"
	"github.com/elcinzorlu/notification-system/internal/provider"
	ratelimiter "github.com/elcinzorlu/notification-system/internal/rate_limiter"
	"github.com/elcinzorlu/notification-system/internal/retry"
	ws "github.com/elcinzorlu/notification-system/internal/websocket"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service orchestrates all notification business logic.
type Service struct {
	repo        repository.Repository
	queue       *queue.Queue
	rateLimiter *ratelimiter.RateLimiter
	providers   map[model.Channel]provider.Provider
	metrics     *metrics.Metrics
	wsHub       *ws.Hub
	logger      *zap.Logger
	maxRetries  int
}

// New creates a new notification service.
func New(
	repo repository.Repository,
	q *queue.Queue,
	rl *ratelimiter.RateLimiter,
	providers map[model.Channel]provider.Provider,
	m *metrics.Metrics,
	hub *ws.Hub,
	logger *zap.Logger,
	maxRetries int,
) *Service {
	return &Service{
		repo:        repo,
		queue:       q,
		rateLimiter: rl,
		providers:   providers,
		metrics:     m,
		wsHub:       hub,
		logger:      logger,
		maxRetries:  maxRetries,
	}
}

// Create creates a single notification.
func (s *Service) Create(ctx context.Context, req model.CreateRequest, correlationID string) (*model.Notification, error) {
	// Validate content
	if err := s.validateContent(req); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// Idempotency check
	if req.IdempotencyKey != nil && *req.IdempotencyKey != "" {
		existing, err := s.repo.GetByIdempotencyKey(ctx, *req.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("idempotency check failed: %w", err)
		}
		if existing != nil {
			s.logger.Info("Idempotent request, returning existing notification",
				zap.String("notification_id", existing.ID.String()),
				zap.String("idempotency_key", *req.IdempotencyKey),
				zap.String("correlation_id", correlationID),
			)
			return existing, nil
		}
	}

	// Render template if needed
	content := req.Content
	if req.TemplateID != nil {
		rendered, err := s.renderTemplate(ctx, *req.TemplateID, req.Variables)
		if err != nil {
			return nil, fmt.Errorf("template rendering failed: %w", err)
		}
		content = rendered
	}

	// Build notification
	notification := &model.Notification{
		ID:             uuid.New(),
		Recipient:      req.Recipient,
		Channel:        req.Channel,
		Content:        content,
		Subject:        req.Subject,
		Priority:       req.Priority,
		Status:         model.StatusPending,
		ScheduledAt:    req.ScheduledAt,
		TemplateID:     req.TemplateID,
		IdempotencyKey: req.IdempotencyKey,
		MaxRetries:     s.maxRetries,
		CorrelationID:  correlationID,
	}

	if req.Variables != nil {
		varsJSON, _ := json.Marshal(req.Variables)
		varsStr := string(varsJSON)
		notification.Variables = &varsStr
	}

	// Persist
	if err := s.repo.Create(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	// Enqueue or schedule
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		if err := s.queue.ScheduleNotification(ctx, notification.ID, notification.Priority, correlationID, *req.ScheduledAt); err != nil {
			return nil, fmt.Errorf("failed to schedule notification: %w", err)
		}
		s.logger.Info("Notification scheduled",
			zap.String("notification_id", notification.ID.String()),
			zap.Time("scheduled_at", *req.ScheduledAt),
			zap.String("correlation_id", correlationID),
		)
	} else {
		if err := s.queue.Enqueue(ctx, notification.ID, notification.Priority, correlationID); err != nil {
			return nil, fmt.Errorf("failed to enqueue notification: %w", err)
		}
		notification.Status = model.StatusQueued
		_ = s.repo.UpdateStatus(ctx, notification.ID, model.StatusQueued, nil, nil)
	}

	return notification, nil
}

// CreateBatch creates a batch of notifications.
func (s *Service) CreateBatch(ctx context.Context, req model.BatchRequest, correlationID string) (*model.BatchResponse, error) {
	if len(req.Notifications) == 0 {
		return nil, fmt.Errorf("batch must contain at least one notification")
	}
	if len(req.Notifications) > 1000 {
		return nil, fmt.Errorf("batch size exceeds maximum of 1000")
	}

	batchID := uuid.New()
	notifications := make([]*model.Notification, 0, len(req.Notifications))

	for i, r := range req.Notifications {
		if err := s.validateContent(r); err != nil {
			return nil, fmt.Errorf("notification %d validation error: %w", i, err)
		}

		content := r.Content
		if r.TemplateID != nil {
			rendered, err := s.renderTemplate(ctx, *r.TemplateID, r.Variables)
			if err != nil {
				return nil, fmt.Errorf("notification %d template rendering failed: %w", i, err)
			}
			content = rendered
		}

		n := &model.Notification{
			ID:             uuid.New(),
			BatchID:        &batchID,
			Recipient:      r.Recipient,
			Channel:        r.Channel,
			Content:        content,
			Subject:        r.Subject,
			Priority:       r.Priority,
			Status:         model.StatusPending,
			ScheduledAt:    r.ScheduledAt,
			TemplateID:     r.TemplateID,
			IdempotencyKey: r.IdempotencyKey,
			MaxRetries:     s.maxRetries,
			CorrelationID:  correlationID,
		}
		if r.Variables != nil {
			varsJSON, _ := json.Marshal(r.Variables)
			varsStr := string(varsJSON)
			n.Variables = &varsStr
		}
		notifications = append(notifications, n)
	}

	// Batch persist
	if err := s.repo.CreateBatch(ctx, notifications); err != nil {
		return nil, fmt.Errorf("failed to create batch: %w", err)
	}

	// Enqueue all
	for _, n := range notifications {
		if n.ScheduledAt != nil && n.ScheduledAt.After(time.Now()) {
			_ = s.queue.ScheduleNotification(ctx, n.ID, n.Priority, correlationID, *n.ScheduledAt)
		} else {
			if err := s.queue.Enqueue(ctx, n.ID, n.Priority, correlationID); err != nil {
				s.logger.Error("Failed to enqueue batch notification",
					zap.String("notification_id", n.ID.String()),
					zap.Error(err),
				)
				continue
			}
			n.Status = model.StatusQueued
			_ = s.repo.UpdateStatus(ctx, n.ID, model.StatusQueued, nil, nil)
		}
	}

	result := make([]model.Notification, len(notifications))
	for i, n := range notifications {
		result[i] = *n
	}

	return &model.BatchResponse{
		BatchID:       batchID,
		Total:         len(notifications),
		Notifications: result,
	}, nil
}

// GetByID retrieves a notification by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*model.Notification, error) {
	return s.repo.GetByID(ctx, id)
}

// GetByBatchID retrieves all notifications in a batch.
func (s *Service) GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]model.Notification, error) {
	return s.repo.GetByBatchID(ctx, batchID)
}

// Cancel cancels a pending/queued notification.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) error {
	return s.repo.Cancel(ctx, id)
}

// List retrieves notifications with filtering and pagination.
func (s *Service) List(ctx context.Context, filter model.ListFilter, page, pageSize int) (*model.PaginatedResponse, error) {
	notifications, total, err := s.repo.List(ctx, filter, page, pageSize)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	return &model.PaginatedResponse{
		Data:     notifications,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// Process processes a notification from the queue: rate-limit → send → update status.
func (s *Service) Process(ctx context.Context, notificationID uuid.UUID, correlationID string, attempt int) error {
	logger := s.logger.With(
		zap.String("notification_id", notificationID.String()),
		zap.String("correlation_id", correlationID),
		zap.Int("attempt", attempt),
	)

	// Fetch notification
	notification, err := s.repo.GetByID(ctx, notificationID)
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	// Check if already sent or cancelled
	if notification.Status == model.StatusSent || notification.Status == model.StatusCancelled {
		logger.Info("Notification already in terminal state, skipping", zap.String("status", string(notification.Status)))
		return nil
	}

	// Update status to processing
	_ = s.repo.UpdateStatus(ctx, notificationID, model.StatusProcessing, nil, nil)
	s.broadcastStatus(notification, model.StatusProcessing, attempt, "")

	// Rate limit wait
	if err := s.rateLimiter.Wait(ctx, string(notification.Channel)); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Get provider
	prov, ok := s.providers[notification.Channel]
	if !ok {
		errMsg := fmt.Sprintf("no provider for channel: %s", notification.Channel)
		_ = s.repo.UpdateStatus(ctx, notificationID, model.StatusFailed, nil, &errMsg)
		s.metrics.IncrementFailed(string(notification.Channel))
		s.broadcastStatus(notification, model.StatusFailed, attempt, errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Increment attempt
	_ = s.repo.IncrementAttempts(ctx, notificationID)

	// Send
	startTime := time.Now()
	result, sendErr := prov.Send(ctx, notification)
	elapsed := time.Since(startTime)
	s.metrics.RecordLatency(elapsed)

	if sendErr != nil {
		logger.Error("Provider send failed",
			zap.String("channel", string(notification.Channel)),
			zap.Duration("latency", elapsed),
			zap.Error(sendErr),
		)

		// Check retry strategy
		strategy := retry.DefaultStrategy()
		strategy.MaxRetries = s.maxRetries
		shouldRetry, _ := retry.ShouldRetry(attempt, sendErr, strategy)

		errMsg := sendErr.Error()

		if shouldRetry {
			// Re-enqueue to retry queue
			if qErr := s.queue.EnqueueRetry(ctx, notificationID, notification.Priority, correlationID, attempt+1); qErr != nil {
				logger.Error("Failed to enqueue retry", zap.Error(qErr))
			}
			_ = s.repo.UpdateStatus(ctx, notificationID, model.StatusQueued, nil, &errMsg)
			s.broadcastStatus(notification, model.StatusQueued, attempt, errMsg)
			logger.Info("Notification queued for retry", zap.Int("next_attempt", attempt+1))
		} else {
			// Send to DLQ
			_ = s.queue.EnqueueDLQ(ctx, notificationID, correlationID, errMsg)
			_ = s.repo.UpdateStatus(ctx, notificationID, model.StatusFailed, nil, &errMsg)
			s.metrics.IncrementFailed(string(notification.Channel))
			s.broadcastStatus(notification, model.StatusFailed, attempt, errMsg)
			logger.Error("Notification permanently failed, sent to DLQ")
		}
		return sendErr
	}

	// Success
	var providerMsgID *string
	if result != nil && result.ProviderMessageID != "" {
		providerMsgID = &result.ProviderMessageID
	}

	_ = s.repo.UpdateStatus(ctx, notificationID, model.StatusSent, providerMsgID, nil)
	s.metrics.IncrementSent(string(notification.Channel))
	s.broadcastStatus(notification, model.StatusSent, attempt, "")

	logger.Info("Notification sent successfully",
		zap.String("channel", string(notification.Channel)),
		zap.Duration("latency", elapsed),
	)

	return nil
}

// GetMetrics returns the current system metrics.
func (s *Service) GetMetrics() *model.MetricsResponse {
	successRate, failureRate := s.metrics.GetRates()
	queueDepth, _ := s.queue.QueueDepth()

	return &model.MetricsResponse{
		QueueDepth:          queueDepth,
		NotificationsSent:   s.metrics.GetSentCounts(),
		NotificationsFailed: s.metrics.GetFailedCounts(),
		ProcessingLatency:   s.metrics.GetLatencyPercentiles(),
		SuccessRate:         successRate,
		FailureRate:         failureRate,
	}
}

func (s *Service) broadcastStatus(notification *model.Notification, status model.Status, attempt int, errMsg string) {
	if s.wsHub != nil {
		s.wsHub.BroadcastStatus(
			notification.ID.String(),
			string(status),
			string(notification.Channel),
			attempt,
			errMsg,
		)
	}
}

// validateContent validates the notification content based on channel constraints.
func (s *Service) validateContent(req model.CreateRequest) error {
	if req.Recipient == "" {
		return fmt.Errorf("recipient is required")
	}
	if !req.Channel.IsValid() {
		return fmt.Errorf("invalid channel: %s (must be sms, email, or push)", req.Channel)
	}
	if !req.Priority.IsValid() {
		req.Priority = model.PriorityNormal
	}

	switch req.Channel {
	case model.ChannelSMS:
		if len(req.Content) > 160 {
			return fmt.Errorf("SMS content exceeds 160 character limit (got %d)", len(req.Content))
		}
	case model.ChannelPush:
		if len(req.Content) > 256 {
			return fmt.Errorf("push content exceeds 256 character limit (got %d)", len(req.Content))
		}
	case model.ChannelEmail:
		if req.Subject == "" {
			return fmt.Errorf("email subject is required")
		}
	}

	if req.Content == "" && req.TemplateID == nil {
		return fmt.Errorf("content or template_id is required")
	}

	return nil
}

// renderTemplate fetches a template from DB and renders it with variables.
func (s *Service) renderTemplate(ctx context.Context, templateID uuid.UUID, vars map[string]string) (string, error) {
	tmpl, err := s.repo.GetTemplateByID(ctx, templateID)
	if err != nil {
		return "", fmt.Errorf("template %s not found: %w", templateID, err)
	}

	rendered, err := RenderTemplateString(tmpl.Body, vars)
	if err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", tmpl.Name, err)
	}

	return rendered, nil
}

// RenderTemplateString renders a Go template string with variables.
func RenderTemplateString(tmplStr string, vars map[string]string) (string, error) {
	t, err := template.New("notification").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// CreateTemplate creates a new notification template.
func (s *Service) CreateTemplate(ctx context.Context, tmpl *model.Template) error {
	if tmpl.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if tmpl.Body == "" {
		return fmt.Errorf("template body is required")
	}
	if !tmpl.Channel.IsValid() {
		return fmt.Errorf("invalid channel: %s", tmpl.Channel)
	}
	tmpl.ID = uuid.New()
	return s.repo.CreateTemplate(ctx, tmpl)
}

// ListTemplates returns all templates.
func (s *Service) ListTemplates(ctx context.Context) ([]model.Template, error) {
	return s.repo.ListTemplates(ctx)
}

// GetTemplate returns a template by ID.
func (s *Service) GetTemplate(ctx context.Context, id uuid.UUID) (*model.Template, error) {
	return s.repo.GetTemplateByID(ctx, id)
}
