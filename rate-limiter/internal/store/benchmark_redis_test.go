package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ── Redis benchmarks ──────────────────────────────────────────────────────────
// These measure real-world throughput including Redis round-trip latency.
// Run with: go test -bench=. -benchtime=10s ./internal/store/...
// The ns/op number is your latency per check.
// ops/sec = 1,000,000,000 / ns_per_op  ← put this in your resume bullet.

func BenchmarkRedisTokenBucket_Sequential(b *testing.B) {
	rs := newTestStore(b)
	ctx := context.Background()
	rs.client.Del(ctx, "rl:tb:bench:seq")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rs.TokenBucketAllow(ctx, "bench:seq", b.N, float64(b.N))
	}
}

func BenchmarkRedisTokenBucket_Parallel(b *testing.B) {
	rs := newTestStore(b)
	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rs.TokenBucketAllow(ctx, "bench:parallel", b.N, float64(b.N))
		}
	})
}

func BenchmarkRedisSlidingWindow_Sequential(b *testing.B) {
	rs := newTestStore(b)
	ctx := context.Background()
	rs.client.Del(ctx, "rl:sw:bench:seq")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rs.SlidingWindowAllow(ctx, "bench:seq", b.N, time.Hour)
	}
}

func BenchmarkRedisSlidingWindow_Parallel(b *testing.B) {
	rs := newTestStore(b)
	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rs.SlidingWindowAllow(ctx, "bench:parallel", b.N, time.Hour)
		}
	})
}

// BenchmarkRedisTokenBucket_MultiKey simulates 100 distinct users hitting
// the service concurrently — the most realistic production traffic pattern.
func BenchmarkRedisTokenBucket_MultiKey(b *testing.B) {
	rs := newTestStore(b)
	ctx := context.Background()
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("user:%d", i)
		rs.client.Del(ctx, "rl:tb:"+keys[i])
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rs.TokenBucketAllow(ctx, keys[i%len(keys)], 10000, 10000)
			i++
		}
	})
}
