package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

const adminKeyForTests = "admin-secret"

func TestAdminHandlerCreateTenantCreatesAuditEntry(t *testing.T) {
	h, st, planID, _ := seededAdminHandler(t)

	rr := doAdminRequest(t, st, http.MethodPost, "/api/v1/admin/tenants", map[string]any{
		"name":    "tenant-b",
		"plan_id": planID.String(),
	}, nil, h.CreateTenant)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].(map[string]any)
	if data["name"] != "tenant-b" {
		t.Fatalf("unexpected tenant payload: %#v", data)
	}

	tenants, err := st.ListTenants(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 3 {
		t.Fatalf("expected 3 tenants including public and created tenant, got %d", len(tenants))
	}

	audit, err := st.ListAuditEntries(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "tenant.create" || audit[0].Actor != "admin" {
		t.Fatalf("unexpected audit entries: %#v", audit)
	}
}

func TestAdminHandlerUpdateTenantOverridePersistsValues(t *testing.T) {
	h, st, _, tenantID := seededAdminHandler(t)

	rr := doAdminRequest(t, st, http.MethodPatch, "/api/v1/admin/tenants/"+tenantID.String(), map[string]any{
		"max_domains":       9,
		"retention_hours":   72,
		"daily_quota":       500,
		"max_message_bytes": 2048,
	}, map[string]string{"id": tenantID.String()}, h.UpdateTenantOverride)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	override, err := st.GetOverride(context.Background(), tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if override == nil {
		t.Fatal("expected override to be stored")
	}
	if override.MaxDomains == nil || *override.MaxDomains != 9 {
		t.Fatalf("unexpected max_domains: %#v", override.MaxDomains)
	}
	if override.RetentionHours == nil || *override.RetentionHours != 72 {
		t.Fatalf("unexpected retention_hours: %#v", override.RetentionHours)
	}
	if override.DailyQuota == nil || *override.DailyQuota != 500 {
		t.Fatalf("unexpected daily_quota: %#v", override.DailyQuota)
	}

	audit, err := st.ListAuditEntries(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "tenant.override.upsert" {
		t.Fatalf("unexpected audit entries: %#v", audit)
	}
}

func TestAdminHandlerUpdateTenantOverrideAllowsClearingBackToInherited(t *testing.T) {
	h, st, _, tenantID := seededAdminHandler(t)

	first := doAdminRequest(t, st, http.MethodPatch, "/api/v1/admin/tenants/"+tenantID.String(), map[string]any{
		"max_domains":     9,
		"retention_hours": 72,
	}, map[string]string{"id": tenantID.String()}, h.UpdateTenantOverride)
	if first.Code != http.StatusOK {
		t.Fatalf("expected first update 200, got %d body=%s", first.Code, first.Body.String())
	}

	second := doAdminRequest(t, st, http.MethodPatch, "/api/v1/admin/tenants/"+tenantID.String(), map[string]any{
		"max_domains":     nil,
		"retention_hours": nil,
	}, map[string]string{"id": tenantID.String()}, h.UpdateTenantOverride)
	if second.Code != http.StatusOK {
		t.Fatalf("expected clear update 200, got %d body=%s", second.Code, second.Body.String())
	}

	override, err := st.GetOverride(context.Background(), tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if override == nil {
		t.Fatal("expected override to remain stored")
	}
	if override.MaxDomains != nil {
		t.Fatalf("expected max_domains to be cleared, got %#v", override.MaxDomains)
	}
	if override.RetentionHours != nil {
		t.Fatalf("expected retention_hours to be cleared, got %#v", override.RetentionHours)
	}
}

func TestAdminHandlerCreateAPIKeyDefaultsScopesAndStoresHash(t *testing.T) {
	h, st, _, tenantID := seededAdminHandler(t)

	rr := doAdminRequest(t, st, http.MethodPost, "/api/v1/admin/tenants/"+tenantID.String()+"/keys", map[string]any{
		"label": "primary",
	}, map[string]string{"id": tenantID.String()}, h.CreateAPIKey)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].(map[string]any)
	raw := data["key"].(string)
	if len(raw) < 12 || data["key_prefix"] != raw[:12] {
		t.Fatalf("unexpected key payload: %#v", data)
	}
	scopes := data["scopes"].([]any)
	if len(scopes) != 1 || scopes[0].(string) != "*" {
		t.Fatalf("expected default scopes [*], got %#v", scopes)
	}

	keys, err := st.ListAPIKeys(context.Background(), tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 stored api key, got %d", len(keys))
	}
	wantHash := sha256.Sum256([]byte(raw))
	if keys[0].KeyHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("unexpected key hash: got %s", keys[0].KeyHash)
	}
	if keys[0].Label != "primary" {
		t.Fatalf("unexpected key label: %#v", keys[0])
	}

	audit, err := st.ListAuditEntries(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "api_key.create" {
		t.Fatalf("unexpected audit entries: %#v", audit)
	}
}

func TestAdminHandlerSMTPPolicyFallbackAndUpdate(t *testing.T) {
	h, st, _, _ := seededAdminHandler(t)

	getRR := doAdminRequest(t, st, http.MethodGet, "/api/v1/admin/policy", nil, nil, h.GetSMTPPolicy)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	var getBody map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody["data"].(map[string]any)["default_accept"] != true {
		t.Fatalf("expected fallback default policy, got %#v", getBody)
	}

	updateRR := doAdminRequest(t, st, http.MethodPatch, "/api/v1/admin/policy", map[string]any{
		"default_accept":        false,
		"accept_domains":        []string{"example.com"},
		"reject_domains":        []string{"bad.test"},
		"default_store":         true,
		"store_domains":         []string{"example.com"},
		"discard_domains":       []string{"trash.test"},
		"reject_origin_domains": []string{"spam.test"},
	}, nil, h.UpdateSMTPPolicy)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", updateRR.Code, updateRR.Body.String())
	}

	policy, err := st.GetSMTPPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil || policy.DefaultAccept != false || len(policy.AcceptDomains) != 1 || policy.AcceptDomains[0] != "example.com" {
		t.Fatalf("unexpected stored policy: %#v", policy)
	}
}

func TestAdminHandlerStatsSummarizesStoreCounts(t *testing.T) {
	h, st, _, tenantID := seededAdminHandler(t)

	zoneID := uuid.New()
	mailboxID := uuid.New()
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  time.Now(),
	})
	st.SeedMailbox(&models.Mailbox{
		ID:             mailboxID,
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      "user",
		ResolvedDomain: "mail.test",
		FullAddress:    "user@mail.test",
		AccessMode:     models.AccessPublic,
		CreatedAt:      time.Now(),
	})
	st.SeedMessage(&models.Message{
		ID:         uuid.New(),
		TenantID:   tenantID,
		MailboxID:  mailboxID,
		ZoneID:     zoneID,
		Subject:    "hello",
		Recipients: []string{"user@mail.test"},
		ReceivedAt: time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	})

	rr := doAdminRequest(t, st, http.MethodGet, "/api/v1/admin/stats", nil, nil, h.Stats)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].(map[string]any)
	if int(data["tenants_count"].(float64)) != 2 {
		t.Fatalf("unexpected tenants_count: %#v", data["tenants_count"])
	}
	if int(data["plans_count"].(float64)) != 1 {
		t.Fatalf("unexpected plans_count: %#v", data["plans_count"])
	}
	if int(data["domains_count"].(float64)) != 1 || int(data["mailboxes_count"].(float64)) != 1 || int(data["messages_count"].(float64)) != 1 {
		t.Fatalf("unexpected stats payload: %#v", data)
	}
}

func seededAdminHandler(t *testing.T) (*AdminHandler, *testutil.FakeStore, uuid.UUID, uuid.UUID) {
	t.Helper()

	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              100,
		DailyQuota:            500,
	})
	st.SeedTenant(&models.Tenant{
		ID:      uuid.MustParse(publicTenantIDForTests),
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

	defaultPolicy := models.SMTPPolicy{
		DefaultAccept: true,
		DefaultStore:  true,
	}
	return NewAdminHandler(st, nil, defaultPolicy, zerolog.Nop()), st, planID, tenantID
}

func doAdminRequest(
	t *testing.T,
	st *testutil.FakeStore,
	method, path string,
	body any,
	params map[string]string,
	handler http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKeyForTests)

	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(withRouteContext(req, rctx))
	}

	rr := httptest.NewRecorder()
	wrapped := middleware.Auth(st, adminKeyForTests, publicTenantIDForTests)(handler)
	wrapped.ServeHTTP(rr, req)
	return rr
}
