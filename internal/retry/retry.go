package retry

import (
	"errors"
	"math"
	"math/rand"
	"net"
	"time"
)

// Error classification types.
var (
	ErrRetryable    = errors.New("retryable error")
	ErrNonRetryable = errors.New("non-retryable error")
)

// ProviderError represents an error from the external provider with HTTP status.
type ProviderError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *ProviderError) Error() string {
	return e.Message
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// NewProviderError creates a new ProviderError.
func NewProviderError(statusCode int, message string, err error) *ProviderError {
	return &ProviderError{
		StatusCode: statusCode,
		Message:    message,
		Err:        err,
	}
}

// Strategy holds retry configuration.
type Strategy struct {
	MaxRetries     int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	JitterFraction float64 // 0.0 to 1.0
}

// DefaultStrategy returns a default retry strategy.
func DefaultStrategy() Strategy {
	return Strategy{
		MaxRetries:     5,
		BaseDelay:      1 * time.Second,
		MaxDelay:       16 * time.Second,
		JitterFraction: 0.2,
	}
}

// ShouldRetry determines if the error is retryable and calculates the next delay.
// Error classification:
//   - Network errors → retry
//   - 5xx status codes → retry
//   - 4xx status codes → fail (non-retryable)
//   - Validation errors → fail (non-retryable)
//   - Timeout errors → retry
func ShouldRetry(attempt int, err error, strategy Strategy) (bool, time.Duration) {
	if attempt >= strategy.MaxRetries {
		return false, 0
	}

	if !isRetryable(err) {
		return false, 0
	}

	delay := calculateBackoff(attempt, strategy)
	return true, delay
}

// isRetryable classifies the error.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for ProviderError (HTTP status based classification)
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		// 4xx → non-retryable (client error)
		if providerErr.StatusCode >= 400 && providerErr.StatusCode < 500 {
			return false
		}
		// 5xx → retryable (server error)
		if providerErr.StatusCode >= 500 {
			return true
		}
	}

	// Network errors → retryable
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for explicit retryable/non-retryable markers
	if errors.Is(err, ErrRetryable) {
		return true
	}
	if errors.Is(err, ErrNonRetryable) {
		return false
	}

	// Default: retryable (assume transient)
	return true
}

// calculateBackoff computes exponential backoff with jitter.
// Delays: ~1s, ~2s, ~4s, ~8s, ~16s (capped at MaxDelay)
func calculateBackoff(attempt int, strategy Strategy) time.Duration {
	delay := float64(strategy.BaseDelay) * math.Pow(2, float64(attempt))

	if time.Duration(delay) > strategy.MaxDelay {
		delay = float64(strategy.MaxDelay)
	}

	// Add jitter
	jitter := delay * strategy.JitterFraction
	delay = delay + (rand.Float64()*2-1)*jitter

	return time.Duration(delay)
}
