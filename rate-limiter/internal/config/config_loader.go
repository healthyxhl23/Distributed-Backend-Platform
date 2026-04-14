package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// RateConfig holds the rate limit rules for a given tier or key.
// Stored in the KV Store as JSON:
//   {"limit":100,"window_ms":60000,"algorithm":"token_bucket"}
type RateConfig struct {
	Limit      int           `json:"limit"`
	WindowMS   int64         `json:"window_ms"`
	Algorithm  string        `json:"algorithm"`
	WindowSize time.Duration `json:"-"` // derived from WindowMS after decode
}

// defaults are used when the KV Store is unreachable or the key is not found.
// These mirror what Server.java pre-populates on startup.
var defaults = map[string]RateConfig{
	"default": {Limit: 100, WindowMS: 60000, Algorithm: "token_bucket"},
	"free":    {Limit: 50, WindowMS: 60000, Algorithm: "sliding_window"},
	"premium": {Limit: 1000, WindowMS: 60000, Algorithm: "token_bucket"},
}

type cachedEntry struct {
	config    RateConfig
	expiresAt time.Time
}

// Loader fetches rate limit configs from the KV Store HTTP bridge,
// caches them locally, and falls back to hardcoded defaults on error.
//
// The KV Store stores configs under the key "rate:config:<tier>".
// The Loader prefixes the key automatically — callers just pass the tier.
//
// Thread-safe — safe to call concurrently from multiple goroutines.
type Loader struct {
	addrs    []string          // HTTP bridge addresses e.g. ["localhost:2099","localhost:2100"]
	cacheTTL time.Duration     // how long to cache a fetched config (e.g. 30s)
	cache    map[string]cachedEntry
	mu       sync.RWMutex
	client   *http.Client
}

// NewLoader creates a Loader that reads from the given KV Store HTTP bridge
// addresses, caching each result for cacheTTL.
func NewLoader(addrs []string, cacheTTL time.Duration) *Loader {
	return &Loader{
		addrs:    addrs,
		cacheTTL: cacheTTL,
		cache:    make(map[string]cachedEntry),
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

// Get returns the RateConfig for the given tier (e.g. "free", "premium", "default").
// Cache hit  → returns immediately without a network call.
// Cache miss → fetches from KV Store, writes to cache, returns result.
// Error      → logs and returns the hardcoded default for that tier.
func (l *Loader) Get(ctx context.Context, tier string) RateConfig {
	// fast path — read lock only
	l.mu.RLock()
	if entry, ok := l.cache[tier]; ok && time.Now().Before(entry.expiresAt) {
		l.mu.RUnlock()
		return entry.config
	}
	l.mu.RUnlock()

	// slow path — fetch from KV Store
	cfg, err := l.fetchFromKV(ctx, tier)
	if err != nil {
		cfg = l.defaultFor(tier)
	}

	// write to cache under write lock
	l.mu.Lock()
	l.cache[tier] = cachedEntry{
		config:    cfg,
		expiresAt: time.Now().Add(l.cacheTTL),
	}
	l.mu.Unlock()

	return cfg
}

// Ping checks whether at least one KV Store node is reachable.
// Call this at startup to warn if the KV Store is unavailable.
func (l *Loader) Ping(ctx context.Context) error {
	for _, addr := range l.addrs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("http://%s/health", addr), nil)
		if err != nil {
			continue
		}
		resp, err := l.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil // at least one node is up
		}
	}
	return fmt.Errorf("all KV Store nodes unreachable: %v", l.addrs)
}

// fetchFromKV tries each KV Store address in order until one responds.
// The KV Store key is "rate:config:<tier>".
func (l *Loader) fetchFromKV(ctx context.Context, tier string) (RateConfig, error) {
	kvKey := "rate:config:" + tier

	for _, addr := range l.addrs {
		endpoint := fmt.Sprintf("http://%s/get?key=%s",
			addr, url.QueryEscape(kvKey))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}

		resp, err := l.client.Do(req)
		if err != nil {
			continue // node unreachable, try next
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return RateConfig{}, fmt.Errorf("key not found in KV Store: %s", kvKey)
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var cfg RateConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			return RateConfig{}, fmt.Errorf("parse config for %s: %w", kvKey, err)
		}

		// derive WindowSize from WindowMS
		cfg.WindowSize = time.Duration(cfg.WindowMS) * time.Millisecond
		return cfg, nil
	}

	return RateConfig{}, fmt.Errorf("all KV Store nodes unreachable")
}

// defaultFor returns the hardcoded default for a tier, falling back to "default".
func (l *Loader) defaultFor(tier string) RateConfig {
	if cfg, ok := defaults[tier]; ok {
		cfg.WindowSize = time.Duration(cfg.WindowMS) * time.Millisecond
		return cfg
	}
	cfg := defaults["default"]
	cfg.WindowSize = time.Duration(cfg.WindowMS) * time.Millisecond
	return cfg
}
