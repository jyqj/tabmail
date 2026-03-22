package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func TestTriggerVerifyUpdatesZoneStatus(t *testing.T) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()

	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "test",
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 100,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              100,
		DailyQuota:            1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:        zoneID,
		TenantID:  tenantID,
		Domain:    "mail.test",
		TXTRecord: "tabmail-verify=ok",
	})

	h := NewDomainHandler(st, hooks.New(hooks.Config{}, zerolog.Nop()), "mx.mail.test", zerolog.Nop())
	h.lookupTXT = func(name string) ([]string, error) {
		return []string{"tabmail-verify=ok", "v=spf1 include:test"}, nil
	}
	h.lookupMX = func(name string) ([]*net.MX, error) {
		return []*net.MX{{Host: "mx.mail.test.", Pref: 10}}, nil
	}

	handler := middleware.Auth(st, "admin-secret", publicTenantIDForTests)(http.HandlerFunc(h.TriggerVerify))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/"+zoneID.String()+"/verify", nil)
	req.Header.Set("X-Admin-Key", "admin-secret")
	req.Header.Set("X-Tenant-ID", tenantID.String())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", zoneID.String())
	req = req.WithContext(withRouteContext(req, rctx))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	zone, err := st.GetZone(req.Context(), zoneID)
	if err != nil {
		t.Fatal(err)
	}
	if zone == nil || !zone.IsVerified || !zone.MXVerified {
		t.Fatalf("expected verified zone, got %#v", zone)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].(map[string]any)
	if data["is_verified"] != true || data["mx_verified"] != true {
		t.Fatalf("unexpected response: %#v", data)
	}
}

const publicTenantIDForTests = "00000000-0000-0000-0000-000000000001"

func withRouteContext(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}
