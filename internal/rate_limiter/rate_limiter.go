package ratelimiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a sliding window rate limiter using Redis.
type RateLimiter struct {
	client     *redis.Client
	limitPerSec int
}

// New creates a new rate limiter.
func New(client *redis.Client, limitPerSec int) *RateLimiter {
	return &RateLimiter{
		client:     client,
		limitPerSec: limitPerSec,
	}
}

// Allow checks if a request is allowed for the given channel.
// Uses Redis INCR with 1-second TTL as a sliding window counter.
// Key format: rate:{channel}:{unix_timestamp}
func (r *RateLimiter) Allow(ctx context.Context, channel string) (bool, error) {
	now := time.Now().Unix()
	key := fmt.Sprintf("rate:%s:%d", channel, now)

	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 2*time.Second) // TTL slightly longer than window

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("rate limiter pipeline error: %w", err)
	}

	count := incr.Val()
	return count <= int64(r.limitPerSec), nil
}

// Wait blocks until the rate limit allows the request or context is cancelled.
func (r *RateLimiter) Wait(ctx context.Context, channel string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			allowed, err := r.Allow(ctx, channel)
			if err != nil {
				return err
			}
			if allowed {
				return nil
			}
			// Wait a small interval before retrying
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

// CurrentCount returns the current count for a channel in the current window.
func (r *RateLimiter) CurrentCount(ctx context.Context, channel string) (int64, error) {
	now := time.Now().Unix()
	key := fmt.Sprintf("rate:%s:%d", channel, now)
	count, err := r.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}
