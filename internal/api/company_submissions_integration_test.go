package api_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

// The /company/submissions endpoints are exercised against the real Postgres
// store so the company route wiring, actor resolution and the projection DTO
// are all validated end to end.
func TestCompanySubmissionsEndpoints(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Submission Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{TenantID: tenant.ID, Email: "admin@contact.test", DisplayName: "Administrator", Role: models.RoleAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	adminActor := authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "company.test", IsVerified: true, MXVerified: true}
	if err := st.CreateZone(ctx, zone); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfigureCompany(ctx, adminActor, company.Settings{Name: "Submission Co", PrimaryZoneID: zone.ID}); err != nil {
		t.Fatal(err)
	}
	hash := company.Hash("submitter")
	if _, err := st.InviteEmployee(ctx, adminActor, company.InvitationInput{Email: "worker@contact.test", LocalPart: "worker", DisplayName: "Worker"}, hash); err != nil {
		t.Fatal(err)
	}
	if err := st.ActivateEmployee(ctx, hash, "test-only-password-hash"); err != nil {
		t.Fatal(err)
	}
	worker, err := st.GetUserByEmail(ctx, "worker@contact.test")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := st.GetMailboxByAddress(ctx, "worker@company.test")
	if err != nil {
		t.Fatal(err)
	}
	job := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &worker.ID, SenderUserID: &worker.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, RcptTo: []string{"dest@client.test"}, To: []string{"dest@client.test"}, Subject: "api projection", State: models.OutboundSent, RecipientLedger: true}
	if err := st.CreateOutboundJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,'dest@client.test','accepted') ON CONFLICT (job_id,address) DO UPDATE SET state=EXCLUDED.state`, tenant.ID, job.ID); err != nil {
		t.Fatal(err)
	}
	// A foreign tenant's job must never appear.
	other := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &admin.ID, SenderUserID: &admin.ID, MailFrom: "admin@company.test", RcptTo: []string{"x@client.test"}, To: []string{"x@client.test"}, Subject: "admin secret", State: models.OutboundSent}
	if err := st.CreateOutboundJob(ctx, other); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	obj := testutil.NewMemoryObjectStore()
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, JWTSecret: "jwt-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, NamingMode: policy.NamingFull, StripPlus: true, HTTP: config.HTTP{CookieSecure: true}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), CompanyRepository: st, Logger: zerolog.Nop()})
	token := issueAccessTokenForExistingUser(t, worker)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/company/submissions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list submissions: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"status":"accepted"`) || !strings.Contains(body, "api projection") || !strings.Contains(body, `"total":1`) {
		t.Fatalf("list payload wrong: %s", body)
	}
	for _, leaked := range []string{"lease_until", "smtp_response", "claimed_at", "next_attempt_at", "max_attempts", `"attempts"`, "delivery_token", "raw_mime"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("queue internals leaked into submission view: %s", leaked)
		}
	}
	if strings.Contains(body, "admin secret") {
		t.Fatal("foreign submission leaked into the employee view")
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/company/submissions/"+job.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"recipients"`) {
		t.Fatalf("submission detail: %d %s", w.Code, w.Body.String())
	}

	// Another member's submission is 404 by id, admin title or not.
	adminToken := issueAccessTokenForExistingUser(t, admin)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/company/submissions/"+job.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("foreign submission detail must be 404: %d %s", w.Code, w.Body.String())
	}

	// Pagination follows the shared list convention.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/company/submissions?page=1&per_page=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"per_page":1`) {
		t.Fatalf("pagination params ignored: %d %s", w.Code, w.Body.String())
	}

	// Anonymous access is rejected.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/company/submissions", nil))
	if w.Code == 200 {
		t.Fatal("anonymous submissions access allowed")
	}
}
