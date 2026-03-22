package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"tabmail/internal/store"
)

// RateLimiter enforces per-tenant RPM and per-IP fallback limits.
type RateLimiter struct {
	rdb   *redis.Client
	store store.Store
	ipRPM int // fallback RPM for public/unauthenticated
}

func NewRateLimiter(rdb *redis.Client, st store.Store, publicIPRPM int) *RateLimiter {
	return &RateLimiter{rdb: rdb, store: st, ipRPM: publicIPRPM}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		t := TenantFromCtx(ctx)
		mode := AuthModeFromCtx(ctx)

		if t != nil && t.IsSuper {
			next.ServeHTTP(w, r)
			return
		}

		var key string
		var limit int

		if mode == AuthModeAPIKey && t != nil && t.ID != [16]byte{} {
			cfg, err := rl.store.EffectiveConfig(ctx, t.ID)
			if err == nil && cfg != nil {
				key = fmt.Sprintf("rate:tenant:%s", t.ID)
				limit = cfg.RPMLimit
			}
		}

		if key == "" {
			ip := realIP(r)
			key = fmt.Sprintf("rate:ip:%s", ip)
			limit = rl.ipRPM
		}

		if limit <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		allowed, err := rl.checkSlidingWindow(ctx, key, limit, time.Minute)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			writeQuotaError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			return
		}

		if mode == AuthModeAPIKey && t != nil && t.ID != [16]byte{} {
			cfg, err := rl.store.EffectiveConfig(ctx, t.ID)
			if err == nil && cfg != nil && cfg.DailyQuota > 0 {
				ok, err := rl.checkDailyQuota(ctx, fmt.Sprintf("quota:tenant:%s:%s", t.ID, time.Now().UTC().Format("20060102")), cfg.DailyQuota)
				if err == nil && !ok {
					writeQuotaError(w, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "daily quota exceeded")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().UnixMilli()
	windowStart := now - window.Milliseconds()

	pipe := rl.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, key, window+time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return countCmd.Val() < int64(limit), nil
}

func realIP(r *http.Request) string {
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func (rl *RateLimiter) checkDailyQuota(ctx context.Context, key string, limit int) (bool, error) {
	pipe := rl.rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 25*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return incr.Val() <= int64(limit), nil
}

func writeQuotaError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": msg,
		},
	})
}
