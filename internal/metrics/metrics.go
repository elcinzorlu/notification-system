package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	keyPrefix = "metrics:"
	keySent   = keyPrefix + "sent:"
	keyFailed = keyPrefix + "failed:"
)

// Metrics collects real-time metrics for the notification system.
// Sent/failed counters are stored in Redis (shared between API and Worker).
// Latency samples are kept in-memory on the worker side.
type Metrics struct {
	redis *redis.Client

	// Processing latency tracking (in-memory, worker-side only)
	latencies   []float64
	latencyLock sync.Mutex
	maxSamples  int
}

// New creates a new Metrics collector backed by Redis.
func New(redisClient *redis.Client) *Metrics {
	return &Metrics{
		redis:      redisClient,
		latencies:  make([]float64, 0, 1000),
		maxSamples: 10000,
	}
}

// IncrementSent increments the sent counter for the given channel in Redis.
func (m *Metrics) IncrementSent(channel string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.redis.Incr(ctx, keySent+channel)
}

// IncrementFailed increments the failed counter for the given channel in Redis.
func (m *Metrics) IncrementFailed(channel string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.redis.Incr(ctx, keyFailed+channel)
}

// RecordLatency records the processing latency for a notification.
// Also pushes to Redis list so latency data is available from the API process.
func (m *Metrics) RecordLatency(d time.Duration) {
	ms := float64(d.Milliseconds())

	// Local cache
	m.latencyLock.Lock()
	if len(m.latencies) >= m.maxSamples {
		m.latencies = m.latencies[m.maxSamples/2:]
	}
	m.latencies = append(m.latencies, ms)
	m.latencyLock.Unlock()

	// Push to Redis (keep last 10000 samples)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.redis.RPush(ctx, keyPrefix+"latency", ms)
	m.redis.LTrim(ctx, keyPrefix+"latency", -int64(m.maxSamples), -1)
}

// GetSentCounts returns sent counts per channel from Redis.
func (m *Metrics) GetSentCounts() map[string]int64 {
	return m.getChannelCounts(keySent)
}

// GetFailedCounts returns failed counts per channel from Redis.
func (m *Metrics) GetFailedCounts() map[string]int64 {
	return m.getChannelCounts(keyFailed)
}

func (m *Metrics) getChannelCounts(prefix string) map[string]int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	counts := map[string]int64{"sms": 0, "email": 0, "push": 0}
	for _, ch := range []string{"sms", "email", "push"} {
		val, err := m.redis.Get(ctx, prefix+ch).Int64()
		if err == nil {
			counts[ch] = val
		}
	}
	return counts
}

// GetLatencyPercentiles returns p50, p95, p99 latency in milliseconds from Redis.
func (m *Metrics) GetLatencyPercentiles() map[string]float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	vals, err := m.redis.LRange(ctx, keyPrefix+"latency", 0, -1).Result()
	if err != nil || len(vals) == 0 {
		return map[string]float64{"p50": 0, "p95": 0, "p99": 0}
	}

	samples := make([]float64, 0, len(vals))
	for _, v := range vals {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			samples = append(samples, f)
		}
	}

	if len(samples) == 0 {
		return map[string]float64{"p50": 0, "p95": 0, "p99": 0}
	}

	sort.Float64s(samples)
	n := len(samples)

	return map[string]float64{
		"p50": samples[percentileIndex(n, 50)],
		"p95": samples[percentileIndex(n, 95)],
		"p99": samples[percentileIndex(n, 99)],
	}
}

// GetRates returns success and failure rates as percentages.
func (m *Metrics) GetRates() (successRate, failureRate float64) {
	sent := m.GetSentCounts()
	failed := m.GetFailedCounts()

	totalSent := sent["sms"] + sent["email"] + sent["push"]
	totalFailed := failed["sms"] + failed["email"] + failed["push"]
	total := totalSent + totalFailed

	if total == 0 {
		return 0, 0
	}
	return float64(totalSent) / float64(total) * 100, float64(totalFailed) / float64(total) * 100
}

func percentileIndex(n, p int) int {
	idx := (n * p) / 100
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// SyncFromDB initializes Redis metrics counters from PostgreSQL.
// Should be called on startup to recover metrics after Redis restarts.
func (m *Metrics) SyncFromDB(db *gorm.DB, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type channelCount struct {
		Channel string
		Count   int64
	}

	// Sync sent counts
	var sentCounts []channelCount
	db.Raw("SELECT channel, COUNT(*) as count FROM notifications WHERE status = 'sent' GROUP BY channel").Scan(&sentCounts)
	for _, c := range sentCounts {
		m.redis.Set(ctx, keySent+c.Channel, c.Count, 0)
	}

	// Sync failed counts
	var failedCounts []channelCount
	db.Raw("SELECT channel, COUNT(*) as count FROM notifications WHERE status = 'failed' GROUP BY channel").Scan(&failedCounts)
	for _, c := range failedCounts {
		m.redis.Set(ctx, keyFailed+c.Channel, c.Count, 0)
	}

	// Log
	totalSent := int64(0)
	totalFailed := int64(0)
	for _, c := range sentCounts {
		totalSent += c.Count
	}
	for _, c := range failedCounts {
		totalFailed += c.Count
	}

	logger.Info("Metrics synced from database",
		zap.Int64("total_sent", totalSent),
		zap.Int64("total_failed", totalFailed),
	)
}

