package middleware

import (
	"context"
	"encoding/json"
	"errors"
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

// slidingWindowScript trims the window, counts what remains, records the
// current request, and reports whether the pre-existing count was under the
// limit. Running it as one script makes the count-then-record pair atomic; the
// previous pipeline let concurrent requests all observe the same count.
//
// KEYS[1] window key. ARGV: window start (ms), now (ms), member, TTL (ms), limit.
var slidingWindowScript = redis.NewScript(`
redis.call("ZREMRANGEBYSCORE", KEYS[1], "0", ARGV[1])
local count = redis.call("ZCARD", KEYS[1])
redis.call("ZADD", KEYS[1], ARGV[2], ARGV[3])
redis.call("PEXPIRE", KEYS[1], ARGV[4])
if count < tonumber(ARGV[5]) then
  return 1
end
return 0
`)

func (rl *RateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if rl.rdb == nil {
		return true, nil
	}
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d:%s", now, uuid.NewString())

	allowed, err := slidingWindowScript.Run(ctx, rl.rdb, []string{key},
		now-window.Milliseconds(),
		now,
		member,
		(window + time.Second).Milliseconds(),
		limit,
	).Int()
	if err != nil {
		return false, err
	}
	return allowed == 1, nil
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

// dailyQuotaScript increments the day counter, sets its TTL on first use, and
// reports whether the caller is still within the limit.
//
// KEYS[1] date-stamped counter key. ARGV: TTL (seconds), limit.
var dailyQuotaScript = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
  redis.call("EXPIRE", KEYS[1], tonumber(ARGV[1]))
end
if n <= tonumber(ARGV[2]) then
  return 1
end
return 0
`)

func (rl *RateLimiter) checkDailyQuota(ctx context.Context, key string, limit int) (bool, error) {
	if rl.rdb == nil {
		return true, nil
	}
	allowed, err := dailyQuotaScript.Run(ctx, rl.rdb, []string{key}, int((25 * time.Hour).Seconds()), limit).Int()
	if err != nil {
		return false, err
	}
	return allowed == 1, nil
}

func (rl *RateLimiter) CheckAddressRateLimit(ctx context.Context, address string, limit int, window time.Duration) (bool, error) {
	key := fmt.Sprintf("rate:token:%s", strings.ToLower(strings.TrimSpace(address)))
	return rl.checkSlidingWindow(ctx, key, limit, window)
}

// Failed-login throttling bounds password guessing per account. The counter is
// keyed on the email being guessed rather than the client IP, so distributed
// guessing against one account is still throttled.
const (
	LoginFailureLimit  = 10
	LoginFailureWindow = 15 * time.Minute
)

var errLoginThrottleUnavailable = errors.New("login throttle unavailable")

// LoginAttemptsExceeded reports whether an identity has produced too many
// recent failed logins. It fails open when Redis is unreachable; callers use
// RecordLoginFailure's error to apply a fallback defence.
func (rl *RateLimiter) LoginAttemptsExceeded(ctx context.Context, identity string) bool {
	if rl == nil || rl.rdb == nil {
		return false
	}
	n, err := rl.rdb.Get(ctx, loginFailureKey(identity)).Int64()
	if err != nil {
		return false
	}
	return n >= LoginFailureLimit
}

// RecordLoginFailure counts a failed login against the identity's window.
func (rl *RateLimiter) RecordLoginFailure(ctx context.Context, identity string) error {
	if rl == nil || rl.rdb == nil {
		return errLoginThrottleUnavailable
	}
	key := loginFailureKey(identity)
	pipe := rl.rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, LoginFailureWindow)
	_, err := pipe.Exec(ctx)
	return err
}

func loginFailureKey(identity string) string {
	return "rate:login-failure:" + strings.ToLower(strings.TrimSpace(identity))
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
