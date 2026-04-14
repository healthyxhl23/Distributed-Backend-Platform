package limiter

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket_AllowsUpToCapacity(t *testing.T) {
	// capacity=5, refillRate=1 token/sec
	// first 5 requests should all be allowed
	lim := NewTokenBucketLimiter(5, 1.0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		res, err := lim.Allow(ctx, "user:test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Allowed {
			t.Fatalf("request %d should be allowed, got denied", i+1)
		}
	}
}

func TestTokenBucket_DeniesAfterCapacity(t *testing.T) {
	// 6th request should be denied — bucket is empty
	lim := NewTokenBucketLimiter(5, 1.0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		lim.Allow(ctx, "user:test")
	}

	res, err := lim.Allow(ctx, "user:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("6th request should be denied but was allowed")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("denied result should include a positive RetryAfter")
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	// refillRate=10 tokens/sec — after 200ms, ~2 tokens should have refilled
	lim := NewTokenBucketLimiter(5, 10.0)
	ctx := context.Background()

	// drain the bucket
	for range 5 {
		lim.Allow(ctx, "user:test")
	}

	// wait for refill
	time.Sleep(200 * time.Millisecond)

	res, err := lim.Allow(ctx, "user:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("request should be allowed after refill period")
	}
}

func TestTokenBucket_IsolatesKeys(t *testing.T) {
	// draining one key's bucket should not affect another key
	lim := NewTokenBucketLimiter(2, 1.0)
	ctx := context.Background()

	// drain user A
	lim.Allow(ctx, "user:A")
	lim.Allow(ctx, "user:A")

	// user B should still have a full bucket
	res, err := lim.Allow(ctx, "user:B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("user:B should not be affected by user:A's bucket")
	}
}

func TestTokenBucket_RemainingDecrementsCorrectly(t *testing.T) {
	lim := NewTokenBucketLimiter(3, 1.0)
	ctx := context.Background()

	res1, _ := lim.Allow(ctx, "user:test")
	res2, _ := lim.Allow(ctx, "user:test")
	res3, _ := lim.Allow(ctx, "user:test")

	if res1.Remaining != 2 {
		t.Errorf("after 1st request: expected remaining=2, got %d", res1.Remaining)
	}
	if res2.Remaining != 1 {
		t.Errorf("after 2nd request: expected remaining=1, got %d", res2.Remaining)
	}
	if res3.Remaining != 0 {
		t.Errorf("after 3rd request: expected remaining=0, got %d", res3.Remaining)
	}
}

func TestTokenBucket_ConcurrentAccess(t *testing.T) {
	// fire 100 goroutines simultaneously — should not panic or race
	// run with: go test -race ./internal/limiter/...
	lim := NewTokenBucketLimiter(100, 10.0)
	ctx := context.Background()
	done := make(chan struct{}, 100)

	for range 100 {
		go func() {
			lim.Allow(ctx, "user:concurrent")
			done <- struct{}{}
		}()
	}

	for range 100 {
		<-done
	}
}