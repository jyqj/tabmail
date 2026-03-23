package autocreate

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb       *redis.Client
	routeRPM  int
	tenantRPM int
	window    time.Duration
}

func NewLimiter(rdb *redis.Client, routeRPM, tenantRPM int) *Limiter {
	if rdb == nil || (routeRPM <= 0 && tenantRPM <= 0) {
		return nil
	}
	return &Limiter{
		rdb:       rdb,
		routeRPM:  routeRPM,
		tenantRPM: tenantRPM,
		window:    time.Minute,
	}
}

func (l *Limiter) Allow(ctx context.Context, tenantID, routeID uuid.UUID) (bool, error) {
	if l == nil || l.rdb == nil {
		return true, nil
	}
	if l.routeRPM > 0 {
		ok, err := l.checkSlidingWindow(ctx, fmt.Sprintf("autocreate:route:%s", routeID), l.routeRPM)
		if err != nil || !ok {
			return ok, err
		}
	}
	if l.tenantRPM > 0 {
		return l.checkSlidingWindow(ctx, fmt.Sprintf("autocreate:tenant:%s", tenantID), l.tenantRPM)
	}
	return true, nil
}

func (l *Limiter) checkSlidingWindow(ctx context.Context, key string, limit int) (bool, error) {
	now := time.Now().UnixMilli()
	windowStart := now - l.window.Milliseconds()
	pipe := l.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, key, l.window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return countCmd.Val() < int64(limit), nil
}
