package api_test

import (
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
	"tabmail/internal/models"
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
	for _, who := range []*models.User{sender, reader} {
		for _, p := range []string{path, "/api/v1/outbound"} {
			w := rbRequest(t, h, who, "GET", p, "", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), j.TextBody) || !strings.Contains(w.Body.String(), `"content_redacted":false`) {
				t.Fatalf("authorized history %d %s", w.Code, w.Body.String())
			}
		}
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
}
func TestReleaseMailboxOwnerAndGrantList(t *testing.T) {
	st, obj, tenant := seededStores(t)
	admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
	employee := seedUserForTest(t, st, tenant, models.RoleUser)
	reader := seedUserForTest(t, st, tenant, models.RoleUser)
	stranger := seedUserForTest(t, st, tenant, models.RoleUser)
	h := rbRouter(t, st, obj)
	body := `{"address":"employee@mail.test","owner_user_id":"` + employee.ID.String() + `","retention_hours_override":0}`
	w := rbRequest(t, h, admin, "POST", "/api/v1/mailboxes", body, nil)
	if w.Code != 201 {
		t.Fatalf("owner create %d %s", w.Code, w.Body.String())
	}
	mb, _ := st.GetMailboxByAddress(context.Background(), "employee@mail.test")
	if mb.OwnerUserID == nil || *mb.OwnerUserID != employee.ID || mb.AccessMode != models.AccessToken {
		t.Fatal("ownership/default lost")
	}
	if err := st.SetMailboxGrant(context.Background(), &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: reader.ID, CanRead: true}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*models.User{employee, reader} {
		w := rbRequest(t, h, u, "GET", "/api/v1/mailboxes", "", nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), mb.ID.String()) {
			t.Fatalf("assigned mailbox undiscoverable: %d %s", w.Code, w.Body.String())
		}
	}
	if w := rbRequest(t, h, stranger, "GET", "/api/v1/mailboxes", "", nil); strings.Contains(w.Body.String(), mb.ID.String()) {
		t.Fatal("ungranted mailbox discovered")
	}
	if w := rbRequest(t, h, nil, "GET", "/api/v1/mailbox/employee@mail.test", "", nil); w.Code != 404 {
		t.Fatalf("anonymous personal access %d", w.Code)
	}
}
func TestReleaseMailboxRejectsInvalidOwner(t *testing.T) {
	for _, mode := range []string{"other-company", "admin", "inactive", "missing"} {
		t.Run(mode, func(t *testing.T) {
			st, obj, tenant := seededStores(t)
			admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
			owner := seedUserForTest(t, st, tenant, models.RoleUser)
			switch mode {
			case "other-company":
				owner.TenantID = uuid.MustParse(publicTenantID)
			case "admin":
				owner.Role = models.RoleAdmin
			case "inactive":
				owner.IsActive = false
			case "missing":
				owner.ID = uuid.New()
			}
			if mode != "missing" {
				if err := st.UpdateUser(context.Background(), owner); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]any{"address": "badowner@mail.test", "owner_user_id": owner.ID})
			w := rbRequest(t, rbRouter(t, st, obj), admin, "POST", "/api/v1/mailboxes", string(body), nil)
			if w.Code != 400 {
				t.Fatalf("invalid owner status %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// null explicitly clears a profile; a missing field must preserve it.
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
	got, _ := st.GetUser(context.Background(), employee.ID)
	if got.PermissionProfileID == nil || *got.PermissionProfileID != profile {
		t.Fatal("absent profile field was overwritten")
	}
	w = rbRequest(t, h, admin, "PATCH", path, `{"permission_profile_id":null}`, nil)
	if w.Code != 200 {
		t.Fatalf("patch null: %d %s", w.Code, w.Body.String())
	}
	got, _ = st.GetUser(context.Background(), employee.ID)
	if got.PermissionProfileID != nil {
		t.Fatal("explicit null did not clear permission profile")
	}
}
