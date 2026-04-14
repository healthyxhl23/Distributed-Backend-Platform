package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	nethttp "github.com/herryxhl/rate-limiter/api/http"
	"github.com/herryxhl/rate-limiter/internal/config"
	"github.com/herryxhl/rate-limiter/internal/limiter"
	"github.com/herryxhl/rate-limiter/internal/store"
)

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	port      := getEnv("PORT", "8080")
	// KV Store HTTP bridge addresses — comma-separated
	// Default: 5 nodes on ports 2099-2103 (RMI ports 1099-1103 + 1000)
	kvAddrs := strings.Split(
		getEnv("KV_ADDRS", "localhost:2099,localhost:2100,localhost:2101,localhost:2102,localhost:2103"),
		",",
	)

	ctx := context.Background()

	// connect to Redis
	rs := store.NewRedisStore(redisAddr)
	if err := rs.Ping(ctx); err != nil {
		log.Fatalf("cannot reach Redis at %s: %v", redisAddr, err)
	}
	log.Printf("connected to Redis at %s", redisAddr)
	defer rs.Close()

	// connect to KV Store config loader — non-fatal if KV Store is down,
	// the loader will fall back to hardcoded defaults automatically
	cfgLoader := config.NewLoader(kvAddrs, 30*time.Second)
	if err := cfgLoader.Ping(ctx); err != nil {
		log.Printf("WARNING: KV Store unreachable (%v) — using default configs", err)
	} else {
		log.Printf("connected to KV Store at %v", kvAddrs)
	}

	// load initial configs from KV Store (or defaults if unreachable)
	defaultCfg := cfgLoader.Get(ctx, "default")
	freeCfg    := cfgLoader.Get(ctx, "free")
	premiumCfg := cfgLoader.Get(ctx, "premium")

	log.Printf("loaded configs — default: %+v | free: %+v | premium: %+v",
		defaultCfg, freeCfg, premiumCfg)

	// wire up limiters using KV Store configs
	limiters := map[string]limiter.Limiter{
		"token_bucket":   limiter.NewRedisTokenBucketLimiter(rs, defaultCfg.Limit, float64(defaultCfg.Limit)/60.0),
		"sliding_window": limiter.NewRedisSlidingWindowLimiter(rs, defaultCfg.Limit, defaultCfg.WindowSize),
	}

	h := nethttp.NewHandler(limiters, rs)
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      h.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// start server in background goroutine
	go func() {
		log.Printf("rate-limiter listening on :%s", port)
		log.Printf("endpoints: POST /check  GET /health  GET /ready  GET /metrics")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// block until SIGINT or SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("shutdown complete")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}