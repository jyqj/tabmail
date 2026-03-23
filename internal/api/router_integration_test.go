package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
