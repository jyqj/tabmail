// Package ratelimit owns atomic admission against one Redis sliding window.
// Callers retain their own key scopes, limit configuration and failure policy.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Keep every attempt, including denied ones, in the existing rolling history.
// A pipeline only batches commands; the count and add must execute together
// so two clients cannot both admit against the same remaining capacity.
var slidingWindow = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '0', ARGV[1])
local count = redis.call('ZCARD', KEYS[1])
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return count
`)

func Allow(ctx context.Context, client *redis.Client, key string, limit int, window time.Duration) (bool, error) {
	if client == nil {
		return true, nil
	}
	now := time.Now().UnixMilli()
	// Match go-redis Expire's whole-second conversion, including its minimum
	// positive expiry, while preserving the original window+one-second TTL.
	ttl := window + time.Second
	expirySeconds := int64(ttl / time.Second)
	if ttl > 0 && ttl < time.Second {
		expirySeconds = 1
	}
	count, err := slidingWindow.Run(ctx, client, []string{key},
		now-window.Milliseconds(), now,
		fmt.Sprintf("%d:%s", now, uuid.NewString()), expirySeconds,
	).Int64()
	if err != nil {
		return false, err
	}
	return count < int64(limit), nil
}
