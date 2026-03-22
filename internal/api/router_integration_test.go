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

	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mx.test", publicTenantID, middleware.NewRateLimiter(rdb, st, 20), zerolog.Nop())

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
	router := api.NewRouter(st, obj, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), policy.NamingFull, true, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, "admin-secret", "mx.test", publicTenantID, middleware.NewRateLimiter(rdb, st, 20), zerolog.Nop())

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
