package limiter

import (
	"context"
	"math"
	"sync"
	"time"
)

// bucket holds the state for a single key
type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// TokenBucketLimiter manages one bucket per key in memory.
// Good for: bursty traffic where short spikes are acceptable.
// A user can accumulate tokens when idle and spend them quickly in a burst.
//
// Example: capacity=10, refillRate=2.0
//   → max burst of 10 requests
//   → sustained rate of 2 requests/sec
type TokenBucketLimiter struct {
	capacity   float64        // max tokens a bucket can hold
	refillRate float64        // tokens added per second
	buckets    map[string]*bucket
	mu         sync.Mutex
}

// NewTokenBucketLimiter creates an in-memory token bucket limiter.
//   capacity:   max burst size       (e.g. 10)
//   refillRate: sustained req/sec    (e.g. 2.0)
func NewTokenBucketLimiter(capacity int, refillRate float64) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		capacity:   float64(capacity),
		refillRate: refillRate,
		buckets:    make(map[string]*bucket),
	}
}

// Allow checks if the request for the given key should be allowed.
// Thread-safe — safe to call concurrently from multiple goroutines.
func (tbl *TokenBucketLimiter) Allow(ctx context.Context, key string) (Result, error) {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	now := time.Now()

	b, ok := tbl.buckets[key]
	if !ok {
		// first request from this key — start with a full bucket
		b = &bucket{tokens: tbl.capacity, lastRefill: now}
		tbl.buckets[key] = b
	}

	// refill tokens based on time elapsed since last request
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = math.Min(tbl.capacity, b.tokens+elapsed*tbl.refillRate)
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return Result{
			Allowed:   true,
			Remaining: int(b.tokens),
			Limit:     int(tbl.capacity),
		}, nil
	}

	// denied — calculate how long until 1 token is available
	tokensNeeded := 1 - b.tokens
	retryAfter := time.Duration(tokensNeeded / tbl.refillRate * float64(time.Second))

	return Result{
		Allowed:    false,
		Remaining:  0,
		Limit:      int(tbl.capacity),
		RetryAfter: retryAfter,
	}, nil
}