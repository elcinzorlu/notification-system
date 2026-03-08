package provider

import (
	"context"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
)

// SendResult represents the result from the external provider.
type SendResult struct {
	ProviderMessageID string `json:"messageId"`
	Status            string `json:"status"`
	Timestamp         string `json:"timestamp"`
}

// Provider defines the interface for sending notifications.
type Provider interface {
	// Send sends a notification to the external provider.
	Send(ctx context.Context, notification *model.Notification) (*SendResult, error)

	// Channel returns the channel this provider handles.
	Channel() model.Channel
}
