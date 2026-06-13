package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

const outboundTestJWTSecret = "jwt-test-secret"

type outboundAccessFixture struct {
	st            *testutil.FakeStore
	tenantID      uuid.UUID
	otherTenantID uuid.UUID
	userA         *models.User
	userB         *models.User
	tenantAdmin   *models.User
	platformAdmin *models.User
	apiKeyA       uuid.UUID
	apiKeyB       uuid.UUID
	apiKeyARaw    string
	apiKeyBRaw    string
}

func TestOutboundJobAccessCheckCoversGetRetryAndAttempts(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())

	ownerJobID := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID:        ownerJobID,
		TenantID:  f.tenantID,
		UserID:    &f.userA.ID,
		State:     models.OutboundDead,
		CreatedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.CreateOutboundAttempt(context.Background(), &models.OutboundAttempt{
		ID:       uuid.New(),
		JobID:    ownerJobID,
		TenantID: f.tenantID,
		Attempt:  1,
		Error:    "failed",
	}); err != nil {
		t.Fatal(err)
	}

	intruderHeaders := outboundUserHeaders(t, f.userB)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		fn     http.HandlerFunc
	}{
		{"get", http.MethodGet, "/api/v1/outbound/" + ownerJobID.String(), h.GetJob},
		{"retry", http.MethodPost, "/api/v1/outbound/" + ownerJobID.String() + "/retry", h.RetryJob},
		{"attempts", http.MethodGet, "/api/v1/outbound/" + ownerJobID.String() + "/attempts", h.ListAttempts},
	} {
		rr := doOutboundHandlerRequest(t, f.st, tc.fn, tc.method, tc.path, map[string]string{"id": ownerJobID.String()}, intruderHeaders)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s: expected intruder 404, got %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
	}

	rr := doOutboundHandlerRequest(t, f.st, h.GetJob, http.MethodGet, "/api/v1/outbound/"+ownerJobID.String(), map[string]string{"id": ownerJobID.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner get expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doOutboundHandlerRequest(t, f.st, h.ListAttempts, http.MethodGet, "/api/v1/outbound/"+ownerJobID.String()+"/attempts", map[string]string{"id": ownerJobID.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusOK || outboundDataLen(t, rr) != 1 {
		t.Fatalf("owner attempts expected one item, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doOutboundHandlerRequest(t, f.st, h.RetryJob, http.MethodPost, "/api/v1/outbound/"+ownerJobID.String()+"/retry", map[string]string{"id": ownerJobID.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner retry expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	job, err := f.st.GetOutboundJob(context.Background(), ownerJobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != models.OutboundPending {
		t.Fatalf("expected retry to requeue owner job, got state=%s", job.State)
	}

	apiJobID := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID:        apiJobID,
		TenantID:  f.tenantID,
		APIKeyID:  &f.apiKeyA,
		State:     models.OutboundFailed,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	wrongKeyHeaders := map[string]string{"X-API-Key": f.apiKeyBRaw}
	for _, tc := range []struct {
		name   string
		method string
		path   string
		fn     http.HandlerFunc
	}{
		{"api key get", http.MethodGet, "/api/v1/outbound/" + apiJobID.String(), h.GetJob},
		{"api key retry", http.MethodPost, "/api/v1/outbound/" + apiJobID.String() + "/retry", h.RetryJob},
		{"api key attempts", http.MethodGet, "/api/v1/outbound/" + apiJobID.String() + "/attempts", h.ListAttempts},
	} {
		rr := doOutboundHandlerRequest(t, f.st, tc.fn, tc.method, tc.path, map[string]string{"id": apiJobID.String()}, wrongKeyHeaders)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s: expected wrong api key 404, got %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
	}
	rr = doOutboundHandlerRequest(t, f.st, h.GetJob, http.MethodGet, "/api/v1/outbound/"+apiJobID.String(), map[string]string{"id": apiJobID.String()}, map[string]string{"X-API-Key": f.apiKeyARaw})
	if rr.Code != http.StatusOK {
		t.Fatalf("own api key get expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestOutboundListJobsScopesRegularPrincipalsAndTenantContext(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())
	now := time.Now()
	for i, job := range []*models.OutboundJob{
		{ID: uuid.New(), TenantID: f.tenantID, UserID: &f.userA.ID, State: models.OutboundSent},
		{ID: uuid.New(), TenantID: f.tenantID, UserID: &f.userB.ID, State: models.OutboundSent},
		{ID: uuid.New(), TenantID: f.tenantID, APIKeyID: &f.apiKeyA, State: models.OutboundSent},
		{ID: uuid.New(), TenantID: f.tenantID, APIKeyID: &f.apiKeyB, State: models.OutboundSent},
		{ID: uuid.New(), TenantID: f.otherTenantID, UserID: &f.userA.ID, State: models.OutboundSent},
	} {
		job.CreatedAt = now.Add(time.Duration(i) * time.Second)
		if err := f.st.CreateOutboundJob(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}

	rr := doOutboundHandlerRequest(t, f.st, h.ListJobs, http.MethodGet, "/api/v1/outbound", nil, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusOK || outboundDataLen(t, rr) != 1 {
		t.Fatalf("user list expected one own job, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr = doOutboundHandlerRequest(t, f.st, h.ListJobs, http.MethodGet, "/api/v1/outbound", nil, map[string]string{"X-API-Key": f.apiKeyARaw})
	if rr.Code != http.StatusOK || outboundDataLen(t, rr) != 1 {
		t.Fatalf("api key list expected one own job, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr = doOutboundHandlerRequest(t, f.st, h.ListJobs, http.MethodGet, "/api/v1/outbound", nil, outboundUserHeaders(t, f.tenantAdmin))
	if rr.Code != http.StatusOK || outboundDataLen(t, rr) != 4 {
		t.Fatalf("tenant admin list expected tenant jobs only, got %d body=%s", rr.Code, rr.Body.String())
	}

	headers := outboundUserHeaders(t, f.platformAdmin)
	headers["X-Tenant-ID"] = f.tenantID.String()
	rr = doOutboundHandlerRequest(t, f.st, h.ListJobs, http.MethodGet, "/api/v1/outbound", nil, headers)
	if rr.Code != http.StatusOK || outboundDataLen(t, rr) != 4 {
		t.Fatalf("platform admin list expected selected tenant jobs only, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteSuppressionIsTenantScoped(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())

	suppressionID := uuid.New()
	address := "blocked@example.test"
	if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{
		ID:        suppressionID,
		TenantID:  f.otherTenantID,
		Address:   address,
		Reason:    "hard_bounce",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	rr := doOutboundHandlerRequest(t, f.st, h.DeleteSuppression, http.MethodDelete, "/api/v1/suppression/"+suppressionID.String(), map[string]string{"id": suppressionID.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("cross-tenant delete expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	exists, err := f.st.IsSuppressed(context.Background(), f.otherTenantID, address)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("cross-tenant delete removed suppression from another tenant")
	}

	otherTenantUser := &models.User{ID: uuid.New(), TenantID: f.otherTenantID, Email: "other@example.test", Role: models.RoleUser, IsActive: true}
	if err := f.st.CreateUser(context.Background(), otherTenantUser); err != nil {
		t.Fatal(err)
	}
	rr = doOutboundHandlerRequest(t, f.st, h.DeleteSuppression, http.MethodDelete, "/api/v1/suppression/"+suppressionID.String(), map[string]string{"id": suppressionID.String()}, outboundUserHeaders(t, otherTenantUser))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("same-tenant delete expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	exists, err = f.st.IsSuppressed(context.Background(), f.otherTenantID, address)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("same-tenant delete did not remove suppression")
	}
}

func TestSendDoesNotRequireZoneOwnership(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())

	f.st.SeedZone(&models.DomainZone{
		ID:          uuid.New(),
		TenantID:    f.tenantID,
		OwnerUserID: &f.userB.ID,
		Domain:      "shared.example.test",
		IsVerified:  true,
		MXVerified:  true,
	})

	// userA does not own the zone: authorization must still pass (send.from
	// carries no ownership rule), so the request reaches the later
	// mailbox-existence check instead of being rejected with 403.
	rr := doOutboundSendRequest(t, f.st, h, `{"from":"noone@shared.example.test","to":["rcpt@example.org"]}`, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "from address does not exist as a mailbox") {
		t.Fatalf("non-owner same-tenant send should pass authz, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Cross-tenant zone: denied by the seam's tenant isolation.
	f.st.SeedZone(&models.DomainZone{
		ID:         uuid.New(),
		TenantID:   f.otherTenantID,
		Domain:     "other.example.test",
		IsVerified: true,
		MXVerified: true,
	})
	rr = doOutboundSendRequest(t, f.st, h, `{"from":"x@other.example.test","to":["rcpt@example.org"]}`, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant send expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func doOutboundSendRequest(t *testing.T, st *testutil.FakeStore, h *OutboundHandler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(h.Send)).ServeHTTP(rr, req)
	return rr
}

func newOutboundAccessFixture(t *testing.T) outboundAccessFixture {
	t.Helper()
	st := testutil.NewFakeStore()
	publicID := uuid.MustParse(publicTenantIDForTests)
	tenantID := uuid.New()
	otherTenantID := uuid.New()
	st.SeedTenant(&models.Tenant{ID: publicID, Name: "public"})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a"})
	st.SeedTenant(&models.Tenant{ID: otherTenantID, Name: "tenant-b"})

	userA := &models.User{ID: uuid.New(), TenantID: tenantID, Email: "a@example.test", Role: models.RoleUser, IsActive: true}
	userB := &models.User{ID: uuid.New(), TenantID: tenantID, Email: "b@example.test", Role: models.RoleUser, IsActive: true}
	tenantAdmin := &models.User{ID: uuid.New(), TenantID: tenantID, Email: "admin@example.test", Role: models.RoleAdmin, IsActive: true}
	platformAdmin := &models.User{ID: uuid.New(), TenantID: otherTenantID, Email: "platform@example.test", Role: models.RoleSuperAdmin, IsActive: true}
	for _, u := range []*models.User{userA, userB, tenantAdmin, platformAdmin} {
		if err := st.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}

	st.RegisterAPIKey("key-a", &models.Tenant{ID: tenantID, Name: "tenant-a"}, []string{"send:read", "send:write"})
	st.RegisterAPIKey("key-b", &models.Tenant{ID: tenantID, Name: "tenant-a"}, []string{"send:read", "send:write"})
	_, apiKeyA, _, _, _, err := st.ResolveAPIKey(context.Background(), "key-a")
	if err != nil || apiKeyA == nil {
		t.Fatalf("resolve key-a: %v", err)
	}
	_, apiKeyB, _, _, _, err := st.ResolveAPIKey(context.Background(), "key-b")
	if err != nil || apiKeyB == nil {
		t.Fatalf("resolve key-b: %v", err)
	}

	return outboundAccessFixture{
		st:            st,
		tenantID:      tenantID,
		otherTenantID: otherTenantID,
		userA:         userA,
		userB:         userB,
		tenantAdmin:   tenantAdmin,
		platformAdmin: platformAdmin,
		apiKeyA:       *apiKeyA,
		apiKeyB:       *apiKeyB,
		apiKeyARaw:    "key-a",
		apiKeyBRaw:    "key-b",
	}
}

func doOutboundHandlerRequest(t *testing.T, st *testutil.FakeStore, fn http.HandlerFunc, method, path string, params map[string]string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(withRouteContext(req, rctx))
	}
	rr := httptest.NewRecorder()
	middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(fn)).ServeHTTP(rr, req)
	return rr
}

func outboundUserHeaders(t *testing.T, user *models.User) map[string]string {
	t.Helper()
	token, err := authn.IssueAccessToken(outboundTestJWTSecret, user)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func outboundDataLen(t *testing.T, rr *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return len(body.Data)
}
