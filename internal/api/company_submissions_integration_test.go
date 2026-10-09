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
	job := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &worker.ID, SenderUserID: &worker.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, RcptTo: []string{"dest@client.test", "private@client.test"}, To: []string{"dest@client.test"}, BCC: []string{"private@client.test"}, Subject: "api projection", TextBody: "private API sent body", HeadersJSON: json.RawMessage(`{"X-Tag":"safe","Bcc":"blocked-header@client.test"}`), State: models.OutboundSent, RecipientLedger: true}
	if err := st.CreateOutboundJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbound_recipients SET state='accepted' WHERE tenant_id=$1 AND job_id=$2`, tenant.ID, job.ID); err != nil {
		t.Fatal(err)
	}
	// A different owner in this same tenant must never appear.
	other := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &admin.ID, SenderUserID: &admin.ID, MailFrom: "admin@company.test", RcptTo: []string{"x@client.test"}, To: []string{"x@client.test"}, Subject: "admin secret", State: models.OutboundSent}
	if err := st.CreateOutboundJob(ctx, other); err != nil {
		t.Fatal(err)
	}

	// True tenant isolation is an independent negative control, not a
	// same-tenant owner fixture labeled as cross-tenant.
	foreignTenant := &models.Tenant{Name: "Foreign receipt tenant", PlanID: tenant.PlanID}
	if err := st.CreateTenant(ctx, foreignTenant); err != nil {
		t.Fatal(err)
	}
	foreignUser := &models.User{TenantID: foreignTenant.ID, Email: "foreign@contact.test", PasswordHash: "test-only", Role: models.RoleUser, IsActive: true}
	if err := st.CreateUser(ctx, foreignUser); err != nil {
		t.Fatal(err)
	}
	foreignZone := &models.DomainZone{TenantID: foreignTenant.ID, Domain: "foreign-receipt.test"}
	if err := st.CreateZone(ctx, foreignZone); err != nil {
		t.Fatal(err)
	}
	foreignJob := &models.OutboundJob{TenantID: foreignTenant.ID, ZoneID: foreignZone.ID, UserID: &foreignUser.ID, SenderUserID: &foreignUser.ID, MailFrom: "foreign@foreign-receipt.test", RcptTo: []string{"foreign@client.test"}, To: []string{"foreign@client.test"}, Subject: "foreign tenant secret", State: models.OutboundSent}
	if err := st.CreateOutboundJob(ctx, foreignJob); err != nil {
		t.Fatal(err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	obj := testutil.NewMemoryObjectStore()
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, JWTSecret: "jwt-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, NamingMode: policy.NamingFull, StripPlus: true, HTTP: config.HTTP{CookieSecure: true}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), CompanyRepository: st, Logger: zerolog.Nop()})
	token := issueAccessTokenForExistingUser(t, worker)
	strictReceipt := func(raw json.RawMessage) company.Submission {
		t.Helper()
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{"id": true, "tenant_id": true, "state": true, "status": true, "progress": true, "created_at": true, "updated_at": true, "attempt_count": true, "next_retry": true, "delivery_uncertain": true, "capabilities": true}
		for name := range fields {
			if !allowed[name] {
				t.Fatalf("ordinary HTTP receipt outside strict whitelist: %s", name)
			}
		}
		for _, spec := range []struct {
			name    string
			allowed map[string]bool
		}{{"progress", map[string]bool{"completeness": true, "counts": true}}, {"capabilities", map[string]bool{"view_content": true, "retry": true, "retry_block_reason": true}}} {
			if raw, ok := fields[spec.name]; ok {
				var nested map[string]json.RawMessage
				if err := json.Unmarshal(raw, &nested); err != nil {
					t.Fatal(err)
				}
				for name := range nested {
					if !spec.allowed[name] {
						t.Fatalf("HTTP %s.%s outside whitelist", spec.name, name)
					}
				}
				if counts, ok := nested["counts"]; ok {
					var aggregate map[string]json.RawMessage
					if err := json.Unmarshal(counts, &aggregate); err != nil {
						t.Fatal(err)
					}
					for name := range aggregate {
						switch name {
						case "total", "accepted", "pending", "temporary", "permanent", "uncertain":
						default:
							t.Fatalf("HTTP progress.counts.%s outside whitelist", name)
						}
					}
				}
			}
		}
		for _, private := range []string{job.Subject, job.MailFrom, job.TextBody, job.To[0], job.BCC[0], "blocked-header@client.test", "admin secret", "foreign tenant secret", other.ID.String(), foreignJob.ID.String()} {
			if strings.Contains(string(raw), private) {
				t.Fatalf("ordinary HTTP receipt disclosed %q", private)
			}
		}
		var receipt company.Submission
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ID != job.ID || receipt.TenantID == nil || *receipt.TenantID != tenant.ID || receipt.State != models.OutboundSent || receipt.Status != "accepted" || receipt.Progress.Completeness != "known" || receipt.Progress.Counts == nil || *receipt.Progress.Counts != (company.OutboundReceiptCounts{Total: 2, Accepted: 2}) || receipt.DeliveryUncertain {
			t.Fatalf("HTTP receipt scope/full-ledger aggregate wrong: %+v", receipt)
		}
		return receipt
	}
	request := func(token, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/company/submissions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list submissions: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Data []json.RawMessage                  `json:"data"`
		Meta struct{ Total, Page, PerPage int } `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Meta.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("HTTP own/tenant scope wrong: %s", w.Body.String())
	}
	strictReceipt(list.Data[0])

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/company/submissions/"+job.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("submission detail: %d %s", w.Code, w.Body.String())
	}
	var detail struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	strictReceipt(detail.Data)
	// Sensitive source expectations are asserted only on the dedicated
	// current-readable/live content route, never an ordinary receipt.
	contentResponse := request(token, "/api/v1/company/submissions/"+job.ID.String()+"/content")
	if contentResponse.Code != 200 {
		t.Fatalf("dedicated current-read content: %d %s", contentResponse.Code, contentResponse.Body.String())
	}
	var content struct {
		Data company.SubmissionContent `json:"data"`
	}
	if err := json.Unmarshal(contentResponse.Body.Bytes(), &content); err != nil {
		t.Fatal(err)
	}
	if content.Data.ID != job.ID || content.Data.Subject != job.Subject || content.Data.MailFrom != job.MailFrom || content.Data.TextBody != job.TextBody || len(content.Data.To) != 1 || content.Data.To[0] != job.To[0] || len(content.Data.BCC) != 1 || content.Data.BCC[0] != job.BCC[0] || content.Data.RecipientCompleteness != "complete" {
		t.Fatal("dedicated content lost original sensitive source")
	}
	if strings.Contains(contentResponse.Body.String(), "blocked-header@client.test") {
		t.Fatal("structured BCC admitted a caller-supplied Bcc header")
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

	if w := request(token, "/api/v1/company/submissions/"+foreignJob.ID.String()); w.Code != 404 {
		t.Fatalf("cross-tenant receipt must be 404: %d %s", w.Code, w.Body.String())
	}
	if w := request(adminToken, "/api/v1/company/submissions/"+job.ID.String()+"/content"); w.Code != 404 {
		t.Fatalf("admin receipt title cannot grant content: %d %s", w.Code, w.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if w := request(token, "/api/v1/company/submissions/"+job.ID.String()+"/content"); w.Code != 404 {
		t.Fatalf("expired content must not follow retained receipt: %d %s", w.Code, w.Body.String())
	}
	retained := request(token, "/api/v1/company/submissions/"+job.ID.String())
	if retained.Code != 200 {
		t.Fatal("expired content erased valid operation receipt")
	}
	if err := json.Unmarshal(retained.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	strictReceipt(detail.Data)

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

// Dedicated sent-content exposes durable structured BCC only with CURRENT
// mailbox read and a LIVE sent item. Ordinary receipts never carry recipient
// details. Raw Bcc headers, queue internals and storage keys remain forbidden.
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
		// Preserve ordered/repeated structured categories independently from the
		// deduplicated delivery envelope. A To/CC overlap is not reclassified.
		RcptTo: []string{"dest@client.test", "copy@client.test", "blind@client.test"}, To: []string{"dest@client.test"}, CC: []string{"copy@client.test", "dest@client.test", "copy@client.test"}, BCC: []string{"blind@client.test", "blind@client.test"},
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
	// subset; structured snapshot BCC is distinct from a blocked raw Bcc header.
	w := get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/content")
	if w.Code != 200 {
		t.Fatalf("submission content: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"subject":"sent content"`, `"text_body":"see attached"`, `"X-Tag":"ok"`, `"to":["dest@client.test"]`, `"cc":["copy@client.test","dest@client.test","copy@client.test"]`, `"bcc":["blind@client.test","blind@client.test"]`, `"recipient_completeness":"complete"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("content missing %s: %s", want, body)
		}
	}
	for _, leaked := range []string{"hidden@client.test", "object_key", "raw_mime", "lease_until", "smtp_response", "delivery_token"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Fatalf("content leaked %s: %s", leaked, body)
		}
	}

	var contentProjection struct {
		Data company.SubmissionContent `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &contentProjection); err != nil {
		t.Fatal(err)
	}
	if contentProjection.Data.RecipientCompleteness != "complete" || len(contentProjection.Data.Headers) != 1 || contentProjection.Data.Headers["X-Tag"] != "ok" {
		t.Fatal("structured BCC altered safe-display headers/completeness")
	}
	for name := range contentProjection.Data.Headers {
		if strings.EqualFold(name, "bcc") {
			t.Fatal("caller-supplied raw Bcc header exposed")
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
	grantAccess, grantErr := st.GetWorkMailbox(ctx, adminActor, mb.ID)
	must(grantErr)
	must(st.SetWorkGrant(ctx, adminActor, models.MailboxGrant{MailboxID: mb.ID, UserID: reader.ID, CanRead: true}, grantAccess.Revision))
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
	grantAccess, grantErr = st.GetWorkMailbox(ctx, adminActor, mb.ID)
	must(grantErr)
	must(st.SetWorkGrant(ctx, adminActor, models.MailboxGrant{MailboxID: mb.ID, UserID: reader.ID, CanRead: false}, grantAccess.Revision))
	for _, path := range []string{
		"/api/v1/company/submissions/" + job.ID.String() + "/content",
		"/api/v1/company/submissions/" + job.ID.String() + "/attachments",
	} {
		if w = get(readerToken, path); w.Code != 404 {
			t.Fatalf("revoked reader must get 404 on %s: %d", path, w.Code)
		}
	}
	// The shipping detail source is the immutable archive, not the queue or
	// ledger. Removing both must preserve original To/CC/BCC/body/pinned files.
	if _, err := pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=$1)+(SELECT count(*) FROM outbound_recipients WHERE job_id=$1)`, job.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("queue/ledger cleanup control did not run")
	}
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/content")
	if w.Code != 200 {
		t.Fatalf("durable current-read content after queue cleanup: %d %s", w.Code, w.Body.String())
	}
	var durable struct {
		Data company.SubmissionContent `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &durable); err != nil {
		t.Fatal(err)
	}
	if durable.Data.Subject != job.Subject || durable.Data.TextBody != job.TextBody || durable.Data.HTMLBody != job.HTMLBody || durable.Data.RecipientCompleteness != "complete" || len(durable.Data.To) != len(job.To) || len(durable.Data.CC) != len(job.CC) || len(durable.Data.BCC) != len(job.BCC) {
		t.Fatal("queue cleanup lost durable structured content")
	}
	for i, address := range job.To {
		if durable.Data.To[i] != address {
			t.Fatal("queue cleanup changed To order/values")
		}
	}
	for i, address := range job.CC {
		if durable.Data.CC[i] != address {
			t.Fatal("queue cleanup changed CC order/values")
		}
	}
	for i, address := range job.BCC {
		if durable.Data.BCC[i] != address {
			t.Fatal("queue cleanup changed structured BCC")
		}
	}
	for name := range durable.Data.Headers {
		if strings.EqualFold(name, "bcc") {
			t.Fatal("queue cleanup exposed a raw Bcc header")
		}
	}

	for _, private := range []string{"hidden@client.test", `"Bcc":`, "object_key", "smtp_response", "raw_mime"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("durable snapshot leaked raw header/internals", private)
		}
	}
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "report.csv") || !strings.Contains(w.Body.String(), "draft.bin") {
		t.Fatal("queue cleanup lost archive attachment pins")
	}
	w = get(workerToken, "/api/v1/company/submissions/"+job.ID.String()+"/attachments/"+pinned.ID.String()+"/download")
	if w.Code != 200 || w.Body.String() != "payload-bytes" {
		t.Fatal("queue cleanup lost pinned attachment bytes")
	}
	if _, err := pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/company/submissions/" + job.ID.String() + "/content", "/api/v1/company/submissions/" + job.ID.String() + "/attachments", "/api/v1/company/submissions/" + job.ID.String() + "/attachments/" + pinned.ID.String() + "/download"} {
		if w = get(workerToken, path); w.Code != 404 {
			t.Fatalf("complete snapshot bypassed live-item qualification: %d %s", w.Code, w.Body.String())
		}
	}
}
