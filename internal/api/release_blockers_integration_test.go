package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"tabmail/internal/api/handlers"
	"tabmail/internal/authn"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func rbSeedRefresh(t *testing.T, st *testutil.FakeStore, u *models.User) *http.Cookie {
	t.Helper()
	raw, hash, err := authn.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err = st.CreateRefreshToken(context.Background(), &models.RefreshToken{UserID: u.ID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: handlers.RefreshCookieName, Value: raw}
}
func rbRefreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == handlers.RefreshCookieName && c.MaxAge > 0 {
			if !c.HttpOnly || !c.Secure || c.Path != "/api/v1/auth" {
				t.Fatalf("insecure refresh cookie: %+v", c)
			}
			return c
		}
	}
	t.Fatal("missing rotated cookie")
	return nil
}
func TestReleaseRefreshCookieFamilyAndReplay(t *testing.T) {
	st, obj, tenant := seededStores(t)
	u := seedUserForTest(t, st, tenant, models.RoleUser)
	h := rbRouter(t, st, obj)
	old := rbSeedRefresh(t, st, u)
	otherFamily := rbSeedRefresh(t, st, u)
	child := rbRefreshCookie(t, rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", old))
	rootRow, _ := st.GetRefreshToken(context.Background(), authn.HashToken(old.Value))
	childRow, _ := st.GetRefreshToken(context.Background(), authn.HashToken(child.Value))
	if rootRow.RevokedAt == nil || rootRow.FamilyID != childRow.FamilyID {
		t.Fatal("family not preserved in atomic rotation")
	}
	if w := rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", old); w.Code != 401 {
		t.Fatalf("replay accepted: %d", w.Code)
	}
	if w := rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", child); w.Code != 401 {
		t.Fatalf("family descendant survived replay: %d", w.Code)
	}
	rbRefreshCookie(t, rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", otherFamily))
}
func TestReleaseConcurrentRefreshSingleConsumer(t *testing.T) {
	st, obj, tenant := seededStores(t)
	u := seedUserForTest(t, st, tenant, models.RoleUser)
	h := rbRouter(t, st, obj)
	old := rbSeedRefresh(t, st, u)
	start := make(chan struct{})
	codes := make(chan int, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", old).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 401 {
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("old token consumed %d times", success)
	}
}
func TestReleaseLogoutRevokesConcurrentDescendants(t *testing.T) {
	st, obj, tenant := seededStores(t)
	u := seedUserForTest(t, st, tenant, models.RoleUser)
	h := rbRouter(t, st, obj)
	old := rbSeedRefresh(t, st, u)
	child := rbRefreshCookie(t, rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", old))
	if w := rbRequest(t, h, u, "POST", "/api/v1/auth/logout", "{}", old); w.Code != 204 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	if w := rbRequest(t, h, nil, "POST", "/api/v1/auth/refresh", "{}", child); w.Code != 401 {
		t.Fatal("logout left rotated child usable")
	}
	if w := rbRequest(t, h, u, "POST", "/api/v1/auth/logout", "{}", old); w.Code != 204 {
		t.Fatal("logout not idempotent")
	}
}
func TestReleaseInactiveAndExpiredRefreshDenied(t *testing.T) {
	for _, mode := range []string{"inactive", "expired"} {
		t.Run(mode, func(t *testing.T) {
			st, obj, tenant := seededStores(t)
			u := seedUserForTest(t, st, tenant, models.RoleUser)
			raw, hash, err := authn.GenerateRefreshToken()
			if err != nil {
				t.Fatal(err)
			}
			expiry := time.Now().Add(time.Hour)
			if mode == "expired" {
				expiry = time.Now().Add(-time.Hour)
			} else {
				u.IsActive = false
				if err = st.UpdateUser(context.Background(), u); err != nil {
					t.Fatal(err)
				}
			}
			if err = st.CreateRefreshToken(context.Background(), &models.RefreshToken{UserID: u.ID, TokenHash: hash, ExpiresAt: expiry}); err != nil {
				t.Fatal(err)
			}
			w := rbRequest(t, rbRouter(t, st, obj), nil, "POST", "/api/v1/auth/refresh", "{}", &http.Cookie{Name: handlers.RefreshCookieName, Value: raw})
			if w.Code != 401 {
				t.Fatalf("%s refresh %d", mode, w.Code)
			}
		})
	}
}
func TestReleaseSuperAdminSelectedTenantAndLastAdmin(t *testing.T) {
	st, obj, tenant := seededStores(t)
	operator := seedUserForTest(t, st, uuid.MustParse(publicTenantID), models.RoleSuperAdmin)
	target := seedUserForTest(t, st, tenant, models.RoleAdmin)
	h := rbRouter(t, st, obj)
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PATCH", "/api/v1/admin/users/"+target.ID.String(), strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+issueAccessTokenForExistingUser(t, operator))
		r.Header.Set("X-Tenant-ID", tenant.String())
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(`{"display_name":"Managed company admin"}`); w.Code != 200 {
		t.Fatalf("impersonation failed %d %s", w.Code, w.Body.String())
	}
	if w := request(`{"is_active":false}`); w.Code != 409 {
		t.Fatalf("last admin removal %d %s", w.Code, w.Body.String())
	}
	seedUserForTest(t, st, tenant, models.RoleAdmin)
	if w := request(`{"role":"user","is_active":false}`); w.Code != 200 {
		t.Fatalf("guard rejected valid removal %d %s", w.Code, w.Body.String())
	}
}
func TestReleaseSharedOutboundReadIsNotRetry(t *testing.T) {
	st, obj, tenant := seededStores(t)
	ctx := context.Background()
	sender := seedUserForTest(t, st, tenant, models.RoleUser)
	reader := seedUserForTest(t, st, tenant, models.RoleUser)
	admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: findTenantZone(t, st, tenant), FullAddress: "shared@mail.test", AccessMode: models.AccessToken}
	st.SeedMailbox(mb)
	j := &models.OutboundJob{TenantID: tenant, ZoneID: mb.ZoneID, UserID: &sender.ID, SenderUserID: &sender.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, To: []string{"visible@example.test"}, BCC: []string{"bcc-hidden@example.test"}, RcptTo: []string{"visible@example.test", "bcc-hidden@example.test"}, Subject: "Shared history", TextBody: "shared-sensitive-body", State: models.OutboundFailed}
	if err := st.CreateOutboundJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	attempt := &models.OutboundAttempt{JobID: j.ID, TenantID: tenant, Error: "RCPT bcc-hidden@example.test failed", SMTPResponse: "bcc-hidden@example.test"}
	if err := st.CreateOutboundAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	h := rbRouter(t, st, obj)
	path := "/api/v1/outbound/" + j.ID.String()
	if w := rbRequest(t, h, reader, "GET", path, "", nil); w.Code != 404 {
		t.Fatalf("ungranted detail %d", w.Code)
	}
	grant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: reader.ID, CanRead: true}
	if err := st.SetMailboxGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	// Ordinary detail/list stay receipt-only even for a current mailbox reader.
	// The independent real content route is exercised below with fresh PG.
	assertReceipt := func(w *httptest.ResponseRecorder, list, canRead bool) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("receipt status=%d", w.Code)
		}
		for _, secret := range []string{j.TextBody, j.Subject, j.MailFrom, j.To[0], j.BCC[0], "content_redacted"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("ordinary receipt disclosed content or a retired marker")
			}
		}
		var envelope struct{ Data json.RawMessage }
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		data := envelope.Data
		if list {
			var rows []json.RawMessage
			if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 1 {
				t.Fatalf("receipt list count=%d error=%v", len(rows), err)
			}
			data = rows[0]
		}
		assertFields := func(raw json.RawMessage, names string) map[string]json.RawMessage {
			t.Helper()
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
				t.Fatalf("receipt object missing or malformed: %v", err)
			}
			allowed := map[string]bool{}
			for _, name := range strings.Fields(names) {
				allowed[name] = true
			}
			for name := range fields {
				if !allowed[name] {
					t.Fatalf("ordinary receipt field outside explicit whitelist: %s", name)
				}
			}
			return fields
		}
		fields := assertFields(data, "id tenant_id state status progress created_at updated_at attempt_count next_retry delivery_uncertain capabilities")
		progress := assertFields(fields["progress"], "completeness counts")
		assertFields(progress["counts"], "total accepted pending temporary permanent uncertain")
		assertFields(fields["capabilities"], "view_content retry retry_block_reason")
		var receipt company.OutboundReceipt
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields() // Includes nested progress/counts/capabilities.
		if err := decoder.Decode(&receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ID != j.ID || receipt.TenantID == nil || *receipt.TenantID != tenant || receipt.State != models.OutboundFailed || receipt.Status != "needs_attention" || receipt.Progress.Completeness != "known" || receipt.Progress.Counts == nil || *receipt.Progress.Counts != (company.OutboundReceiptCounts{Total: 2, Pending: 2}) || receipt.DeliveryUncertain {
			t.Fatal("ordinary receipt lost identity or complete ledger progress")
		}
		if receipt.Capabilities == nil || receipt.Capabilities.ViewContent != canRead || receipt.Capabilities.Retry {
			t.Fatal("read authority widened retry or changed current content capability")
		}
		if canRead && !list && receipt.Capabilities.RetryBlockReason != "sender_authority" {
			t.Fatal("reader retry refusal lost sender-authority reason")
		}
	}
	for _, p := range []string{path, "/api/v1/outbound"} {
		w := rbRequest(t, h, reader, "GET", p, "", nil)
		assertReceipt(w, p == "/api/v1/outbound", true)
		w = rbRequest(t, h, sender, "GET", p, "", nil)
		assertReceipt(w, p == "/api/v1/outbound", false)
	}
	if w := rbRequest(t, h, reader, "POST", path+"/retry", "{}", nil); w.Code != 403 {
		t.Fatalf("read-only grant retried: %d %s", w.Code, w.Body.String())
	}
	if w := rbRequest(t, h, admin, "GET", path+"/attempts", "", nil); w.Code != 200 || strings.Contains(w.Body.String(), "bcc-hidden") {
		t.Fatalf("attempt leak %d %s", w.Code, w.Body.String())
	}
	grant.CanRead = false
	if err := st.SetMailboxGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if w := rbRequest(t, h, reader, "GET", path, "", nil); w.Code != 404 {
		t.Fatal("revoked grant still fetched shared history")
	}
	if w := rbRequest(t, h, reader, "GET", "/api/v1/outbound", "", nil); strings.Contains(w.Body.String(), j.ID.String()) {
		t.Fatal("revoked grant still listed shared history")
	}
	stored, _ := st.GetOutboundJob(ctx, j.ID)
	if stored.TextBody != j.TextBody || len(stored.BCC) != 1 {
		t.Fatal("redaction mutated stored mail")
	}
	t.Run("current_content_route", func(t *testing.T) {
		f := testpg.NewR5HTTPFixture(t)
		c := f.Companies[0]
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		setRead := func(role string, read bool) {
			t.Helper()
			box, err := f.Store.GetWorkMailbox(ctx, c.Actor, c.Shared.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Store.SetWorkGrant(ctx, c.Actor, models.MailboxGrant{TenantID: c.Tenant.ID, MailboxID: c.Shared.ID, UserID: c.Users[role].ID, CanRead: read}, box.Revision); err != nil {
				t.Fatal(err)
			}
		}
		setRead("sender", false)
		job := &models.OutboundJob{TenantID: c.Tenant.ID, ZoneID: c.Zone.ID, UserID: &c.Users["sender"].ID, SenderUserID: &c.Users["sender"].ID, SenderMailboxID: &c.Shared.ID, MailFrom: c.Shared.FullAddress, To: []string{"visible@example.test"}, BCC: []string{"bcc-hidden@example.test"}, RcptTo: []string{"visible@example.test", "bcc-hidden@example.test"}, Subject: "Shared history", TextBody: "shared-sensitive-body", State: models.OutboundFailed}
		if err := f.Store.CreateOutboundJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		contentPath := "/api/v1/company/submissions/" + job.ID.String() + "/content"
		status, raw := f.Request(t, ctx, f.JWT(0, "reader"), "GET", contentPath, nil, "")
		var content struct{ Data company.SubmissionContent }
		if status != 200 || json.Unmarshal(raw, &content) != nil || content.Data.ID != job.ID || content.Data.TextBody != job.TextBody || content.Data.Subject != job.Subject || content.Data.MailFrom != job.MailFrom || len(content.Data.To) != 1 || content.Data.To[0] != job.To[0] || len(content.Data.BCC) != 1 || content.Data.BCC[0] != job.BCC[0] || content.Data.RecipientCompleteness != "complete" || content.Data.ContentRedacted {
			t.Fatal("current reader did not receive the independent durable content projection")
		}
		for _, role := range []string{"sender", "admin"} {
			if status, _ := f.Request(t, ctx, f.JWT(0, role), "GET", contentPath, nil, ""); status != 404 {
				t.Fatalf("%s without current read grant received content: status=%d", role, status)
			}
		}
		if status, _ := f.Request(t, ctx, f.JWT(0, "reader"), "POST", "/api/v1/outbound/"+job.ID.String()+"/retry", map[string]any{}, ""); status != 403 {
			t.Fatalf("current content reader retried: status=%d", status)
		}
		setRead("reader", false)
		if status, _ := f.Request(t, ctx, f.JWT(0, "reader"), "GET", contentPath, nil, ""); status != 404 {
			t.Fatalf("revoked reader retained content: status=%d", status)
		}
	})
}

// An absent profile field preserves the ordinary member PATCH path. Explicit
// null/present profile intent must now use the versioned assignment protocol;
// this legacy entry rejects it rather than silently clearing or assigning.
func TestReleaseMemberPatchNullVersusAbsent(t *testing.T) {
	st, obj, tenant := seededStores(t)
	admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
	employee := seedUserForTest(t, st, tenant, models.RoleUser)
	profile := uuid.New()
	employee.PermissionProfileID = &profile
	if err := st.UpdateUser(context.Background(), employee); err != nil {
		t.Fatal(err)
	}
	h := rbRouter(t, st, obj)
	path := "/api/v1/admin/users/" + employee.ID.String()
	w := rbRequest(t, h, admin, "PATCH", path, `{"display_name":"preserve profile"}`, nil)
	if w.Code != 200 {
		t.Fatalf("patch absent: %d %s", w.Code, w.Body.String())
	}
	got, err := st.GetUser(context.Background(), employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PermissionProfileID == nil || *got.PermissionProfileID != profile || got.DisplayName != "preserve profile" {
		t.Fatal("omitted profile intent did not preserve assignment and ordinary member field update")
	}
	before := *got
	audits, err := st.ListAuditEntries(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"permission_profile_id":null}`,
		`{"permission_profile_id":"` + profile.String() + `"}`,
		`{"display_name":"must not partially update","permission_profile_id":null}`,
	} {
		w = rbRequest(t, h, admin, "PATCH", path, body, nil)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"CONFLICT"`) || !strings.Contains(w.Body.String(), "use permission-editor/assignment with expected_revision") {
			t.Fatalf("legacy explicit profile intent must reject upgraded protocol: %d %s", w.Code, w.Body.String())
		}
		got, err = st.GetUser(context.Background(), employee.ID)
		if err != nil {
			t.Fatal(err)
		}
		// This existing fixture has no persistent permission-revision adapter;
		// compare the complete stored member (including session version and
		// UpdatedAt) and audit ledger, without fabricating a CAS observation.
		if got == nil || *got != before {
			t.Fatal("rejected profile intent changed member state/version or partially applied other fields")
		}
		afterAudits, err := st.ListAuditEntries(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(afterAudits) != len(audits) {
			t.Fatal("rejected legacy assignment created an audit")
		}
		for i, entry := range afterAudits {
			if entry.ID != audits[i].ID || entry.Action != audits[i].Action || string(entry.Details) != string(audits[i].Details) {
				t.Fatal("rejected legacy assignment altered an existing audit")
			}
		}
	}
}
