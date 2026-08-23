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
	"tabmail/internal/authn"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
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
	admin := &models.User{
		ID:           uuid.New(),
		TenantID:     tenantID,
		Email:        "admin@example.test",
		PasswordHash: "hash",
		DisplayName:  "Admin",
		Role:         models.RoleAdmin,
		IsActive:     true,
	}
	if err := st.CreateUser(context.Background(), admin); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	st.SeedZone(&models.DomainZone{
		ID:        zoneID,
		TenantID:  tenantID,
		Domain:    "mail.test",
		TXTRecord: "tabmail-verify=ok",
	})

	obj := testutil.NewMemoryObjectStore()
	h := NewDomainHandler(st, obj, rawobject.NewStore(obj, st), hooks.New(hooks.Config{}, zerolog.Nop()), "mx.mail.test", policy.NamingFull, "mailbox-secret", nil, zerolog.Nop())
	h.SetResolvers(
		func(name string) ([]string, error) {
			return []string{"tabmail-verify=ok", "v=spf1 include:test"}, nil
		},
		func(name string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.mail.test.", Pref: 10}}, nil
		},
	)

	token, err := authn.IssueAccessToken("jwt-test-secret", admin)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	handler := middleware.Auth(st, "jwt-test-secret", publicTenantIDForTests)(http.HandlerFunc(h.TriggerVerify))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/"+zoneID.String()+"/verify", nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
