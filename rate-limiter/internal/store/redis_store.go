package store

import (
	"context"
	"fmt"
	"time"

	"github.com/herryxhl/rate-limiter/internal/ratelimit"
	"github.com/redis/go-redis/v9"
)

// tokenBucketLua runs atomically on Redis.
// KEYS[1] = rate limit key
// ARGV[1] = capacity      (int, max tokens)
// ARGV[2] = refill_rate   (float, tokens per second)
// ARGV[3] = now           (int, unix milliseconds)
// Returns: {allowed(0/1), remaining, limit, retry_after_ms}
const tokenBucketLua = `
local key        = KEYS[1]
local capacity   = tonumber(ARGV[1])
local rate       = tonumber(ARGV[2])
local now        = tonumber(ARGV[3])

local data       = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens     = tonumber(data[1])
local last_refill = tonumber(data[2])

if tokens == nil then
    tokens     = capacity
    last_refill = now
end

local elapsed = (now - last_refill) / 1000.0
tokens = math.min(capacity, tokens + elapsed * rate)

local ttl = math.ceil(capacity / rate) + 1

if tokens >= 1 then
    tokens = tokens - 1
    redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
    redis.call('EXPIRE', key, ttl)
    return {1, math.floor(tokens), capacity, 0}
end

local retry_ms = math.ceil((1 - tokens) / rate * 1000)
redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
redis.call('EXPIRE', key, ttl)
return {0, 0, capacity, retry_ms}
`

// slidingWindowLua runs atomically on Redis using a sorted set.
// Each request is stored as a member with its timestamp as the score.
// Expired entries are evicted before every check.
// KEYS[1] = rate limit key
// ARGV[1] = limit       (int, max requests per window)
// ARGV[2] = window_ms   (int, window duration in milliseconds)
// ARGV[3] = now         (int, unix milliseconds)
// Returns: {allowed(0/1), remaining, limit, retry_after_ms}
const slidingWindowLua = `
local key       = KEYS[1]
local limit     = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now       = tonumber(ARGV[3])
local cutoff    = now - window_ms

redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

local count = redis.call('ZCARD', key)

if count < limit then
    local member = tostring(now) .. '-' .. tostring(math.random(999999))
    redis.call('ZADD', key, now, member)
    redis.call('PEXPIRE', key, window_ms)
    return {1, limit - count - 1, limit, 0}
end

local oldest      = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
local oldest_score = tonumber(oldest[2]) or now
local retry_ms    = oldest_score + window_ms - now
return {0, 0, limit, retry_ms}
`

// RedisStore wraps a Redis client and exposes atomic rate limit
// checks via Lua scripts. Lua guarantees the check-and-update
// is a single atomic operation — no race conditions under concurrency.
type RedisStore struct {
	client   *redis.Client
	tbScript *redis.Script
	swScript *redis.Script
}

// NewRedisStore creates a RedisStore connected to addr (e.g. "localhost:6379").
func NewRedisStore(addr string) *RedisStore {
	return &RedisStore{
		client:   redis.NewClient(&redis.Options{Addr: addr}),
		tbScript: redis.NewScript(tokenBucketLua),
		swScript: redis.NewScript(slidingWindowLua),
	}
}

// Ping checks that Redis is reachable. Call this at service startup.
func (rs *RedisStore) Ping(ctx context.Context) error {
	return rs.client.Ping(ctx).Err()
}

// Close releases the underlying Redis connection.
func (rs *RedisStore) Close() error {
	return rs.client.Close()
}

// TokenBucketAllow atomically checks and updates a token bucket in Redis.
func (rs *RedisStore) TokenBucketAllow(
	ctx context.Context,
	key string,
	capacity int,
	refillRate float64,
) (ratelimit.Result, error) {
	now := time.Now().UnixMilli()
	raw, err := rs.tbScript.Run(
		ctx, rs.client,
		[]string{"rl:tb:" + key},
		capacity, refillRate, now,
	).Int64Slice()
	if err != nil {
		return ratelimit.Result{}, fmt.Errorf("token bucket script: %w", err)
	}
	return parseResult(raw), nil
}

// SlidingWindowAllow atomically checks and updates a sliding window in Redis.
func (rs *RedisStore) SlidingWindowAllow(
	ctx context.Context,
	key string,
	limit int,
	windowSize time.Duration,
) (ratelimit.Result, error) {
	now := time.Now().UnixMilli()
	raw, err := rs.swScript.Run(
		ctx, rs.client,
		[]string{"rl:sw:" + key},
		limit, windowSize.Milliseconds(), now,
	).Int64Slice()
	if err != nil {
		return ratelimit.Result{}, fmt.Errorf("sliding window script: %w", err)
	}
	return parseResult(raw), nil
}

// parseResult converts the 4-element Lua response into a Result.
// Lua returns: [allowed(0/1), remaining, limit, retry_after_ms]
func parseResult(raw []int64) ratelimit.Result {
	if len(raw) < 4 {
		return ratelimit.Result{}
	}
	return ratelimit.Result{
		Allowed:    raw[0] == 1,
		Remaining:  int(raw[1]),
		Limit:      int(raw[2]),
		RetryAfter: time.Duration(raw[3]) * time.Millisecond,
	}
}
