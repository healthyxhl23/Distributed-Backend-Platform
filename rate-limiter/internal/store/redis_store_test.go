package store

import (
	"context"
	"testing"
	"time"
)

// newTestStore returns a RedisStore connected to localhost.
// Skips the test if Redis is not reachable.
// tb is satisfied by both *testing.T and *testing.B
type tb interface {
	Helper()
	Skipf(format string, args ...any)
	Cleanup(f func())
}

func newTestStore(t tb) *RedisStore {
	t.Helper()
	rs := NewRedisStore("localhost:6379")
	if err := rs.Ping(context.Background()); err != nil {
		t.Skipf("Redis not available, skipping: %v", err)
	}
	t.Cleanup(func() { rs.Close() })
	return rs
}

func TestRedisTokenBucket_AllowsUpToCapacity(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:tb:allow"

	// flush any state from previous runs
	rs.client.Del(ctx, "rl:tb:"+key)

	for i := 0; i < 5; i++ {
		res, err := rs.TokenBucketAllow(ctx, key, 5, 1.0)
		if err != nil {
			t.Fatalf("unexpected error on request %d: %v", i+1, err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRedisTokenBucket_DeniesAtCapacity(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:tb:deny"

	rs.client.Del(ctx, "rl:tb:"+key)

	for i := 0; i < 5; i++ {
		rs.TokenBucketAllow(ctx, key, 5, 1.0)
	}

	res, err := rs.TokenBucketAllow(ctx, key, 5, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("6th request should be denied")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("denied result must include positive RetryAfter")
	}
}

func TestRedisTokenBucket_RefillsOverTime(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:tb:refill"

	rs.client.Del(ctx, "rl:tb:"+key)

	// drain with fast refill rate
	for i := 0; i < 3; i++ {
		rs.TokenBucketAllow(ctx, key, 3, 10.0)
	}

	time.Sleep(150 * time.Millisecond)

	res, err := rs.TokenBucketAllow(ctx, key, 3, 10.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("should be allowed after refill period")
	}
}

func TestRedisSlidingWindow_AllowsUpToLimit(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:sw:allow"

	rs.client.Del(ctx, "rl:sw:"+key)

	for i := 0; i < 5; i++ {
		res, err := rs.SlidingWindowAllow(ctx, key, 5, time.Minute)
		if err != nil {
			t.Fatalf("unexpected error on request %d: %v", i+1, err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRedisSlidingWindow_DeniesAtLimit(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:sw:deny"

	rs.client.Del(ctx, "rl:sw:"+key)

	for i := 0; i < 5; i++ {
		rs.SlidingWindowAllow(ctx, key, 5, time.Minute)
	}

	res, err := rs.SlidingWindowAllow(ctx, key, 5, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("6th request should be denied")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("denied result must include positive RetryAfter")
	}
}

func TestRedisSlidingWindow_AllowsAfterWindowExpires(t *testing.T) {
	rs := newTestStore(t)
	ctx := context.Background()
	key := "test:sw:expire"

	rs.client.Del(ctx, "rl:sw:"+key)

	rs.SlidingWindowAllow(ctx, key, 2, 200*time.Millisecond)
	rs.SlidingWindowAllow(ctx, key, 2, 200*time.Millisecond)

	res, _ := rs.SlidingWindowAllow(ctx, key, 2, 200*time.Millisecond)
	if res.Allowed {
		t.Fatal("should be denied while window is full")
	}

	time.Sleep(250 * time.Millisecond)

	res, err := rs.SlidingWindowAllow(ctx, key, 2, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("should be allowed after window expires")
	}
}