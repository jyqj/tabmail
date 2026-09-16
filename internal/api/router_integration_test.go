package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/settings"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

const publicTenantID = "00000000-0000-0000-0000-000000000001"

const testMetricsToken = "metrics-test-token"

// TestRouter_DocsAssetsSelfHosted pins the CDN removal: /docs and /redoc must
// reference only same-origin assets, and those assets must actually be served.
func TestRouter_DocsAssetsSelfHosted(t *testing.T) {
	st, obj, _ := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })

	router := testRouter(st, obj, rdb)

	for _, page := range []string{"/docs", "/redoc"} {
		req := httptest.NewRequest(http.MethodGet, page, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", page, rr.Code)
		}
		body := rr.Body.String()
		for _, forbidden := range []string{"unpkg.com", "cdn.redoc.ly", "https://"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still references external origin %q", page, forbidden)
			}
		}
	}

	for _, asset := range []string{
		"/docs-assets/swagger-ui.css",
		"/docs-assets/swagger-ui-bundle.js",
		"/docs-assets/redoc.standalone.js",
	} {
		req := httptest.NewRequest(http.MethodGet, asset, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", asset, rr.Code)
		}
		if rr.Body.Len() < 1024 {
			t.Fatalf("%s: suspiciously small body (%d bytes)", asset, rr.Body.Len())
		}
	}
}

func TestRouter_AdminCanListIngestJobsAndWebhookDeliveries(t *testing.T) {
	st, obj, _ := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })

	job := &models.IngestJob{
		ID:            uuid.New(),
		Source:        "smtp",
		RemoteIP:      "127.0.0.1",
		MailFrom:      "sender@example.org",
		Recipients:    []string{"user@mail.test"},
		RawObjectKey:  "sha256/aa/job.eml",
		State:         "retry",
		Attempts:      2,
		LastError:     "temporary failure",
		NextAttemptAt: time.Now().Add(time.Minute),
	}
	if err := st.CreateIngestJob(context.Background(), job, nil); err != nil {
		t.Fatal(err)
	}

	event := &models.OutboxEvent{
		ID:         uuid.New(),
		EventType:  "message.received",
		Payload:    []byte(`{"type":"message.received"}`),
		OccurredAt: time.Now(),
		State:      "done",
	}
	if err := st.CreateOutboxEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateWebhookDeliveries(context.Background(), event, []string{"https://example.com/hook"}); err != nil {
		t.Fatal(err)
	}

	router := testRouter(st, obj, rdb)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest/jobs", nil)
	setAdminAuth(t, st, req, uuid.Nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for ingest jobs, got %d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/webhooks/deliveries", nil)
	setAdminAuth(t, st, req, uuid.Nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for webhook deliveries, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRouter_MetricsExposeQueueDepthAndHistograms(t *testing.T) {
	st, obj, _ := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })

	if err := st.CreateIngestJob(context.Background(), &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  "raw/pending.eml",
		State:         "pending",
		NextAttemptAt: time.Now().Add(time.Minute),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateIngestJob(context.Background(), &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  "raw/processing.eml",
		State:         "processing",
		NextAttemptAt: time.Now().Add(time.Minute),
	}, nil); err != nil {
		t.Fatal(err)
	}

	router := testRouter(st, obj, rdb)

	// Unauthenticated scrapes are rejected.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /metrics, got %d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testMetricsToken)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, needle := range []string{
		"tabmail_ingest_queue_depth 2",
		"tabmail_ingest_queue_ready_depth 1",
		"tabmail_ingest_queue_inflight 1",
		"tabmail_ingest_job_latency_seconds_bucket",
		"tabmail_retention_sweep_duration_seconds_bucket",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected /metrics output to contain %q, got:\n%s", needle, body)
		}
	}
}

func TestRouter_UserSeesOnlyOwnedDomains(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	owner := seedUserForTest(t, st, tenantID, models.RoleUser)
	other := seedUserForTest(t, st, tenantID, models.RoleUser)
	ownedZone := &models.DomainZone{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OwnerUserID: &owner.ID,
		Domain:      "owned.mail.test",
		IsVerified:  true,
		MXVerified:  true,
		TXTRecord:   "tabmail-verify-owned",
	}
	otherZone := &models.DomainZone{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OwnerUserID: &other.ID,
		Domain:      "other.mail.test",
		IsVerified:  true,
		MXVerified:  true,
		TXTRecord:   "tabmail-verify-other",
	}
	st.SeedZone(ownedZone)
	st.SeedZone(otherZone)

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	req.Header.Set("Authorization", "Bearer "+issueAccessTokenForExistingUser(t, owner))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []models.DomainZone `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != ownedZone.ID {
		t.Fatalf("expected only owned zone, got %#v", body.Data)
	}
}

func TestRouter_ImpersonationRespectsTenantRateLimit(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	plan, err := st.GetPlan(context.Background(), uuid.MustParse("00000000-0000-0000-0000-000000000010"))
	if err != nil || plan == nil {
		t.Fatalf("get plan: %v plan=%#v", err, plan)
	}
	plan.RPMLimit = 1
	plan.DailyQuota = 100
	if err := st.UpdatePlan(context.Background(), plan); err != nil {
		t.Fatalf("update plan: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	router := testRouter(st, obj, rdb)

	mkReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
		setAdminAuth(t, st, req, tenantID)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}

	first := mkReq()
	if first.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d body=%s", first.Code, first.Body.String())
	}

	second := mkReq()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "RATE_LIMITED") {
		t.Fatalf("expected RATE_LIMITED error, got %s", second.Body.String())
	}
}

func TestRouter_UserJWTRespectsTenantRateLimit(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	plan, err := st.GetPlan(context.Background(), uuid.MustParse("00000000-0000-0000-0000-000000000010"))
	if err != nil || plan == nil {
		t.Fatalf("get plan: %v plan=%#v", err, plan)
	}
	plan.RPMLimit = 1
	plan.DailyQuota = 100
	if err := st.UpdatePlan(context.Background(), plan); err != nil {
		t.Fatalf("update plan: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	router := testRouter(st, obj, rdb)
	token := issueAccessTokenForTest(t, st, tenantID, models.RoleUser)

	mkReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}

	first := mkReq()
	if first.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d body=%s", first.Code, first.Body.String())
	}
	second := mkReq()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d body=%s", second.Code, second.Body.String())
	}
}

func TestRouter_APIKeyCannotUseInteractiveAuthRoutes(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("tenant-key", tenant, []string{"domains:read"})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/auth/me"},
		{method: http.MethodPost, path: "/api/v1/auth/logout"},
		{method: http.MethodPost, path: "/api/v1/auth/change-password", body: `{"old_password":"old","new_password":"new"}`},
		{method: http.MethodPost, path: "/api/v1/keys", body: `{"label":"self-elevate","scopes":["domains:read"]}`},
		{method: http.MethodGet, path: "/api/v1/keys"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "tenant-key")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403 for API key, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}
}

type countingStore struct {
	*testutil.FakeStore
	webhookCountCalls int
	ingestCountCalls  int
}

func (s *countingStore) CountWebhookDeliveriesByState(ctx context.Context, states ...string) (int, error) {
	s.webhookCountCalls++
	return s.FakeStore.CountWebhookDeliveriesByState(ctx, states...)
}

func (s *countingStore) CountIngestJobsByState(ctx context.Context, states ...string) (int, error) {
	s.ingestCountCalls++
	return s.FakeStore.CountIngestJobsByState(ctx, states...)
}

func TestRouter_MetricsDBCountsAreLightlyCached(t *testing.T) {
	base, obj, _ := seededStores(t)
	st := &countingStore{FakeStore: base}
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1, DialerRetries: 1, DialerRetryTimeout: time.Millisecond, DialTimeout: time.Millisecond, PoolTimeout: time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })

	router := testRouter(st, obj, rdb)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Header.Set("Authorization", "Bearer "+testMetricsToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected metrics request %d to return 200, got %d body=%s", i+1, rr.Code, rr.Body.String())
		}
	}

	if st.webhookCountCalls != 2 {
		t.Fatalf("expected cached metrics to hit webhook DB counts only once per query shape, got %d calls", st.webhookCountCalls)
	}
	if st.ingestCountCalls != 2 {
		t.Fatalf("expected cached metrics to hit ingest DB counts only once per query shape, got %d calls", st.ingestCountCalls)
	}
}

func seededStores(t *testing.T) (*testutil.FakeStore, *testutil.MemoryObjectStore, uuid.UUID) {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "test",
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              1000,
		DailyQuota:            1000,
	})
	st.SeedTenant(&models.Tenant{
		ID:      uuid.MustParse(publicTenantID),
		Name:    "public",
		PlanID:  planID,
		IsSuper: false,
	})
	tenantID := uuid.New()
	st.SeedTenant(&models.Tenant{
		ID:      tenantID,
		Name:    "tenant-a",
		PlanID:  planID,
		IsSuper: false,
	})
	st.SeedZone(&models.DomainZone{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=test",
	})
	return st, obj, tenantID
}

func findTenantZone(t *testing.T, st *testutil.FakeStore, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	zones, err := st.ListZones(context.Background(), tenantID)
	if err != nil || len(zones) == 0 {
		t.Fatalf("list zones: %v", err)
	}
	return zones[0].ID
}

func testRouter(st store.Store, obj *testutil.MemoryObjectStore, rdb *redis.Client) http.Handler {
	return api.NewRouter(api.RouterConfig{
		Store:              st,
		ObjectStore:        obj,
		Hub:                realtime.NewHub(10, st),
		Dispatcher:         hooks.New(hooks.Config{}, zerolog.Nop()),
		NamingMode:         policy.NamingFull,
		StripPlus:          true,
		DefaultPolicy:      models.SMTPPolicy{DefaultAccept: true, DefaultStore: true},
		JWTSecret:          "jwt-test-secret",
		MailboxTokenSecret: "mailbox-secret",
		ExpectedMXHost:     "mx.test",
		PublicTenantID:     publicTenantID,
		DefaultPlanID:      uuid.MustParse("00000000-0000-0000-0000-000000000010"),
		OpenRegistration:   true,
		Settings:           settings.NewManager(st, zerolog.Nop()),
		HTTP:               config.HTTP{MetricsToken: testMetricsToken},
		RateLimiter:        middleware.NewRateLimiter(rdb, st, 20, nil),
		Logger:             zerolog.Nop(),
	})
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any, headers map[string]string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("%s %s failed: %d %s", method, path, rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func adminHeaders(t *testing.T, st *testutil.FakeStore, tenantID uuid.UUID) map[string]string {
	t.Helper()
	token := issueAccessTokenForTest(t, st, uuid.MustParse(publicTenantID), models.RoleSuperAdmin)
	headers := map[string]string{"Authorization": "Bearer " + token}
	if tenantID != uuid.Nil {
		headers["X-Tenant-ID"] = tenantID.String()
	}
	return headers
}

func setAdminAuth(t *testing.T, st *testutil.FakeStore, req *http.Request, tenantID uuid.UUID) {
	t.Helper()
	for k, v := range adminHeaders(t, st, tenantID) {
		req.Header.Set(k, v)
	}
}

func seedUserForTest(t *testing.T, st *testutil.FakeStore, tenantID uuid.UUID, role models.UserRole) *models.User {
	t.Helper()
	user := &models.User{
		ID:           uuid.New(),
		TenantID:     tenantID,
		Email:        uuid.NewString() + "@example.test",
		PasswordHash: "hash",
		DisplayName:  "Test User",
		Role:         role,
		IsActive:     true,
	}
	if err := st.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user
}

func issueAccessTokenForExistingUser(t *testing.T, user *models.User) string {
	t.Helper()
	token, err := authn.IssueAccessToken("jwt-test-secret", user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func issueAccessTokenForTest(t *testing.T, st *testutil.FakeStore, tenantID uuid.UUID, role models.UserRole) string {
	t.Helper()
	return issueAccessTokenForExistingUser(t, seedUserForTest(t, st, tenantID, role))
}
