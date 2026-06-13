package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"tabmail/internal/models"
)

func TestRouterWebhookEndpointScopesAndJWTAccess(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("webhook-read", tenant, []string{"webhooks:read"})
	st.RegisterAPIKey("webhook-write", tenant, []string{"webhooks:write"})
	st.RegisterAPIKey("domain-read", tenant, []string{"domains:read"})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	expectStatus(t, router, http.MethodGet, "/api/v1/webhook-endpoints", nil, map[string]string{
		"X-API-Key": "webhook-read",
	}, http.StatusOK)
	expectStatus(t, router, http.MethodGet, "/api/v1/webhook-endpoints", nil, map[string]string{
		"X-API-Key": "domain-read",
	}, http.StatusForbidden)
	expectStatus(t, router, http.MethodPost, "/api/v1/webhook-endpoints", validWebhookEndpointBody(), map[string]string{
		"X-API-Key": "webhook-read",
	}, http.StatusForbidden)
	expectStatus(t, router, http.MethodPost, "/api/v1/webhook-endpoints", validWebhookEndpointBody(), map[string]string{
		"X-API-Key": "webhook-write",
	}, http.StatusCreated)
	expectStatus(t, router, http.MethodPatch, "/api/v1/webhook-endpoints/"+uuid.NewString(), map[string]any{"is_active": false}, map[string]string{
		"X-API-Key": "webhook-read",
	}, http.StatusForbidden)
	expectStatus(t, router, http.MethodDelete, "/api/v1/webhook-endpoints/"+uuid.NewString(), nil, map[string]string{
		"X-API-Key": "webhook-read",
	}, http.StatusForbidden)

	userToken := issueAccessTokenForTest(t, st, tenantID, models.RoleUser)
	expectStatus(t, router, http.MethodPost, "/api/v1/webhook-endpoints", validWebhookEndpointBody(), map[string]string{
		"Authorization": "Bearer " + userToken,
	}, http.StatusForbidden)

	tenantAdminToken := issueAccessTokenForTest(t, st, tenantID, models.RoleAdmin)
	expectStatus(t, router, http.MethodPost, "/api/v1/webhook-endpoints", validWebhookEndpointBody(), map[string]string{
		"Authorization": "Bearer " + tenantAdminToken,
	}, http.StatusCreated)
}

func TestRouterWebhookEndpointURLAndFieldSanitization(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("webhook-write", tenant, []string{"webhooks:write"})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)
	headers := map[string]string{"X-API-Key": "webhook-write"}

	for _, rawURL := range []string{
		"http://example.com/hook",
		"https://localhost/hook",
		"https://127.0.0.1/hook",
		"https://10.0.0.1/hook",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/hook",
		"https:///missing-host",
	} {
		t.Run(rawURL, func(t *testing.T) {
			expectStatus(t, router, http.MethodPost, "/api/v1/webhook-endpoints", map[string]any{"url": rawURL}, headers, http.StatusBadRequest)
		})
	}

	rr := performJSON(router, http.MethodPost, "/api/v1/webhook-endpoints", map[string]any{
		"url":         " https://example.com/hook ",
		"secret":      "  webhook-secret  ",
		"event_types": []string{" message.received ", "MESSAGE.DELETED", "message.received"},
	}, headers)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response payload: %#v", out)
	}
	if _, ok := data["secret"]; ok {
		t.Fatalf("secret must not be rendered in response: %#v", data)
	}
	gotTypes, ok := data["event_types"].([]any)
	if !ok || len(gotTypes) != 2 || gotTypes[0] != "message.received" || gotTypes[1] != "message.deleted" {
		t.Fatalf("event_types not sanitized/deduplicated: %#v", data["event_types"])
	}
}

func validWebhookEndpointBody() map[string]any {
	return map[string]any{
		"url":         "https://example.com/hook",
		"event_types": []string{"message.received"},
	}
}

func expectStatus(t *testing.T, router http.Handler, method, path string, body any, headers map[string]string, want int) {
	t.Helper()
	rr := performJSON(router, method, path, body, headers)
	if rr.Code != want {
		t.Fatalf("%s %s expected %d, got %d body=%s", method, path, want, rr.Code, rr.Body.String())
	}
}

func performJSON(router http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}
