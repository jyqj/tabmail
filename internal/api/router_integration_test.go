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
	"golang.org/x/crypto/bcrypt"

	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/testutil"
)

const publicTenantID = "00000000-0000-0000-0000-000000000001"

func TestRouter_PublicCannotManageDomains(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(`{"domain":"mail.example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	_ = tenantID
}

func TestRouter_MailboxTokenFlow(t *testing.T) {
	st, obj, tenantID := seededStores(t)

	hash, err := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	zoneID := findTenantZone(t, st, tenantID)
	mb := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      "secure",
		ResolvedDomain: "mail.test",
		FullAddress:    "secure@mail.test",
		AccessMode:     models.AccessToken,
	}
	s := string(hash)
	mb.PasswordHash = &s
	st.SeedMailbox(mb)
	if err := obj.Put(context.Background(), "raw/1.eml", bytes.NewBufferString("Subject: hello\r\n\r\nhello body"), 0); err != nil {
		t.Fatal(err)
	}
	st.SeedMessage(&models.Message{
		ID:           uuid.New(),
		TenantID:     tenantID,
		MailboxID:    mb.ID,
		ZoneID:       zoneID,
		Sender:       "sender@example.org",
		Recipients:   []string{mb.FullAddress},
		Subject:      "hello",
		RawObjectKey: "raw/1.eml",
		ReceivedAt:   time.Now(),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	tokenResp := doJSON(t, router, http.MethodPost, "/api/v1/token", map[string]any{
		"address":  mb.FullAddress,
		"password": "Passw0rd!",
	}, nil)
	token := tokenResp["data"].(map[string]any)["token"].(string)
	if token == "" {
		t.Fatal("expected mailbox token")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailbox/"+mb.FullAddress, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	items, ok := body["data"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 message, got %#v", body["data"])
	}
}

func TestRouter_CreateMailboxSupportsRetentionAndExpiry(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	expiresAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	body := doJSON(t, router, http.MethodPost, "/api/v1/mailboxes", map[string]any{
		"address":                  "retained@mail.test",
		"access_mode":              "token",
		"password":                 "Passw0rd!",
		"retention_hours_override": 6,
		"expires_at":               expiresAt.Format(time.RFC3339),
	}, map[string]string{
		"X-Admin-Key": "admin-secret",
		"X-Tenant-ID": tenantID.String(),
	})

	data := body["data"].(map[string]any)
	if data["retention_hours_override"].(float64) != 6 {
		t.Fatalf("expected retention override 6, got %#v", data["retention_hours_override"])
	}
	if data["expires_at"].(string) == "" {
		t.Fatalf("expected expires_at in response, got %#v", data)
	}

	mb, err := st.GetMailboxByAddress(context.Background(), "retained@mail.test")
	if err != nil {
		t.Fatal(err)
	}
	if mb == nil {
		t.Fatal("expected mailbox to be created")
	}
	if mb.RetentionHoursOverride == nil || *mb.RetentionHoursOverride != 6 {
		t.Fatalf("unexpected retention override: %#v", mb.RetentionHoursOverride)
	}
	if mb.ExpiresAt == nil || !mb.ExpiresAt.UTC().Equal(expiresAt) {
		t.Fatalf("unexpected expires_at: %#v", mb.ExpiresAt)
	}
}

func TestRouter_AdminCanListIngestJobsAndWebhookDeliveries(t *testing.T) {
	st, obj, _ := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
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
	if err := st.CreateIngestJob(context.Background(), job); err != nil {
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

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest/jobs", nil)
	req.Header.Set("X-Admin-Key", "admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for ingest jobs, got %d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/webhooks/deliveries", nil)
	req.Header.Set("X-Admin-Key", "admin-secret")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for webhook deliveries, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRouter_MetricsExposeQueueDepthAndHistograms(t *testing.T) {
	st, obj, _ := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })

	if err := st.CreateIngestJob(context.Background(), &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  "raw/pending.eml",
		State:         "pending",
		NextAttemptAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateIngestJob(context.Background(), &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  "raw/processing.eml",
		State:         "processing",
		NextAttemptAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
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

func TestRouter_SuggestAddressReturnsStructuredMailboxAddress(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("tenant-key", tenant, []string{"domains:read"})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	domainID := findTenantZone(t, st, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/"+domainID.String()+"/suggest-address", nil)
	req.Header.Set("X-API-Key", "tenant-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"]
	address, _ := data["address"].(string)
	if !strings.HasSuffix(address, "@mail.test") {
		t.Fatalf("unexpected address: %#v", address)
	}
	local, _ := data["local_part"].(string)
	if len(local) != 18 {
		t.Fatalf("unexpected local part length: %q", local)
	}
	if data["algorithm"] != policy.AddressSuggestionAlgorithm {
		t.Fatalf("unexpected algorithm: %#v", data["algorithm"])
	}
}

func TestRouter_SuggestAddressSupportsRandomSubdomain(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	tenant, err := st.GetTenant(context.Background(), tenantID)
	if err != nil || tenant == nil {
		t.Fatalf("get tenant: %v tenant=%#v", err, tenant)
	}
	st.RegisterAPIKey("tenant-key", tenant, []string{"domains:read"})

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	domainID := findTenantZone(t, st, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/domains/"+domainID.String()+"/suggest-address?subdomain=true", nil)
	req.Header.Set("X-API-Key", "tenant-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"]
	domain, _ := data["domain"].(string)
	subLabel, _ := data["subdomain_label"].(string)
	address, _ := data["address"].(string)
	if !strings.HasSuffix(domain, ".mail.test") || subLabel == "" {
		t.Fatalf("unexpected subdomain suggestion payload: %#v", data)
	}
	if !strings.Contains(address, "@"+domain) {
		t.Fatalf("expected address to use randomized subdomain %q, got %q", domain, address)
	}
	if data["mode"] != "subdomain" {
		t.Fatalf("unexpected mode: %#v", data["mode"])
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

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	mkReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes", nil)
		req.Header.Set("X-Admin-Key", "admin-secret")
		req.Header.Set("X-Tenant-ID", tenantID.String())
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
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mailbox-secret", "mx.test", publicTenantID, config.HTTP{}, middleware.NewRateLimiter(rdb, st, 20, nil), zerolog.Nop())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
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
