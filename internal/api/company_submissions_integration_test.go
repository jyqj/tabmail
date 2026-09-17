package api_test

import (
	"context"
	"encoding/json"
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

// The sent-content endpoints expose the actual body and the pinned
// attachments to the same audience as the metadata view, and to nobody else:
// no BCC, no queue internals and no storage keys may leak on any of them.
func TestCompanySubmissionContentAndAttachmentEndpoints(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Content Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
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
	if _, err := st.ConfigureCompany(ctx, adminActor, company.Settings{Name: "Content Co", PrimaryZoneID: zone.ID}); err != nil {
		t.Fatal(err)
	}
	newEmployee := func(email, local string) (*models.User, authz.Actor) {
		hash := company.Hash(local)
		if _, err := st.InviteEmployee(ctx, adminActor, company.InvitationInput{Email: email, LocalPart: local, DisplayName: local}, hash); err != nil {
			t.Fatal(err)
		}
		if err := st.ActivateEmployee(ctx, hash, "test-only-password-hash"); err != nil {
			t.Fatal(err)
		}
		u, err := st.GetUserByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return u, authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: tenant.ID, Role: models.RoleUser}
	}
	worker, workerActor := newEmployee("worker@contact.test", "worker")
	mb, err := st.GetMailboxByAddress(ctx, "worker@company.test")
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := newEmployee("colleague@contact.test", "colleague")

	reserve := func(name string) *company.Attachment {
		a, e := st.ReserveMailAttachment(ctx, workerActor, company.Attachment{MailboxID: mb.ID, Filename: name, Size: int64(len("payload-bytes"))})
		if e != nil {
			t.Fatal(e)
		}
		if e := st.FinishMailAttachment(ctx, workerActor, a.ID, company.Hash("payload-bytes")); e != nil {
			t.Fatal(e)
		}
		return a
	}
	obj := testutil.NewMemoryObjectStore()
	put := func(a *company.Attachment) {
		if err := obj.Put(ctx, a.ObjectKey, strings.NewReader("payload-bytes"), int64(len("payload-bytes"))); err != nil {
			t.Fatal(err)
		}
	}
	pinned := reserve("report.csv")
	put(pinned)
	up := reserve("draft.bin")
	put(up)
	unlinked := reserve("elsewhere.txt")
	put(unlinked)

	job := &models.OutboundJob{
		TenantID: tenant.ID, ZoneID: zone.ID,
		UserID: &worker.ID, SenderUserID: &worker.ID, SenderMailboxID: &mb.ID,
		MailFrom: mb.FullAddress,
		RcptTo:   []string{"dest@client.test"}, To: []string{"dest@client.test"}, BCC: []string{"blind@client.test"},
		Subject: "sent content", TextBody: "see attached",
		HeadersJSON:   json.RawMessage(`{"X-Tag":"ok","Bcc":"hidden@client.test"}`),
		AttachmentIDs: []uuid.UUID{pinned.ID, up.ID}, State: models.OutboundSent,
	}
	if err := st.CreateOutboundJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, JWTSecret: "jwt-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, NamingMode: policy.NamingFull, StripPlus: true, HTTP: config.HTTP{CookieSecure: true}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), CompanyRepository: st, Logger: zerolog.Nop()})
	workerToken := issueAccessTokenForExistingUser(t, worker)
	adminToken := issueAccessTokenForExistingUser(t, admin)

	get := func(token, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	// Content: the submitter sees the actual body and the wire-safe header
	// subset; BCC and queue internals never appear.
	w := get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/content")
	if w.Code != 200 {
		t.Fatalf("submission content: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"subject":"sent content"`, `"text_body":"see attached"`, `"X-Tag":"ok"`, `"to":["dest@client.test"]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("content missing %s: %s", want, body)
		}
	}
	for _, leaked := range []string{"blind@client.test", "hidden@client.test", "object_key", "raw_mime", "lease_until", "smtp_response", "delivery_token", "bcc"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Fatalf("content leaked %s: %s", leaked, body)
		}
	}

	// Attachment list: only pinned attachments, metadata only.
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments")
	if w.Code != 200 {
		t.Fatalf("attachment list: %d %s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	if !strings.Contains(body, "report.csv") || !strings.Contains(body, "draft.bin") || strings.Contains(body, "elsewhere.txt") {
		t.Fatalf("attachment list wrong: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "attachment-") || strings.Contains(strings.ToLower(body), "object_key") || strings.Contains(body, "sha256") {
		t.Fatalf("attachment list leaked storage internals: %s", body)
	}

	// Download streams the verified object bytes.
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments/"+pinned.ID.String()+"/download")
	if w.Code != 200 || w.Body.String() != "payload-bytes" {
		t.Fatalf("attachment download: %d %q", w.Code, w.Body.String())
	}
	if disp := w.Header().Get("Content-Disposition"); !strings.Contains(disp, "report.csv") {
		t.Fatalf("download disposition wrong: %q", disp)
	}

	// An attachment pinned to a different job (or not pinned at all) is 404,
	// as is an unfinished upload.
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments/"+unlinked.ID.String()+"/download")
	if w.Code != 404 {
		t.Fatalf("unpinned attachment download must be 404: %d", w.Code)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_attachments SET state='uploading' WHERE id=$1`, up.ID); err != nil {
		t.Fatal(err)
	}
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments/"+up.ID.String()+"/download")
	if w.Code != 404 {
		t.Fatalf("unfinished attachment download must be 404: %d", w.Code)
	}

	// A current read grant on the sender mailbox confers content authority,
	// including downloads; without the grant, another member — administrator
	// or not — is collapsed to 404 on all three endpoints.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SetWorkGrant(ctx, adminActor, models.MailboxGrant{MailboxID: mb.ID, UserID: reader.ID, CanRead: true}))
	readerToken := issueAccessTokenForExistingUser(t, reader)
	for _, path := range []string{
		"/api/v1/company/submissions/" + job.ID.String() + "/content",
		"/api/v1/company/submissions/" + job.ID.String() + "/attachments",
		"/api/v1/company/submissions/" + job.ID.String() + "/attachments/" + pinned.ID.String() + "/download",
	} {
		w = get(readerToken, path)
		if w.Code != 200 {
			t.Fatalf("granted reader denied %s: %d %s", path, w.Code, w.Body.String())
		}
		w = get(adminToken, path)
		if w.Code != 404 {
			t.Fatalf("out-of-scope viewer must get 404 on %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	must(st.SetWorkGrant(ctx, adminActor, models.MailboxGrant{MailboxID: mb.ID, UserID: reader.ID, CanRead: false}))
	for _, path := range []string{
		"/api/v1/company/submissions/" + job.ID.String() + "/content",
		"/api/v1/company/submissions/" + job.ID.String() + "/attachments",
	} {
		if w = get(readerToken, path); w.Code != 404 {
			t.Fatalf("revoked reader must get 404 on %s: %d", path, w.Code)
		}
	}
}
