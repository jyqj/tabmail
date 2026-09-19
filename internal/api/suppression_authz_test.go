package api_test

// Suppression management is tenant-admin-only for interactive JWT users; API
// keys must carry the dedicated suppression scopes. The delete path also
// demands a non-empty reason and lands with an audit row.
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func suppressionRouterRequest(t *testing.T, h http.Handler, headers map[string]string, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func seedSuppression(t *testing.T, st interface {
	AddSuppression(context.Context, *models.SuppressionEntry) error
}, tenantID uuid.UUID) *models.SuppressionEntry {
	t.Helper()
	e := &models.SuppressionEntry{ID: uuid.New(), TenantID: tenantID, Address: "blocked@example.test", Reason: "hard_bounce", CreatedAt: time.Now()}
	if err := st.AddSuppression(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSuppressionOrdinaryUserForbidden(t *testing.T) {
	st, obj, tenant := seededStores(t)
	user := seedUserForTest(t, st, tenant, models.RoleUser)
	entry := seedSuppression(t, st, tenant)
	h := rbRouter(t, st, obj)
	headers := map[string]string{"Authorization": "Bearer " + issueAccessTokenForExistingUser(t, user)}

	w := suppressionRouterRequest(t, h, headers, http.MethodGet, "/api/v1/suppression", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("ordinary user GET expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	w = suppressionRouterRequest(t, h, headers, http.MethodDelete, "/api/v1/suppression/"+entry.ID.String(), `{"reason":"cleanup"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("ordinary user DELETE expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	exists, err := st.IsSuppressed(context.Background(), tenant, entry.Address)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("forbidden delete removed the suppression entry")
	}
	audits, err := st.ListAuditEntries(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range audits {
		if a.Action == "suppression.delete" {
			t.Fatal("forbidden delete wrote an audit row")
		}
	}
}

func TestSuppressionTenantAdminFlow(t *testing.T) {
	st, obj, tenant := seededStores(t)
	admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
	entry := seedSuppression(t, st, tenant)
	h := rbRouter(t, st, obj)
	headers := map[string]string{"Authorization": "Bearer " + issueAccessTokenForExistingUser(t, admin)}

	w := suppressionRouterRequest(t, h, headers, http.MethodGet, "/api/v1/suppression", "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin GET expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), entry.Address) {
		t.Fatalf("admin GET missing seeded entry, body=%s", w.Body.String())
	}

	w = suppressionRouterRequest(t, h, headers, http.MethodDelete, "/api/v1/suppression/"+entry.ID.String(), `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete without reason expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	w = suppressionRouterRequest(t, h, headers, http.MethodDelete, "/api/v1/suppression/"+entry.ID.String(), `{"reason":"cleanup"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete with reason expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	exists, err := st.IsSuppressed(context.Background(), tenant, entry.Address)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("admin delete did not remove the suppression entry")
	}
	audits, err := st.ListAuditEntries(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range audits {
		if a.Action != "suppression.delete" {
			continue
		}
		found = true
		var details map[string]any
		if err := json.Unmarshal(a.Details, &details); err != nil {
			t.Fatal(err)
		}
		if details["reason"] != "cleanup" || details["address"] != entry.Address || details["suppression_id"] != entry.ID.String() {
			t.Fatalf("audit details incomplete: %v", details)
		}
	}
	if !found {
		t.Fatal("audited delete did not write an audit row")
	}
}

func TestSuppressionAPIKeyScopes(t *testing.T) {
	st, obj, tenant := seededStores(t)
	entry := seedSuppression(t, st, tenant)
	h := rbRouter(t, st, obj)
	tenantModel := &models.Tenant{ID: tenant, Name: "tenant-a"}
	st.RegisterAPIKey("sup-send-key", tenantModel, []string{"send:write"})
	st.RegisterAPIKey("sup-read-key", tenantModel, []string{"suppression:read"})
	st.RegisterAPIKey("sup-manage-key", tenantModel, []string{"suppression:manage"})

	w := suppressionRouterRequest(t, h, map[string]string{"X-API-Key": "sup-read-key"}, http.MethodGet, "/api/v1/suppression", "")
	if w.Code != http.StatusOK {
		t.Fatalf("suppression:read key list expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	w = suppressionRouterRequest(t, h, map[string]string{"X-API-Key": "sup-send-key"}, http.MethodGet, "/api/v1/suppression", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("send:write key list expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	w = suppressionRouterRequest(t, h, map[string]string{"X-API-Key": "sup-send-key"}, http.MethodDelete, "/api/v1/suppression/"+entry.ID.String(), `{"reason":"cleanup"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("send:write key delete expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	w = suppressionRouterRequest(t, h, map[string]string{"X-API-Key": "sup-manage-key"}, http.MethodDelete, "/api/v1/suppression/"+entry.ID.String(), `{"reason":"cleanup"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("suppression:manage key delete expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	exists, err := st.IsSuppressed(context.Background(), tenant, entry.Address)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("suppression:manage key delete did not remove the entry")
	}
}
