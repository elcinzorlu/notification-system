package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/elcinzorlu/notification-system/internal/provider"
	"github.com/elcinzorlu/notification-system/internal/retry"
	"go.uber.org/zap"
)

type emailProvider struct {
	webhookURL string
	client     *http.Client
	logger     *zap.Logger
}

type emailRequest struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Subject string `json:"subject"`
	Content string `json:"content"`
}

// New creates a new Email provider.
func New(webhookURL string, logger *zap.Logger) provider.Provider {
	return &emailProvider{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger,
	}
}

func (p *emailProvider) Channel() model.Channel {
	return model.ChannelEmail
}

func (p *emailProvider) Send(ctx context.Context, notification *model.Notification) (*provider.SendResult, error) {
	reqBody := emailRequest{
		To:      notification.Recipient,
		Channel: string(model.ChannelEmail),
		Subject: notification.Subject,
		Content: notification.Content,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal email request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if cid := notification.CorrelationID; cid != "" {
		req.Header.Set("X-Correlation-ID", cid)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("email provider request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read email response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, retry.NewProviderError(
			resp.StatusCode,
			fmt.Sprintf("email provider returned status %d: %s", resp.StatusCode, string(respBody)),
			nil,
		)
	}

	var result provider.SendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		p.logger.Warn("Failed to parse email response",
			zap.String("body", string(respBody)),
			zap.Error(err),
		)
		result = provider.SendResult{
			Status:    "accepted",
			Timestamp: time.Now().Format(time.RFC3339),
		}
	}

	return &result, nil
}
