package metrics_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/elcinzorlu/notification-system/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTest(t *testing.T) *metrics.Metrics {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return metrics.New(rc)
}

func TestMetrics_IncrementSent(t *testing.T) {
	m := setupTest(t)

	m.IncrementSent("sms")
	m.IncrementSent("sms")
	m.IncrementSent("email")
	m.IncrementSent("push")

	counts := m.GetSentCounts()
	assert.Equal(t, int64(2), counts["sms"])
	assert.Equal(t, int64(1), counts["email"])
	assert.Equal(t, int64(1), counts["push"])
}

func TestMetrics_IncrementFailed(t *testing.T) {
	m := setupTest(t)

	m.IncrementFailed("sms")
	m.IncrementFailed("email")
	m.IncrementFailed("email")

	counts := m.GetFailedCounts()
	assert.Equal(t, int64(1), counts["sms"])
	assert.Equal(t, int64(2), counts["email"])
	assert.Equal(t, int64(0), counts["push"])
}

func TestMetrics_RecordLatency(t *testing.T) {
	m := setupTest(t)

	m.RecordLatency(100 * time.Millisecond)
	m.RecordLatency(200 * time.Millisecond)
	m.RecordLatency(300 * time.Millisecond)

	percentiles := m.GetLatencyPercentiles()
	assert.Greater(t, percentiles["p50"], float64(0))
	assert.Greater(t, percentiles["p95"], float64(0))
	assert.Greater(t, percentiles["p99"], float64(0))
}

func TestMetrics_EmptyLatency(t *testing.T) {
	m := setupTest(t)

	percentiles := m.GetLatencyPercentiles()
	assert.Equal(t, float64(0), percentiles["p50"])
	assert.Equal(t, float64(0), percentiles["p95"])
	assert.Equal(t, float64(0), percentiles["p99"])
}

func TestMetrics_GetRates(t *testing.T) {
	m := setupTest(t)

	m.IncrementSent("sms")
	m.IncrementSent("email")
	m.IncrementSent("push")
	m.IncrementFailed("sms")

	successRate, failureRate := m.GetRates()
	assert.Equal(t, float64(75), successRate)
	assert.Equal(t, float64(25), failureRate)
}

func TestMetrics_GetRates_Empty(t *testing.T) {
	m := setupTest(t)

	successRate, failureRate := m.GetRates()
	assert.Equal(t, float64(0), successRate)
	assert.Equal(t, float64(0), failureRate)
}

func TestMetrics_SharedAcrossInstances(t *testing.T) {
	// Simulates API and Worker both using same Redis
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	worker := metrics.New(rc)
	api := metrics.New(rc)

	// Worker increments
	worker.IncrementSent("sms")
	worker.IncrementSent("sms")
	worker.IncrementFailed("email")

	// API reads — should see worker's data
	sent := api.GetSentCounts()
	failed := api.GetFailedCounts()

	require.Equal(t, int64(2), sent["sms"])
	require.Equal(t, int64(1), failed["email"])
}
