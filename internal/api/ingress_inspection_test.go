package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func TestIngressInspectionRouter(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Inspection API", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "inspect-api.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	box := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "a", ResolvedDomain: zone.Domain, FullAddress: "a@inspect-api.test", AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, box))
	obj := testutil.NewMemoryObjectStore()
	svc := ingest.NewService(st, obj, resolver.New(st, policy.NamingFull, false), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	_, err := svc.Accept(ctx, ingest.Envelope{Source: "smtp", Recipients: []string{box.FullAddress}}, []byte("Subject: test\r\n\r\nbody"), nil)
	must(t, err)
	claim, err := st.ClaimIngress(ctx)
	must(t, err)
	must(t, st.FinishIngress(ctx, claim, 1, time.Now()))
	tokens := map[string]string{}
	for _, role := range []models.UserRole{models.RoleUser, models.RoleAdmin, models.RoleSuperAdmin} {
		u := &models.User{TenantID: tenant.ID, Email: string(role) + "@inspect-api.test", PasswordHash: "!fixture", Role: role, IsActive: true}
		must(t, st.CreateUser(ctx, u))
		token, e := authn.IssueAccessToken("inspection-fixture-only", u)
		must(t, e)
		tokens[string(role)] = token
	}
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, NamingMode: policy.NamingFull, JWTSecret: "inspection-fixture-only", MailboxTokenSecret: "fixture", PublicTenantID: publicTenantID, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), Logger: zerolog.Nop()})
	call := func(method, path, token, key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var b bytes.Buffer
		if body != nil {
			must(t, json.NewEncoder(&b).Encode(body))
		}
		r := httptest.NewRequest(method, path, &b)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		result := httptest.NewRecorder()
		router.ServeHTTP(result, r)
		return result
	}
	path := "/api/v1/admin/ingest/jobs/" + claim.Job.ID.String()
	for _, token := range []string{"", tokens["user"], tokens["admin"]} {
		if res := call("GET", path, token, "", nil); res.Code != 403 {
			t.Fatalf("non-platform inspection %d", res.Code)
		}
	}
	key := "test-inspection-integration-key"
	hash := sha256.Sum256([]byte(key))
	must(t, st.CreateAPIKey(ctx, &models.TenantAPIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "test", Label: "fixture", Scopes: []string{"messages:read"}}))
	if res := call("GET", path, "", key, nil); res.Code != 403 {
		t.Fatalf("API key inspection %d", res.Code)
	}
	res := call("GET", path, tokens["super_admin"], "", nil)
	if res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("inspection %d %s", res.Code, res.Body.String())
	}
	var snap struct {
		Data store.IngressInspection `json:"data"`
	}
	must(t, json.Unmarshal(res.Body.Bytes(), &snap))
	if !snap.Data.CanRetry {
		t.Fatal("held receipt not recoverable")
	}
	list := call("GET", "/api/v1/admin/ingest/jobs", tokens["super_admin"], "", nil)
	if list.Code != 200 || list.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("list cache policy missing")
	}
	for _, v := range []string{"", "invalid", "0001-01-01T00:00:00Z"} {
		res = call("POST", path+"/retry", tokens["super_admin"], "", map[string]any{"reason": "repair", "observed_updated_at": v})
		if res.Code != 400 {
			t.Fatalf("invalid revision accepted: %d", res.Code)
		}
	}
	res = call("POST", path+"/retry", tokens["super_admin"], "", map[string]any{"reason": "repair", "observed_updated_at": snap.Data.UpdatedAt.Add(-time.Second).Format(time.RFC3339Nano)})
	if res.Code != 409 {
		t.Fatalf("stale review %d", res.Code)
	}
	// The documented character limit must work for multibyte characters too.
	reason := strings.Repeat("存储已修复", 360) // 1800 code points, greater than the previous 4096-byte limit.
	res = call("POST", path+"/retry", tokens["super_admin"], "", map[string]any{"reason": reason, "observed_updated_at": snap.Data.UpdatedAt.Format(time.RFC3339Nano)})
	if res.Code != 200 {
		t.Fatalf("valid Unicode reason %d %s", res.Code, res.Body.String())
	}
	var saved string
	must(t, pool.QueryRow(ctx, `SELECT details->>'reason' FROM audit_log WHERE action='ingest.retry'`).Scan(&saved))
	if saved != reason {
		t.Fatal("Unicode reason truncated")
	}
	res = call("POST", path+"/retry", tokens["super_admin"], "", map[string]any{"reason": strings.Repeat("字", 2001)})
	if res.Code != 400 {
		t.Fatal("overlong reason not rejected")
	}
	res = call("GET", "/api/v1/admin/ingest/jobs/"+uuid.New().String(), tokens["super_admin"], "", nil)
	if res.Code != 404 {
		t.Fatal("unknown receipt not 404")
	}
	res = call("GET", "/api/v1/admin/ingest/jobs/not-a-uuid", tokens["super_admin"], "", nil)
	if res.Code != 400 {
		t.Fatal("invalid id not 400")
	}
	legacy := &models.IngestJob{ID: uuid.New(), State: "dead", Source: "smtp", Recipients: []string{box.FullAddress}, RawObjectKey: "legacy.eml"}
	must(t, st.CreateIngestJob(ctx, legacy))
	res = call("GET", "/api/v1/admin/ingest/jobs/"+legacy.ID.String(), tokens["super_admin"], "", nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"retry_block_reason":"legacy_receipt"`) {
		t.Fatal("legacy receipt is ambiguous")
	}
}
