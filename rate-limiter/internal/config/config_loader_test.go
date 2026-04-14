package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockKVStore spins up a test HTTP server that mimics the Java KV Store bridge.
func mockKVStore(t *testing.T, data map[string]RateConfig) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		key := r.URL.Query().Get("key")
		// strip "rate:config:" prefix to look up in test data
		tier := key
		if len(key) > 12 {
			tier = key[12:] // len("rate:config:") == 12
		}
		cfg, ok := data[tier]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}))
}

func TestLoader_FetchesFromKVStore(t *testing.T) {
	srv := mockKVStore(t, map[string]RateConfig{
		"free": {Limit: 50, WindowMS: 60000, Algorithm: "sliding_window"},
	})
	defer srv.Close()

	loader := NewLoader([]string{srv.Listener.Addr().String()}, 30*time.Second)
	cfg := loader.Get(context.Background(), "free")

	if cfg.Limit != 50 {
		t.Errorf("expected limit=50, got %d", cfg.Limit)
	}
	if cfg.Algorithm != "sliding_window" {
		t.Errorf("expected algorithm=sliding_window, got %s", cfg.Algorithm)
	}
	if cfg.WindowSize != 60*time.Second {
		t.Errorf("expected windowSize=60s, got %s", cfg.WindowSize)
	}
}

func TestLoader_CacheHitSkipsNetwork(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		callCount++
		json.NewEncoder(w).Encode(RateConfig{Limit: 100, WindowMS: 60000, Algorithm: "token_bucket"})
	}))
	defer srv.Close()

	loader := NewLoader([]string{srv.Listener.Addr().String()}, 30*time.Second)
	ctx := context.Background()

	loader.Get(ctx, "default")
	loader.Get(ctx, "default") // should hit cache
	loader.Get(ctx, "default") // should hit cache

	if callCount != 1 {
		t.Errorf("expected 1 network call, got %d — cache not working", callCount)
	}
}

func TestLoader_FallsBackToDefaultWhenKVUnreachable(t *testing.T) {
	// point to a port nothing is listening on
	loader := NewLoader([]string{"localhost:19999"}, 30*time.Second)
	cfg := loader.Get(context.Background(), "free")

	// should return the hardcoded default for "free"
	if cfg.Limit != defaults["free"].Limit {
		t.Errorf("expected fallback limit=%d, got %d", defaults["free"].Limit, cfg.Limit)
	}
	if cfg.Algorithm != defaults["free"].Algorithm {
		t.Errorf("expected fallback algorithm=%s, got %s", defaults["free"].Algorithm, cfg.Algorithm)
	}
}

func TestLoader_FallsBackToDefaultForUnknownTier(t *testing.T) {
	// KV Store returns 404 for unknown tier
	srv := mockKVStore(t, map[string]RateConfig{}) // empty store
	defer srv.Close()

	loader := NewLoader([]string{srv.Listener.Addr().String()}, 30*time.Second)
	cfg := loader.Get(context.Background(), "enterprise")

	// should fall back to "default"
	if cfg.Limit != defaults["default"].Limit {
		t.Errorf("expected default limit=%d, got %d", defaults["default"].Limit, cfg.Limit)
	}
}

func TestLoader_TriesMultipleNodes(t *testing.T) {
	// first address is dead, second is alive
	alive := mockKVStore(t, map[string]RateConfig{
		"premium": {Limit: 1000, WindowMS: 60000, Algorithm: "token_bucket"},
	})
	defer alive.Close()

	loader := NewLoader([]string{
		"localhost:19998",             // dead
		alive.Listener.Addr().String(), // alive
	}, 30*time.Second)

	cfg := loader.Get(context.Background(), "premium")

	if cfg.Limit != 1000 {
		t.Errorf("expected limit=1000 from second node, got %d", cfg.Limit)
	}
}

func TestLoader_PingSucceedsWhenNodeAlive(t *testing.T) {
	srv := mockKVStore(t, map[string]RateConfig{})
	defer srv.Close()

	loader := NewLoader([]string{srv.Listener.Addr().String()}, 30*time.Second)
	if err := loader.Ping(context.Background()); err != nil {
		t.Errorf("expected ping to succeed, got: %v", err)
	}
}

func TestLoader_PingFailsWhenAllNodesDead(t *testing.T) {
	loader := NewLoader([]string{"localhost:19997", "localhost:19996"}, 30*time.Second)
	if err := loader.Ping(context.Background()); err == nil {
		t.Error("expected ping to fail when all nodes unreachable")
	}
}
