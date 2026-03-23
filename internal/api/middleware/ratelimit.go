package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"tabmail/internal/models"
)

// RateLimiter enforces per-tenant RPM and per-IP fallback limits.
type RateLimiter struct {
	rdb            *redis.Client
	store          rateLimitStore
	ipRPM          int // fallback RPM for public/unauthenticated
	trustedProxies []*net.IPNet
}

type rateLimitStore interface {
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
}

func NewRateLimiter(rdb *redis.Client, st rateLimitStore, publicIPRPM int, trustedProxyCIDRs []string) *RateLimiter {
	return &RateLimiter{
		rdb:            rdb,
		store:          st,
		ipRPM:          publicIPRPM,
		trustedProxies: parseTrustedProxyCIDRs(trustedProxyCIDRs),
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		t := TenantFromCtx(ctx)
		mode := AuthModeFromCtx(ctx)
		tenantScoped := t != nil && t.ID != uuid.Nil && (mode == AuthModeAPIKey || (mode == AuthModeAdmin && !BypassLimits(ctx)))

		if BypassLimits(ctx) {
			next.ServeHTTP(w, r)
			return
		}

		var key string
		var limit int

		if tenantScoped {
			cfg, err := rl.store.EffectiveConfig(ctx, t.ID)
			if err == nil && cfg != nil {
				key = fmt.Sprintf("rate:tenant:%s", t.ID)
				limit = cfg.RPMLimit
			}
		}

		if key == "" {
			ip := rl.realIP(r)
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

		if tenantScoped {
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
	if rl.rdb == nil {
		return true, nil
	}
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

func (rl *RateLimiter) realIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	remoteIP := net.ParseIP(strings.TrimSpace(host))
	if remoteIP != nil && rl.isTrustedProxy(remoteIP) {
		if xri := r.Header.Get("X-Real-Ip"); xri != "" {
			return strings.TrimSpace(xri)
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" && remoteIP == nil {
		return strings.TrimSpace(xri)
	}
	return host
}

func (rl *RateLimiter) checkDailyQuota(ctx context.Context, key string, limit int) (bool, error) {
	if rl.rdb == nil {
		return true, nil
	}
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

func (rl *RateLimiter) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range rl.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseTrustedProxyCIDRs(items []string) []*net.IPNet {
	var out []*net.IPNet
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			_, network, err := net.ParseCIDR(item)
			if err == nil {
				out = append(out, network)
			}
			continue
		}
		ip := net.ParseIP(item)
		if ip == nil {
			continue
		}
		maskBits := 32
		if ip.To4() == nil {
			maskBits = 128
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(maskBits, maskBits)})
	}
	return out
}
