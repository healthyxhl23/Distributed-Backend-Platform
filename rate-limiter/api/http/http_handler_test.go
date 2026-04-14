package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/herryxhl/rate-limiter/internal/limiter"
)

// mockLimiter lets tests control the Allow() response without Redis.
type mockLimiter struct {
	result limiter.Result
	err    error
}

func (m *mockLimiter) Allow(_ context.Context, _ string) (limiter.Result, error) {
	return m.result, m.err
}

// newTestHandler wires a handler with a mock limiter.
// allowed=true  → limiter always allows
// allowed=false → limiter always denies with a 2s retry
func newTestHandler(allowed bool) *Handler {
	res := limiter.Result{Limit: 100}
	if allowed {
		res.Allowed = true
		res.Remaining = 42
	} else {
		res.Allowed = false
		res.Remaining = 0
		res.RetryAfter = 2 * time.Second
	}
	mock := &mockLimiter{result: res}
	return NewHandler(map[string]limiter.Limiter{
		"token_bucket":   mock,
		"sliding_window": mock,
	}, nil)
}

func TestCheck_AllowedResponse(t *testing.T) {
	h := newTestHandler(true)
	body, _ := json.Marshal(CheckRequest{Key: "user:test", Algorithm: "token_bucket"})
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CheckResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Allowed {
		t.Fatal("expected allowed=true")
	}
	if resp.Remaining != 42 {
		t.Errorf("expected remaining=42, got %d", resp.Remaining)
	}
	if resp.RetryAfter != "" {
		t.Error("retry_after should be absent when allowed")
	}
}

func TestCheck_DeniedResponse(t *testing.T) {
	h := newTestHandler(false)
	body, _ := json.Marshal(CheckRequest{Key: "user:test", Algorithm: "token_bucket"})
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	var resp CheckResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Allowed {
		t.Fatal("expected allowed=false")
	}
	if resp.RetryAfter == "" {
		t.Error("retry_after should be present when denied")
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header should be set on 429")
	}
	if w.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("X-RateLimit-Limit header should always be set")
	}
}

func TestCheck_DefaultsToTokenBucket(t *testing.T) {
	h := newTestHandler(true)
	// no algorithm field — should default to token_bucket
	body, _ := json.Marshal(CheckRequest{Key: "user:test"})
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCheck_UnknownAlgorithmReturns400(t *testing.T) {
	h := newTestHandler(true)
	body, _ := json.Marshal(CheckRequest{Key: "user:test", Algorithm: "magic"})
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCheck_MissingKeyReturns400(t *testing.T) {
	h := newTestHandler(true)
	body, _ := json.Marshal(CheckRequest{Algorithm: "token_bucket"}) // no key
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCheck_WrongMethodReturns405(t *testing.T) {
	h := newTestHandler(true)
	r := httptest.NewRequest(http.MethodGet, "/check", nil)
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHealth_Returns200(t *testing.T) {
	h := newTestHandler(true)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestReady_Returns503WhenStoreIsNil(t *testing.T) {
	h := newTestHandler(true) // nil store
	r := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestCheck_SlidingWindowAlgorithm(t *testing.T) {
	h := newTestHandler(true)
	body, _ := json.Marshal(CheckRequest{Key: "user:test", Algorithm: "sliding_window"})
	r := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
