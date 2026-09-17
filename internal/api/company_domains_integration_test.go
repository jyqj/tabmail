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

// The /company/domains endpoints close the first-install gap: a company
// administrator must be able to add the company domain and drive its DNS
// verification before ConfigureCompany. Coverage: non-admin 403, creation,
// unverified verification checks, tenant isolation, and deletion (including
// the primary-domain guard).
func TestCompanyDomainsEndpoints(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()

	// Company A: administrator + ordinary employee.
	tenantA := &models.Tenant{Name: "Alpha Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(ctx, tenantA); err != nil {
		t.Fatal(err)
	}
	adminA := &models.User{TenantID: tenantA.ID, Email: "alpha-admin@contact.test", DisplayName: "Alpha Admin", Role: models.RoleAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	if err := st.CreateUser(ctx, adminA); err != nil {
		t.Fatal(err)
	}
	adminAActor := authz.Actor{Type: authz.PrincipalUser, ID: adminA.ID, TenantID: tenantA.ID, Role: models.RoleAdmin, IsAdmin: true}
	// Employee activation requires a verified primary company domain, so the
	// baseline company domain is provisioned before the workforce exists.
	primary := &models.DomainZone{TenantID: tenantA.ID, Domain: "primary.test", IsVerified: true, MXVerified: true}
	if err := st.CreateZone(ctx, primary); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfigureCompany(ctx, adminAActor, company.Settings{Name: "Alpha Co", PrimaryZoneID: primary.ID}); err != nil {
		t.Fatal(err)
	}
	hash := company.Hash("employee")
	if _, err := st.InviteEmployee(ctx, adminAActor, company.InvitationInput{Email: "alpha-worker@contact.test", LocalPart: "worker", DisplayName: "Worker"}, hash); err != nil {
		t.Fatal(err)
	}
	if err := st.ActivateEmployee(ctx, hash, "test-only-password-hash"); err != nil {
		t.Fatal(err)
	}
	worker, err := st.GetUserByEmail(ctx, "alpha-worker@contact.test")
	if err != nil {
		t.Fatal(err)
	}

	// Company B with its own administrator.
	tenantB := &models.Tenant{Name: "Beta Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(ctx, tenantB); err != nil {
		t.Fatal(err)
	}
	adminB := &models.User{TenantID: tenantB.ID, Email: "beta-admin@contact.test", DisplayName: "Beta Admin", Role: models.RoleAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	if err := st.CreateUser(ctx, adminB); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: testutil.NewMemoryObjectStore(), JWTSecret: "jwt-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, NamingMode: policy.NamingFull, StripPlus: true, HTTP: config.HTTP{CookieSecure: true}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), CompanyRepository: st, Logger: zerolog.Nop()})

	adminAToken := issueAccessTokenForExistingUser(t, adminA)
	workerToken := issueAccessTokenForExistingUser(t, worker)
	adminBToken := issueAccessTokenForExistingUser(t, adminB)

	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	// A plain employee is rejected everywhere on the domain surface.
	for _, tc := range [][2]string{
		{"POST", "/api/v1/company/domains"},
		{"GET", "/api/v1/company/domains"},
	} {
		if w := call(tc[0], tc[1], workerToken, `{"domain":"alpha.test"}`); w.Code != 403 {
			t.Fatalf("employee %s %s must be 403: %d %s", tc[0], tc[1], w.Code, w.Body.String())
		}
	}

	// Creation succeeds and returns the wizard contract: verification summary
	// plus the DNS records to publish.
	w := call("POST", "/api/v1/company/domains", adminAToken, `{"domain":"Alpha.Test"}`)
	if w.Code != 201 {
		t.Fatalf("create domain: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"domain":"alpha.test"`) || !strings.Contains(body, `"is_verified":false`) || !strings.Contains(body, `"mx_verified":false`) {
		t.Fatalf("create payload wrong: %s", body)
	}
	if !strings.Contains(body, `tabmail-verify=`) || !strings.Contains(body, `"expected_mx"`) {
		t.Fatalf("DNS guidance missing: %s", body)
	}
	if strings.Contains(body, "visibility") || strings.Contains(body, "allow_random_subdomains") || strings.Contains(body, "dkim_private_key") {
		t.Fatalf("platform fields leaked into company DTO: %s", body)
	}
	var domainID string
	if e := pool.QueryRow(ctx, `SELECT id FROM domain_zones WHERE tenant_id=$1 AND domain='alpha.test'`, tenantA.ID).Scan(&domainID); e != nil {
		t.Fatal(e)
	}

	// Listing stays inside the company.
	w = call("GET", "/api/v1/company/domains", adminAToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"domain":"alpha.test"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	// Triggering verification on an unconfigured domain returns the per-record
	// checks (TXT/MX fail without DNS) and persists the unverified state.
	w = call("POST", "/api/v1/company/domains/"+domainID+"/verify", adminAToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"checks"`) || !strings.Contains(w.Body.String(), `"txt":{"status":"fail"`) {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	w = call("GET", "/api/v1/company/domains/"+domainID+"/verification", adminAToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mx":{"status":"fail"`) || !strings.Contains(w.Body.String(), `"txt_record"`) {
		t.Fatalf("verification status: %d %s", w.Code, w.Body.String())
	}

	// Tenant isolation: company B neither sees nor operates company A's domain.
	w = call("GET", "/api/v1/company/domains", adminBToken, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "alpha.test") {
		t.Fatalf("cross-company listing leaked: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{
		"/api/v1/company/domains/" + domainID + "/verify",
		"/api/v1/company/domains/" + domainID + "/verification",
		"/api/v1/company/domains/" + domainID,
	} {
		method := "DELETE"
		if strings.HasSuffix(path, "verify") || strings.HasSuffix(path, "verification") {
			method = "POST"
			if strings.HasSuffix(path, "verification") {
				method = "GET"
			}
		}
		if w = call(method, path, adminBToken, ""); w.Code != 403 {
			t.Fatalf("cross-company %s %s must be 403: %d %s", method, path, w.Code, w.Body.String())
		}
	}

	// A misspelled domain can be removed before ConfigureCompany.
	w = call("POST", "/api/v1/company/domains", adminAToken, `{"domain":"typo.test"}`)
	if w.Code != 201 {
		t.Fatalf("create typo domain: %d %s", w.Code, w.Body.String())
	}
	var typoID string
	if e := pool.QueryRow(ctx, `SELECT id FROM domain_zones WHERE tenant_id=$1 AND domain='typo.test'`, tenantA.ID).Scan(&typoID); e != nil {
		t.Fatal(e)
	}
	if w = call("DELETE", "/api/v1/company/domains/"+typoID, adminAToken, ""); w.Code != 200 {
		t.Fatalf("delete typo domain: %d %s", w.Code, w.Body.String())
	}
	var remaining int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM domain_zones WHERE id=$1`, typoID).Scan(&remaining); e != nil || remaining != 0 {
		t.Fatalf("typo domain not deleted: %v remaining=%d", e, remaining)
	}

	// The configured primary domain is protected while company_settings pins it.
	if w = call("DELETE", "/api/v1/company/domains/"+primary.ID.String(), adminAToken, ""); w.Code != 409 {
		t.Fatalf("primary domain delete must be 409: %d %s", w.Code, w.Body.String())
	}
}
