package ratelimit

import (
	"context"
	"time"
)

// Result holds the outcome of a single rate limit check.
// Returned by every Limiter implementation so the HTTP/gRPC
// handler always gets the same shape regardless of algorithm.
type Result struct {
	Allowed    bool
	Remaining  int           // requests/tokens left in current window
	Limit      int           // total configured limit
	RetryAfter time.Duration // how long to wait before retrying (0 if allowed)
}

// Limiter is the core strategy interface.
// TokenBucketLimiter and SlidingWindowLimiter both implement this.
// The HTTP handler only ever calls Allow() — it never knows which
// algorithm is running underneath.
type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
}
