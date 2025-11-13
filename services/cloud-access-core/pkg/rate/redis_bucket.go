package rate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Result captures the outcome of a token bucket evaluation.
type Result struct {
	Allowed    bool
	Remaining  float64
	RetryAfter time.Duration
}

// TokenBucket defines the behaviour required by policy/limit evaluators.
type TokenBucket interface {
	Take(ctx context.Context, key string, capacity float64, refillRate float64, amount float64, ttl time.Duration) (Result, error)
}

// RedisTokenBucket implements a distributed token bucket backed by Redis.
type RedisTokenBucket struct {
	client *redis.Client
	prefix string
}

// NewRedisTokenBucket builds a Redis-backed token bucket helper.
func NewRedisTokenBucket(client *redis.Client, prefix string) *RedisTokenBucket {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "cloud-access-core:quota"
	}
	return &RedisTokenBucket{client: client, prefix: prefix}
}

var bucketScript = redis.NewScript(`
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local amount = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])
local key = KEYS[1]

if amount <= 0 then
    return {1, capacity, 0}
end

local tokens = capacity
local last = now
local data = redis.call('HMGET', key, 'tokens', 'timestamp')
if data[1] and data[2] then
    tokens = tonumber(data[1])
    last = tonumber(data[2])
end

if not tokens then tokens = capacity end
if not last then last = now end

local elapsed = now - last
if elapsed < 0 then elapsed = 0 end

if refill > 0 then
    tokens = math.min(capacity, tokens + (elapsed * refill))
else
    tokens = capacity
end

local allowed = 0
local retry = 0
if tokens >= amount then
    allowed = 1
    tokens = tokens - amount
else
    if refill > 0 then
        retry = math.floor(((amount - tokens) / refill) + 0.999)
    else
        retry = ttl
    end
end

redis.call('HMSET', key, 'tokens', tokens, 'timestamp', now)
redis.call('PEXPIRE', key, ttl)
return {allowed, tokens, retry}
`)

// Take attempts to consume amount tokens from the configured bucket.
func (b *RedisTokenBucket) Take(ctx context.Context, key string, capacity float64, refillRate float64, amount float64, ttl time.Duration) (Result, error) {
	if capacity <= 0 || amount <= 0 || b == nil || b.client == nil {
		return Result{Allowed: true, Remaining: math.Max(capacity-amount, 0)}, nil
	}
	if refillRate <= 0 {
		refillRate = capacity
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	redisKey := fmt.Sprintf("%s:%s", b.prefix, key)
	now := time.Now().Unix()
	res, err := bucketScript.Run(ctx, b.client, []string{redisKey}, capacity, refillRate, amount, now, ttl.Milliseconds()).Result()
	if err != nil {
		return Result{}, err
	}
	vals, ok := res.([]any)
	if !ok || len(vals) != 3 {
		return Result{}, errors.New("unexpected token bucket response")
	}
	allowedFloat, err := toFloat(vals[0])
	if err != nil {
		return Result{}, err
	}
	remaining, err := toFloat(vals[1])
	if err != nil {
		return Result{}, err
	}
	retryMillis, err := toFloat(vals[2])
	if err != nil {
		return Result{}, err
	}
	retryAfter := time.Duration(retryMillis) * time.Millisecond
	return Result{Allowed: allowedFloat >= 1, Remaining: remaining, RetryAfter: retryAfter}, nil
}

func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case int64:
		return float64(val), nil
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", v)
	}
}
