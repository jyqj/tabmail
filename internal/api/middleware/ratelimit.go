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
	"tabmail/internal/ratelimit"
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
		tenantScoped := t != nil && t.ID != uuid.Nil && (mode == AuthModeAPIKey || mode == AuthModeUser || mode == AuthModeAdmin || (mode == AuthModeSuperAdmin && !BypassLimits(ctx)))

		if BypassLimits(ctx) {
			next.ServeHTTP(w, r)
			return
		}

		var key string
		var limit int
		var tenantCfg *models.EffectiveConfig

		if tenantScoped {
			cfg, err := rl.store.EffectiveConfig(ctx, t.ID)
			if err == nil && cfg != nil {
				tenantCfg = cfg
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

		if tenantScoped && tenantCfg != nil && tenantCfg.DailyQuota > 0 {
			ok, err := rl.checkDailyQuota(ctx, fmt.Sprintf("quota:tenant:%s:%s", t.ID, time.Now().UTC().Format("20060102")), tenantCfg.DailyQuota)
			if err == nil && !ok {
				writeQuotaError(w, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "daily quota exceeded")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return ratelimit.Allow(ctx, rl.rdb, key, limit, window)
}

func (rl *RateLimiter) realIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// A portless synthetic peer can still have a stable bucket, but it
		// cannot establish the proxy provenance supplied by a TCP peer.
		if ip := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); ip != nil {
			return ip.String()
		}
		return "unknown"
	}
	remoteIP := net.ParseIP(strings.TrimSpace(host))
	if remoteIP == nil {
		return "unknown"
	}
	peer := remoteIP.String()
	if !rl.isTrustedProxy(remoteIP) {
		return peer
	}
	if values := r.Header.Values("X-Forwarded-For"); len(values) != 0 {
		// Each trusted proxy appends its observed peer. Walk that chain from
		// the socket towards the client, stopping at the first untrusted IP.
		// Anything before that IP is client-controlled, including malformed
		// prefixes. Include all field lines in their wire order.
		chain := strings.Split(strings.Join(values, ","), ",")
		current := remoteIP
		for i := len(chain) - 1; i >= 0; i-- {
			if !rl.isTrustedProxy(current) {
				return current.String()
			}
			current = net.ParseIP(strings.TrimSpace(chain[i]))
			if current == nil {
				// Invalid data within the trusted suffix cannot prove a
				// client or defer to a conflicting alternate header.
				return peer
			}
		}
		return current.String()
	}
	// Preserve single-header deployments when no forwarded chain is present.
	// The trusted ingress must overwrite X-Real-IP with the observed client.
	if values := r.Header.Values("X-Real-IP"); len(values) == 1 {
		if ip := net.ParseIP(strings.TrimSpace(values[0])); ip != nil {
			return ip.String()
		}
	}
	return peer
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

func (rl *RateLimiter) CheckAddressRateLimit(ctx context.Context, address string, limit int, window time.Duration) (bool, error) {
	key := fmt.Sprintf("rate:token:%s", strings.ToLower(strings.TrimSpace(address)))
	return rl.checkSlidingWindow(ctx, key, limit, window)
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
