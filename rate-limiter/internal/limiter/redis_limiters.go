package limiter

import (
	"context"
	"time"

	"github.com/herryxhl/rate-limiter/internal/store"
)

// RedisTokenBucketLimiter is the production token bucket limiter.
// Backed by Redis so it works correctly across multiple service instances —
// unlike the in-memory version which has separate state per process.
type RedisTokenBucketLimiter struct {
	store      *store.RedisStore
	capacity   int
	refillRate float64
}

func NewRedisTokenBucketLimiter(
	s *store.RedisStore,
	capacity int,
	refillRate float64,
) *RedisTokenBucketLimiter {
	return &RedisTokenBucketLimiter{
		store:      s,
		capacity:   capacity,
		refillRate: refillRate,
	}
}

func (r *RedisTokenBucketLimiter) Allow(ctx context.Context, key string) (Result, error) {
	return r.store.TokenBucketAllow(ctx, key, r.capacity, r.refillRate)
}

// RedisSlidingWindowLimiter is the production sliding window limiter.
// Uses a Redis sorted set — each request is a member scored by timestamp.
// Expired entries are evicted atomically on every check.
type RedisSlidingWindowLimiter struct {
	store      *store.RedisStore
	limit      int
	windowSize time.Duration
}

func NewRedisSlidingWindowLimiter(
	s *store.RedisStore,
	limit int,
	windowSize time.Duration,
) *RedisSlidingWindowLimiter {
	return &RedisSlidingWindowLimiter{
		store:      s,
		limit:      limit,
		windowSize: windowSize,
	}
}

func (r *RedisSlidingWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	return r.store.SlidingWindowAllow(ctx, key, r.limit, r.windowSize)
}
