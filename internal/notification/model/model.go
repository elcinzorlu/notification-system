package model

import (
	"time"

	"github.com/google/uuid"
)

// Channel represents the notification delivery channel.
type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelPush  Channel = "push"
)

// IsValid checks if the channel is valid.
func (c Channel) IsValid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelPush:
		return true
	}
	return false
}

// Priority represents the notification priority level.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// IsValid checks if the priority is valid.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}

// RabbitMQPriority returns the RabbitMQ priority value (0-10).
func (p Priority) RabbitMQPriority() uint8 {
	switch p {
	case PriorityHigh:
		return 10
	case PriorityNormal:
		return 5
	case PriorityLow:
		return 1
	default:
		return 5
	}
}

// Status represents the notification processing status.
type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

// Notification is the core domain entity.
type Notification struct {
	ID                uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	BatchID           *uuid.UUID `json:"batch_id,omitempty" gorm:"type:uuid;index"`
	Recipient         string     `json:"recipient" gorm:"not null"`
	Channel           Channel    `json:"channel" gorm:"type:varchar(10);not null;index"`
	Content           string     `json:"content" gorm:"type:text;not null"`
	Subject           string     `json:"subject,omitempty" gorm:"type:varchar(255)"`
	Priority          Priority   `json:"priority" gorm:"type:varchar(10);not null;default:'normal';index"`
	Status            Status     `json:"status" gorm:"type:varchar(20);not null;default:'pending';index"`
	ScheduledAt       *time.Time `json:"scheduled_at,omitempty" gorm:"index"`
	TemplateID        *uuid.UUID `json:"template_id,omitempty" gorm:"type:uuid"`
	Variables         *string    `json:"variables,omitempty" gorm:"type:jsonb"`
	IdempotencyKey    *string    `json:"idempotency_key,omitempty" gorm:"type:varchar(255);uniqueIndex:idx_idempotency_key,where:idempotency_key IS NOT NULL"`
	ProviderMessageID *string    `json:"provider_message_id,omitempty" gorm:"type:varchar(255)"`
	Attempts          int        `json:"attempts" gorm:"default:0"`
	MaxRetries        int        `json:"max_retries" gorm:"default:5"`
	LastAttemptAt     *time.Time `json:"last_attempt_at,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty" gorm:"type:text"`
	CorrelationID     string     `json:"correlation_id,omitempty" gorm:"type:varchar(255);index"`
	CreatedAt         time.Time  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// Template represents a notification template with variable substitution.
type Template struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name      string    `json:"name" gorm:"type:varchar(255);not null;uniqueIndex"`
	Channel   Channel   `json:"channel" gorm:"type:varchar(10);not null"`
	Subject   string    `json:"subject,omitempty" gorm:"type:varchar(255)"`
	Body      string    `json:"body" gorm:"type:text;not null"`
	Variables *string  `json:"variables,omitempty" gorm:"type:jsonb"` // JSON array of variable names
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// --- Request / Response DTOs ---

// CreateRequest represents a single notification creation request.
type CreateRequest struct {
	Recipient      string            `json:"recipient" validate:"required"`
	Channel        Channel           `json:"channel" validate:"required"`
	Content        string            `json:"content" validate:"required"`
	Subject        string            `json:"subject,omitempty"`
	Priority       Priority          `json:"priority" validate:"required"`
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty"`
	TemplateID     *uuid.UUID        `json:"template_id,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
	IdempotencyKey *string           `json:"idempotency_key,omitempty"`
}

// BatchRequest represents a batch notification creation request.
type BatchRequest struct {
	Notifications []CreateRequest `json:"notifications" validate:"required,min=1,max=1000"`
}

// BatchResponse represents the response for a batch creation.
type BatchResponse struct {
	BatchID       uuid.UUID      `json:"batch_id"`
	Total         int            `json:"total"`
	Notifications []Notification `json:"notifications"`
}

// ListFilter represents filters for listing notifications.
type ListFilter struct {
	Status   *Status  `json:"status,omitempty"`
	Channel  *Channel `json:"channel,omitempty"`
	DateFrom *time.Time `json:"date_from,omitempty"`
	DateTo   *time.Time `json:"date_to,omitempty"`
}

// PaginatedResponse represents a paginated list response.
type PaginatedResponse struct {
	Data     interface{} `json:"data"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int64       `json:"total"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status   string `json:"status"`
	DB       string `json:"db"`
	Redis    string `json:"redis"`
	RabbitMQ string `json:"rabbitmq"`
}

// MetricsResponse represents the metrics endpoint response.
type MetricsResponse struct {
	QueueDepth          map[string]int64   `json:"queue_depth"`
	NotificationsSent   map[string]int64   `json:"notifications_sent_total"`
	NotificationsFailed map[string]int64   `json:"notifications_failed_total"`
	ProcessingLatency   map[string]float64 `json:"processing_latency_ms"`
	SuccessRate         float64            `json:"success_rate"`
	FailureRate         float64            `json:"failure_rate"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}
