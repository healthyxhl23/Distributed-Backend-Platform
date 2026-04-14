package limiter

import (
	"context"
	"fmt"
	"testing"
)

// ── In-memory benchmarks ──────────────────────────────────────────────────────
// These measure raw algorithm speed with no I/O — establishes a baseline.

func BenchmarkTokenBucket_Sequential(b *testing.B) {
	lim := NewTokenBucketLimiter(b.N, float64(b.N))
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		lim.Allow(ctx, "user:bench")
	}
}

func BenchmarkTokenBucket_Parallel(b *testing.B) {
	lim := NewTokenBucketLimiter(b.N, float64(b.N))
	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lim.Allow(ctx, "user:bench")
		}
	})
}

func BenchmarkSlidingWindow_Sequential(b *testing.B) {
	lim := NewSlidingWindowLimiter(b.N, 0) // 0 window = never expires during bench
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		lim.Allow(ctx, "user:bench")
	}
}

func BenchmarkSlidingWindow_Parallel(b *testing.B) {
	lim := NewSlidingWindowLimiter(b.N, 0)
	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lim.Allow(ctx, "user:bench")
		}
	})
}

// ── Multi-key benchmarks ──────────────────────────────────────────────────────
// Simulates real traffic where each user has their own bucket/window.
// Tests map lookup + lock contention under many distinct keys.

func BenchmarkTokenBucket_MultiKey(b *testing.B) {
	lim := NewTokenBucketLimiter(1000, 1000)
	ctx := context.Background()
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("user:%d", i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lim.Allow(ctx, keys[i%len(keys)])
			i++
		}
	})
}

func BenchmarkSlidingWindow_MultiKey(b *testing.B) {
	lim := NewSlidingWindowLimiter(1000, 0)
	ctx := context.Background()
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("user:%d", i)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lim.Allow(ctx, keys[i%len(keys)])
			i++
		}
	})
}
