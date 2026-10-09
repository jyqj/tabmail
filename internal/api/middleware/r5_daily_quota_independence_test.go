package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

type dailyQuotaConfigStore struct {
	configs map[uuid.UUID]*models.EffectiveConfig
}

func (s dailyQuotaConfigStore) EffectiveConfig(_ context.Context, id uuid.UUID) (*models.EffectiveConfig, error) {
	return s.configs[id], nil
}

func dailyQuotaRequest(tenant uuid.UUID, mode string, bypass bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes", nil)
	r.RemoteAddr = "192.0.2.10:50000"
	ctx := context.WithValue(r.Context(), ctxTenant, &models.Tenant{ID: tenant})
	ctx = context.WithValue(ctx, ctxAuthMode, mode)
	ctx = context.WithValue(ctx, ctxBypassLimits, bypass)
	return r.WithContext(ctx)
}

func TestR5DailyQuotaIndependentOfDisabledRPM(t *testing.T) {
	for _, mode := range []string{AuthModeAPIKey, AuthModeUser, AuthModeAdmin, AuthModeSuperAdmin} {
		t.Run(mode, func(t *testing.T) {
			server, client := windowRedis(t)
			tenant := uuid.New()
			st := dailyQuotaConfigStore{map[uuid.UUID]*models.EffectiveConfig{
				tenant: {RPMLimit: 0, DailyQuota: 2},
			}}
			nextCalls := 0
			handler := NewRateLimiter(client, st, 1, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalls++
				w.WriteHeader(http.StatusNoContent)
			}))
			for attempt := 1; attempt <= 4; attempt++ {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, dailyQuotaRequest(tenant, mode, false))
				want := http.StatusNoContent
				if attempt > 2 {
					want = http.StatusTooManyRequests
				}
				if w.Code != want {
					t.Errorf("attempt %d: status=%d want=%d body=%s", attempt, w.Code, want, w.Body.String())
				}
				if w.Code == http.StatusTooManyRequests {
					var body struct{ Error struct{ Code string } }
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != "QUOTA_EXCEEDED" {
						t.Errorf("daily rejection must preserve its own error code: %s (%v)", w.Body.String(), err)
					}
					if w.Header().Get("Retry-After") != "" {
						t.Error("daily quota was incorrectly labelled as a 60-second RPM limit")
					}
				}
			}
			if nextCalls != 2 {
				t.Errorf("downstream executions=%d, want exactly 2", nextCalls)
			}
			key := fmt.Sprintf("quota:tenant:%s:%s", tenant, time.Now().UTC().Format("20060102"))
			if count, err := server.Get(key); err != nil || count != "4" {
				t.Errorf("daily attempt count=%q err=%v, want 4", count, err)
			}
			if ttl := server.TTL(key); ttl != 25*time.Hour {
				t.Errorf("daily counter expiry=%v, want original 25h", ttl)
			}
			if server.Exists("rate:tenant:"+tenant.String()) || server.Exists("rate:ip:192.0.2.10") {
				t.Error("disabled tenant RPM must not create a tenant or fallback IP window")
			}
		})
	}
}

func TestR5DailyQuotaIndependentTenantAndLimitControls(t *testing.T) {
	t.Run("tenant isolation", func(t *testing.T) {
		_, client := windowRedis(t)
		a, b := uuid.New(), uuid.New()
		st := dailyQuotaConfigStore{map[uuid.UUID]*models.EffectiveConfig{
			a: {DailyQuota: 1}, b: {DailyQuota: 1},
		}}
		h := NewRateLimiter(client, st, 1, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		for i, id := range []uuid.UUID{a, a, b, b} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, dailyQuotaRequest(id, AuthModeUser, false))
			want := http.StatusNoContent
			if i%2 == 1 {
				want = http.StatusTooManyRequests
			}
			if w.Code != want {
				t.Errorf("request %d: status=%d want=%d", i, w.Code, want)
			}
		}
	})
	for _, tc := range []struct {
		name       string
		rpm, daily int
		bypass     bool
		want       []int
	}{
		{"both disabled", 0, 0, false, []int{204, 204, 204}},
		{"explicit bypass", 1, 1, true, []int{204, 204, 204}},
		{"rpm only", 1, 0, false, []int{204, 429, 429}},
		{"daily stricter", 10, 1, false, []int{204, 429, 429}},
		{"rpm stricter", 1, 10, false, []int{204, 429, 429}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, client := windowRedis(t)
			tenant := uuid.New()
			st := dailyQuotaConfigStore{map[uuid.UUID]*models.EffectiveConfig{tenant: {RPMLimit: tc.rpm, DailyQuota: tc.daily}}}
			h := NewRateLimiter(client, st, 1, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			for i, want := range tc.want {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, dailyQuotaRequest(tenant, AuthModeSuperAdmin, tc.bypass))
				if w.Code != want {
					t.Errorf("attempt %d: status=%d want=%d", i+1, w.Code, want)
				}
			}
			if (tc.bypass || (tc.rpm == 0 && tc.daily == 0)) && len(server.Keys()) != 0 {
				t.Errorf("disabled/bypassed quotas created Redis state: %v", server.Keys())
			}
		})
	}
}
