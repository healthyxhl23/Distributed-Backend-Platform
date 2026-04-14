package limiter

import (
	"context"
	"sync"
	"time"
)

// SlidingWindowLimiter tracks request timestamps per key in memory.
// Good for: strict per-window enforcement with no burst allowance.
// Every request is counted; the window slides with real time.
//
// Example: limit=10, windowSize=time.Second
//   → at most 10 requests in any rolling 1-second window
type SlidingWindowLimiter struct {
	limit      int
	windowSize time.Duration
	windows    map[string][]time.Time
	mu         sync.Mutex
}

// NewSlidingWindowLimiter creates an in-memory sliding window limiter.
//   limit:      max requests per window
//   windowSize: rolling window duration (0 = never expires)
func NewSlidingWindowLimiter(limit int, windowSize time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:      limit,
		windowSize: windowSize,
		windows:    make(map[string][]time.Time),
	}
}

// Allow checks if the request for the given key should be allowed.
// Thread-safe — safe to call concurrently from multiple goroutines.
func (s *SlidingWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// evict timestamps outside the window (skip when windowSize==0: no expiry)
	if s.windowSize > 0 {
		cutoff := now.Add(-s.windowSize)
		ts := s.windows[key]
		start := 0
		for start < len(ts) && ts[start].Before(cutoff) {
			start++
		}
		s.windows[key] = ts[start:]
	}

	ts := s.windows[key]
	if len(ts) < s.limit {
		s.windows[key] = append(ts, now)
		return Result{
			Allowed:   true,
			Remaining: s.limit - len(s.windows[key]),
			Limit:     s.limit,
		}, nil
	}

	// denied — how long until the oldest entry falls outside the window
	var retryAfter time.Duration
	if s.windowSize > 0 && len(ts) > 0 {
		retryAfter = ts[0].Add(s.windowSize).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
	}

	return Result{
		Allowed:    false,
		Remaining:  0,
		Limit:      s.limit,
		RetryAfter: retryAfter,
	}, nil
}
