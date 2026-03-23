package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

const publicTenantIDForMiddlewareTests = "00000000-0000-0000-0000-000000000001"

func TestAuthResolvesPublicTenant(t *testing.T) {
	st, tenantID := seededAuthStore()

	var gotTenant *models.Tenant
	var gotMode string
	handler := Auth(st, "admin-secret", publicTenantIDForMiddlewareTests)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = TenantFromCtx(r.Context())
		gotMode = AuthModeFromCtx(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if gotTenant == nil || gotTenant.ID != uuid.MustParse(publicTenantIDForMiddlewareTests) {
		t.Fatalf("unexpected public tenant: %#v", gotTenant)
	}
	if gotMode != AuthModePublic {
		t.Fatalf("unexpected auth mode: %q", gotMode)
	}
	if tenantID == uuid.Nil {
		t.Fatal("expected non-zero tenant id in test seed")
	}
}

func TestAuthResolvesAdminAndScopedTenant(t *testing.T) {
	st, tenantID := seededAuthStore()

	var gotTenant *models.Tenant
	var gotAdmin bool
	var gotScopes []string
	handler := Auth(st, "admin-secret", publicTenantIDForMiddlewareTests)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = TenantFromCtx(r.Context())
		gotAdmin = IsAdmin(r.Context())
		gotScopes = APIScopesFromCtx(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Admin-Key", "admin-secret")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !gotAdmin {
		t.Fatal("expected admin context")
	}
	if gotTenant == nil || gotTenant.ID != tenantID || !gotTenant.IsSuper {
		t.Fatalf("unexpected tenant context: %#v", gotTenant)
	}
	if len(gotScopes) != 1 || gotScopes[0] != "*" {
		t.Fatalf("unexpected scopes: %#v", gotScopes)
	}
}

func TestAuthResolvesAPIKeyAndEnforcesScopes(t *testing.T) {
	st, tenantID := seededAuthStore()
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("tenant-key", tenant, []string{"domains:read"})

	okHandler := Auth(st, "admin-secret", publicTenantIDForMiddlewareTests)(
		RequireTenantKeyOrAdmin(
			RequireScopes("domains:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if AuthModeFromCtx(r.Context()) != AuthModeAPIKey {
					t.Fatalf("unexpected auth mode: %q", AuthModeFromCtx(r.Context()))
				}
				w.WriteHeader(http.StatusNoContent)
			})),
		),
	)

	okReq := httptest.NewRequest(http.MethodGet, "/", nil)
	okReq.Header.Set("X-API-Key", "tenant-key")
	okRR := httptest.NewRecorder()
	okHandler.ServeHTTP(okRR, okReq)
	if okRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", okRR.Code, okRR.Body.String())
	}

	failHandler := Auth(st, "admin-secret", publicTenantIDForMiddlewareTests)(
		RequireScopes("domains:write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	failReq := httptest.NewRequest(http.MethodGet, "/", nil)
	failReq.Header.Set("X-API-Key", "tenant-key")
	failRR := httptest.NewRecorder()
	failHandler.ServeHTTP(failRR, failReq)
	if failRR.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", failRR.Code, failRR.Body.String())
	}
}

func TestRequireTenantKeyOrAdminRejectsPublic(t *testing.T) {
	st, _ := seededAuthStore()

	handler := Auth(st, "admin-secret", publicTenantIDForMiddlewareTests)(
		RequireTenantKeyOrAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRealIPPrefersProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.2")
	req.Header.Set("X-Real-Ip", "192.0.2.7")

	if got := realIP(req); got != "192.0.2.7" {
		t.Fatalf("unexpected real ip: %q", got)
	}

	req.Header.Del("X-Real-Ip")
	if got := realIP(req); got != "203.0.113.1" {
		t.Fatalf("unexpected forwarded ip: %q", got)
	}
}

func seededAuthStore() (*testutil.FakeStore, uuid.UUID) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()

	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 20,
		MaxMessagesPerMailbox: 100,
		MaxMessageBytes:       1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{
		ID:      uuid.MustParse(publicTenantIDForMiddlewareTests),
		Name:    "public",
		PlanID:  planID,
		IsSuper: false,
	})
	st.SeedTenant(&models.Tenant{
		ID:      tenantID,
		Name:    "tenant-a",
		PlanID:  planID,
		IsSuper: false,
	})
	return st, tenantID
}

func TestWriteErrorProducesEnvelope(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, http.StatusBadRequest, "BAD_REQUEST", "boom")

	var body map[string]map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusBadRequest || body["error"]["message"] != "boom" {
		t.Fatalf("unexpected response: status=%d body=%v", rr.Code, body)
	}
}
