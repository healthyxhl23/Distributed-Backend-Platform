# Rate Limiter

A production-grade distributed rate limiting service written in Go. Supports two algorithms (token bucket and sliding window), is backed by atomic Redis Lua scripts for cross-instance correctness, and reads its configuration from a Paxos-replicated KV store.

## Architecture

```
Client Request
      │
      ▼
┌─────────────────────────────────┐
│         Rate Limiter (Go)        │
│                                  │
│  POST /check                     │
│       │                          │
│  Middleware (auth · log · trace) │
│       │                          │
│  Limiter Interface               │
│  ┌────────────┬────────────────┐ │
│  │Token Bucket│ Sliding Window │ │
│  └────────────┴────────────────┘ │
└──────┬──────────────┬────────────┘
       │              │
       ▼              ▼
   Redis            KV Store
(live counters)   (rate configs)
  Lua scripts      HTTP bridge
  atomic ops       Paxos consensus
                   5-node cluster
```

**Redis** handles live request counters. Every check-and-increment is a single atomic Lua script — no race conditions under concurrent load.

**KV Store** holds the rate limit rules (e.g. `{"limit":100,"window_ms":60000,"algorithm":"token_bucket"}`). It is a separate Paxos-replicated Java service that guarantees strongly consistent config reads. The Go service caches configs locally for 30 seconds and falls back to hardcoded defaults if the KV Store is unreachable.

## Performance

Benchmarked on Apple M4 with a local Redis instance:

| Scenario | Latency | Throughput |
|---|---|---|
| Token bucket sequential | 23 µs | 43K ops/sec |
| Token bucket parallel | 12 µs | 86K ops/sec |
| Sliding window sequential | 24 µs | 41K ops/sec |
| Sliding window parallel | 11 µs | 89K ops/sec |
| Multi-key parallel (100 keys) | 12 µs | **80K ops/sec** |

In-memory algorithms (no Redis) run at **5–19M ops/sec** with zero heap allocations on the token bucket implementation.

## Algorithms

**Token bucket** — each key has a bucket that fills at a fixed rate and empties per request. Good for endpoints where short bursts are acceptable. A user can accumulate tokens while idle and spend them quickly.

**Sliding window** — counts exact requests in a rolling time window using a Redis sorted set. Strictly enforces the limit regardless of idle time. Good for cost-sensitive endpoints (e.g. Plaid API calls, LLM inference) where every request has a real cost.

## Project structure

```
rate-limiter/
├── cmd/server/
│   └── main.go                  entry point, graceful shutdown
├── api/http/
│   ├── handler.go               POST /check, /health, /ready, /metrics
│   └── handler_test.go          9 unit tests (no Redis required)
├── internal/
│   ├── limiter/
│   │   ├── limiter.go           Limiter interface + Result type
│   │   ├── token_bucket.go      in-memory token bucket
│   │   ├── sliding_window.go    in-memory sliding window
│   │   ├── redis_limiters.go    Redis-backed production wrappers
│   │   ├── token_bucket_test.go
│   │   ├── sliding_window_test.go
│   │   └── benchmark_test.go
│   ├── store/
│   │   ├── redis.go             Lua scripts + Redis client
│   │   ├── redis_test.go        integration tests
│   │   └── benchmark_test.go
│   └── config/
│       ├── loader.go            KV Store config loader + cache
│       └── loader_test.go       7 tests with mock HTTP server
└── metrics/
    └── prometheus.go            request counters + latency histograms
```

## Getting started

**Prerequisites**

- Go 1.21+
- Redis (`brew install redis && brew services start redis`)
- KV Store (optional — service falls back to defaults if unreachable)

**Run**

```bash
# clone and install dependencies
git clone https://github.com/herryxhl/rate-limiter
cd rate-limiter
go mod download

# start the server (defaults: port 8080, Redis at localhost:6379)
go run cmd/server/main.go

# with custom config
REDIS_ADDR=localhost:6379 \
KV_ADDRS=localhost:2099,localhost:2100 \
PORT=8080 \
go run cmd/server/main.go
```

## API

### POST /check

Check whether a request should be allowed.

```bash
curl -X POST localhost:8080/check \
  -H "Content-Type: application/json" \
  -d '{"key":"user:herry","algorithm":"token_bucket"}'
```

**Allowed (200):**
```json
{"allowed":true,"remaining":99,"limit":100}
```

**Denied (429):**
```json
{"allowed":false,"remaining":0,"limit":100,"retry_after":"1.2s"}
```

Response headers:
- `X-RateLimit-Limit` — configured limit
- `X-RateLimit-Remaining` — requests remaining in current window
- `Retry-After` — seconds to wait before retrying (only on 429)

### GET /health

Liveness check. Returns 200 if the process is alive.

### GET /ready

Readiness check. Returns 503 if Redis is unreachable.

### GET /metrics

Prometheus scrape endpoint. Key metrics:

- `rate_limit_requests_total{algorithm, result}` — total checks by algorithm and outcome
- `rate_limit_check_duration_seconds{algorithm}` — latency histogram per algorithm
- `rate_limit_denied_total{algorithm}` — denied request counter

## Configuration

Rate limit rules are stored in the KV Store under `rate:config:<tier>`:

```bash
# set via KV Store client
put,rate:config:free,{"limit":50,"window_ms":60000,"algorithm":"sliding_window"},client1
put,rate:config:premium,{"limit":1000,"window_ms":60000,"algorithm":"token_bucket"},client1
```

Changes propagate to the rate limiter within 30 seconds (cache TTL). If the KV Store is unreachable, the service continues running with these defaults:

| Tier | Limit | Window | Algorithm |
|---|---|---|---|
| default | 100 | 60s | token_bucket |
| free | 50 | 60s | sliding_window |
| premium | 1000 | 60s | token_bucket |

## Running tests

```bash
# all unit tests (no external dependencies)
go test ./... -v

# with race detector
go test -race ./...

# integration tests (requires Redis)
go test ./internal/store/... -v

# in-memory benchmarks
go test -bench=. -benchmem -benchtime=5s ./internal/limiter/...

# Redis benchmarks
go test -bench=. -benchmem -benchtime=10s ./internal/store/...
```

## Design decisions

**Why Lua scripts for Redis?** A naive implementation reads the counter, checks the limit, and increments in separate commands. Under concurrent load, two goroutines can both read "99", both pass the check, and both increment to 100 — exceeding the limit. Lua scripts run atomically on Redis, making the entire check-and-update a single uninterruptible operation.

**Why a strategy pattern for algorithms?** The HTTP handler calls `Allow(ctx, key)` on a `Limiter` interface without knowing which algorithm runs underneath. This means you can swap algorithms per endpoint, add new ones without touching the handler, and unit test each independently with a mock.

**Why cache KV Store configs locally?** The KV Store uses Paxos consensus — it prioritizes correctness over speed. Querying it on every request would add 20–25ms of latency per check. A 30-second local cache reduces KV Store load by ~99% while still picking up config changes within a reasonable window.

**Why separate in-memory and Redis implementations?** In-memory implementations run in unit tests without any infrastructure. Redis implementations run in integration tests and production. The same interface covers both — the test suite doesn't need Redis to validate algorithm correctness.