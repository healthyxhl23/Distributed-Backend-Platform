package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/herryxhl/rate-limiter/internal/limiter"
	"github.com/herryxhl/rate-limiter/internal/store"
	"github.com/herryxhl/rate-limiter/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// CheckRequest is the body expected on POST /check.
type CheckRequest struct {
	Key       string `json:"key"`
	Algorithm string `json:"algorithm"` // "token_bucket" | "sliding_window"
}

// CheckResponse is returned for every /check call.
type CheckResponse struct {
	Allowed    bool   `json:"allowed"`
	Remaining  int    `json:"remaining"`
	Limit      int    `json:"limit"`
	RetryAfter string `json:"retry_after,omitempty"` // only present when denied
}

// Handler holds the router dependencies.
type Handler struct {
	limiters map[string]limiter.Limiter
	store    *store.RedisStore
}

// NewHandler creates a Handler with the given algorithm map and store.
// limiters keys must be "token_bucket" and/or "sliding_window".
func NewHandler(limiters map[string]limiter.Limiter, s *store.RedisStore) *Handler {
	return &Handler{limiters: limiters, store: s}
}

// Routes registers all endpoints and returns the mux.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/check", h.handleCheck)
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/ready", h.handleReady)
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// handleCheck is the core endpoint.
// POST /check  {"key": "user:herry:endpoint:/api/transactions", "algorithm": "token_bucket"}
func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}
	if req.Algorithm == "" {
		req.Algorithm = "token_bucket"
	}

	lim, ok := h.limiters[req.Algorithm]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown algorithm: %s", req.Algorithm), http.StatusBadRequest)
		return
	}

	start := time.Now()
	result, err := lim.Allow(r.Context(), req.Key)
	elapsed := time.Since(start)

	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// record metrics
	resultLabel := "allowed"
	if !result.Allowed {
		resultLabel = "denied"
		metrics.DeniedTotal.WithLabelValues(req.Algorithm).Inc()
	}
	metrics.RequestsTotal.WithLabelValues(req.Algorithm, resultLabel).Inc()
	metrics.CheckDuration.WithLabelValues(req.Algorithm).Observe(elapsed.Seconds())

	// standard rate limit response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))

	resp := CheckResponse{
		Allowed:   result.Allowed,
		Remaining: result.Remaining,
		Limit:     result.Limit,
	}

	if !result.Allowed {
		// Retry-After header = seconds to wait (rounded up)
		retrySeconds := int(result.RetryAfter.Seconds()) + 1
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySeconds))
		resp.RetryAfter = result.RetryAfter.Round(time.Millisecond).String()
		w.WriteHeader(http.StatusTooManyRequests)
	}

	json.NewEncoder(w).Encode(resp)
}

// handleHealth is a liveness check — always 200 if the process is alive.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleReady is a readiness check — 503 if Redis is unreachable.
// Kubernetes uses this to decide whether to send traffic to this pod.
func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if h.store == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
		return
	}

	if err := h.store.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
