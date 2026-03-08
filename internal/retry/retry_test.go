package retry_test

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/elcinzorlu/notification-system/internal/retry"
	"github.com/stretchr/testify/assert"
)

func TestShouldRetry_MaxRetriesExceeded(t *testing.T) {
	strategy := retry.DefaultStrategy()
	shouldRetry, _ := retry.ShouldRetry(5, errors.New("some error"), strategy)
	assert.False(t, shouldRetry, "should not retry after max attempts")
}

func TestShouldRetry_RetryableErrors(t *testing.T) {
	strategy := retry.DefaultStrategy()

	// 5xx errors should be retryable
	t.Run("5xx error", func(t *testing.T) {
		err := retry.NewProviderError(500, "internal server error", nil)
		shouldRetry, delay := retry.ShouldRetry(0, err, strategy)
		assert.True(t, shouldRetry)
		assert.Greater(t, delay, time.Duration(0))
	})

	t.Run("503 error", func(t *testing.T) {
		err := retry.NewProviderError(503, "service unavailable", nil)
		shouldRetry, _ := retry.ShouldRetry(1, err, strategy)
		assert.True(t, shouldRetry)
	})

	// Network errors should be retryable
	t.Run("network error", func(t *testing.T) {
		err := &net.DNSError{Err: "no such host", Name: "example.com"}
		shouldRetry, _ := retry.ShouldRetry(0, err, strategy)
		assert.True(t, shouldRetry)
	})
}

func TestShouldRetry_NonRetryableErrors(t *testing.T) {
	strategy := retry.DefaultStrategy()

	// 4xx errors should NOT be retryable
	t.Run("400 error", func(t *testing.T) {
		err := retry.NewProviderError(400, "bad request", nil)
		shouldRetry, _ := retry.ShouldRetry(0, err, strategy)
		assert.False(t, shouldRetry)
	})

	t.Run("404 error", func(t *testing.T) {
		err := retry.NewProviderError(404, "not found", nil)
		shouldRetry, _ := retry.ShouldRetry(0, err, strategy)
		assert.False(t, shouldRetry)
	})

	t.Run("non-retryable marker", func(t *testing.T) {
		err := fmt.Errorf("validation failed: %w", retry.ErrNonRetryable)
		shouldRetry, _ := retry.ShouldRetry(0, err, strategy)
		assert.False(t, shouldRetry)
	})
}

func TestShouldRetry_ExponentialBackoff(t *testing.T) {
	strategy := retry.Strategy{
		MaxRetries:     5,
		BaseDelay:      1 * time.Second,
		MaxDelay:       16 * time.Second,
		JitterFraction: 0, // No jitter for predictable testing
	}

	err := retry.NewProviderError(500, "server error", nil)

	expectedDelays := []time.Duration{
		1 * time.Second,  // attempt 0: 1s * 2^0
		2 * time.Second,  // attempt 1: 1s * 2^1
		4 * time.Second,  // attempt 2: 1s * 2^2
		8 * time.Second,  // attempt 3: 1s * 2^3
		16 * time.Second, // attempt 4: 1s * 2^4 (capped)
	}

	for i, expected := range expectedDelays {
		shouldRetry, delay := retry.ShouldRetry(i, err, strategy)
		assert.True(t, shouldRetry, "attempt %d should be retryable", i)
		assert.Equal(t, expected, delay, "attempt %d delay mismatch", i)
	}
}

func TestShouldRetry_NilError(t *testing.T) {
	strategy := retry.DefaultStrategy()
	shouldRetry, _ := retry.ShouldRetry(0, nil, strategy)
	assert.False(t, shouldRetry, "nil error should not be retried")
}

func TestProviderError(t *testing.T) {
	innerErr := errors.New("connection refused")
	err := retry.NewProviderError(502, "bad gateway", innerErr)

	assert.Equal(t, "bad gateway", err.Error())
	assert.Equal(t, 502, err.StatusCode)
	assert.True(t, errors.Is(err, innerErr))
}
