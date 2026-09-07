package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func TestIngressRecoveryRouterPermissionsAndAudit(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Receiver", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "inbound.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	box := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "alice", ResolvedDomain: zone.Domain, FullAddress: "alice@inbound.test", AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, box))
	obj := testutil.NewMemoryObjectStore()
	svc := ingest.NewService(st, obj, resolver.New(st, policy.NamingFull, false), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	_, err := svc.Accept(ctx, ingest.Envelope{Source: "smtp", MailFrom: "external@test.example", Recipients: []string{box.FullAddress}}, []byte("Subject: test\r\n\r\nbody"), nil)
	must(t, err)
	c, err := st.ClaimIngress(ctx)
	must(t, err)
	must(t, st.FailIngressTarget(ctx, c, box.ID, "disk unavailable"))
	must(t, st.FinishIngress(ctx, c, 1, time.Now()))
	users := map[string]string{}
	for _, role := range []models.UserRole{models.RoleUser, models.RoleAdmin, models.RoleSuperAdmin} {
		u := &models.User{TenantID: tenant.ID, Email: string(role) + "@user.test", Role: role, IsActive: true, PasswordHash: "!fixture"}
		must(t, st.CreateUser(ctx, u))
		token, err := authn.IssueAccessToken("ingress-test-secret", u)
		must(t, err)
		users[string(role)] = token
	}
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, NamingMode: policy.NamingFull, JWTSecret: "ingress-test-secret", MailboxTokenSecret: "test", PublicTenantID: publicTenantID, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), Logger: zerolog.Nop()})
	call := func(method, path, token string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var b bytes.Buffer
		if body != nil {
			must(t, json.NewEncoder(&b).Encode(body))
		}
		r := httptest.NewRequest(method, path, &b)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, r)
		return rr
	}
	path := "/api/v1/admin/ingest/jobs/" + c.Job.ID.String()
	for _, token := range []string{"", users["user"], users["admin"]} {
		for _, method := range []string{"GET", "POST"} {
			suffix := "/recipients"
			if method == "POST" {
				suffix = "/retry"
			}
			rr := call(method, path+suffix, token, map[string]string{"reason": "repair"})
			if rr.Code != http.StatusForbidden {
				t.Fatalf("unexpected privilege: %d %s", rr.Code, rr.Body.String())
			}
		}
	}
	rr := call("GET", path+"/recipients", users["super_admin"], nil)
	if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list %d %s", rr.Code, rr.Body.String())
	}
	rr = call("POST", path+"/retry", users["super_admin"], map[string]string{})
	if rr.Code != 400 {
		t.Fatalf("missing reason %d", rr.Code)
	}
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_retry_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='ingest.retry' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER retry_audit_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_retry_audit()`)
	must(t, err)
	rr = call("POST", path+"/retry", users["super_admin"], map[string]string{"reason": "storage repaired"})
	if rr.Code != 500 {
		t.Fatalf("audit failure %d", rr.Code)
	}
	var state string
	must(t, pool.QueryRow(ctx, `SELECT state FROM ingest_jobs WHERE id=$1`, c.Job.ID).Scan(&state))
	if state != "dead" {
		t.Fatal("unaudited retry committed")
	}
	_, err = pool.Exec(ctx, `DROP TRIGGER retry_audit_fault ON audit_log`)
	must(t, err)
	rr = call("POST", path+"/retry", users["super_admin"], map[string]string{"reason": "storage repaired"})
	if rr.Code != 200 {
		t.Fatalf("retry %d %s", rr.Code, rr.Body.String())
	}
	rr = call("POST", path+"/retry", users["super_admin"], map[string]string{"reason": "again"})
	if rr.Code != 409 {
		t.Fatalf("replay %d", rr.Code)
	}
	rr = call("GET", "/api/v1/admin/ingest/jobs/"+uuid.New().String()+"/recipients", users["super_admin"], nil)
	if rr.Code != 404 {
		t.Fatalf("unknown receipt %d", rr.Code)
	}
}
