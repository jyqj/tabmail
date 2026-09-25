package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/testutil"
)

const companyTestJWT = "test-only-company-jwt-secret"

func companyRouter(t *testing.T, f *companyFixture, obj *testutil.MemoryObjectStore, svc *outbound.Service) http.Handler {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return api.NewRouter(api.RouterConfig{Store: f.st, CompanyRepository: f.st, ObjectStore: obj, RawObjects: rawobject.NewStore(obj, f.st), JWTSecret: companyTestJWT, MailboxTokenSecret: "test-only-mailbox-secret", PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, f.st, 10000, nil), OutboundService: svc, Logger: zerolog.Nop(), Readiness: f.st.Readiness})
}
func r3Token(t *testing.T, u *models.User) string {
	t.Helper()
	v, e := authn.IssueAccessToken(companyTestJWT, u)
	must(t, e)
	return v
}
func r3HTTP(t *testing.T, h http.Handler, token, method, path string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	raw, e := json.Marshal(body)
	must(t, e)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if strings.HasPrefix(path, "/api/v1/company/drafts/") && strings.HasSuffix(path, "/submit") {
		r.Header.Set("Idempotency-Key", "http-workflow")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: wanted %d got %d: %s", method, path, status, w.Code, w.Body.String())
	}
	return w
}
func r3Data[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v struct {
		Data T `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &v))
	return v.Data
}
func TestR3CompanyHTTPJourney(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := testutil.NewMemoryObjectStore()
	smtp := newLocalSMTP(t)
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: smtp.ln.Addr().(*net.TCPAddr).Port, RelayTLS: "none", PollInterval: 5 * time.Millisecond, RetryDelay: time.Millisecond, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj)
	h := companyRouter(t, f, obj, svc)
	admin, employee := r3Token(t, f.admin), r3Token(t, f.employee)
	for _, path := range []string{"/api/v1/company/mailboxes", "/api/v1/admin/users"} {
		r3HTTP(t, h, "", "GET", path, nil, 401)
	}
	r3HTTP(t, h, "", "POST", "/api/v1/auth/register", map[string]string{"email": "public@test", "password": "test-long-password"}, 403)
	r3HTTP(t, h, employee, "POST", "/api/v1/company/invitations", map[string]string{"email": "x@contact.test", "local_part": "x", "display_name": "X"}, 403)
	invitation := r3Data[struct {
		ActivationToken string `json:"activation_token"`
	}](t, r3HTTP(t, h, admin, "POST", "/api/v1/company/invitations", map[string]string{"email": "new-http@contact.test", "local_part": "new-http", "display_name": "New HTTP"}, 200))
	r3HTTP(t, h, "", "POST", "/api/v1/company/activate", map[string]string{"token": invitation.ActivationToken, "password": "local-test-password-123"}, 200)
	r3HTTP(t, h, "", "POST", "/api/v1/company/activate", map[string]string{"token": invitation.ActivationToken, "password": "local-test-password-123"}, 400)
	login := r3Data[struct {
		AccessToken string `json:"access_token"`
	}](t, r3HTTP(t, h, "", "POST", "/api/v1/auth/login", map[string]string{"email": "new-http@contact.test", "password": "local-test-password-123"}, 200))
	if login.AccessToken == "" {
		t.Fatal("activation cannot log in")
	}
	boxes := r3Data[[]company.MailboxAccess](t, r3HTTP(t, h, login.AccessToken, "GET", "/api/v1/company/mailboxes", nil, 200))
	if len(boxes) != 1 || boxes[0].Mailbox.Kind != "personal" {
		t.Fatalf("provisioning not visible: %+v", boxes)
	}
	shared := f.shared.ID.String()
	grantPath := "/api/v1/company/mailboxes/" + shared + "/grants"
	grantSnapshot := r3Data[company.MailboxGrantSnapshot](t, r3HTTP(t, h, admin, "GET", grantPath, nil, 200))
	r3HTTP(t, h, admin, "PUT", grantPath, map[string]any{"user_id": f.employee.ID, "can_send": true, "template_only": true, "revision": grantSnapshot.Revision}, 200)
	r3HTTP(t, h, employee, "GET", "/api/v1/company/mailboxes/"+shared+"/messages", nil, 403)
	tpl := r3Data[company.Template](t, r3HTTP(t, h, admin, "POST", "/api/v1/company/templates", company.Template{Name: "HTTP template", Draft: templateDraft()}, 200))
	version := r3Data[company.TemplateVersion](t, r3HTTP(t, h, admin, "POST", "/api/v1/company/templates/"+tpl.ID.String()+"/publish", map[string]int{"revision": tpl.Revision}, 200))
	r3HTTP(t, h, admin, "PUT", "/api/v1/company/templates/"+tpl.ID.String()+"/grants", map[string]any{"mailbox_id": f.shared.ID, "user_id": f.employee.ID, "enabled": true}, 200)
	preview := r3Data[map[string]string](t, r3HTTP(t, h, employee, "POST", "/api/v1/company/templates/preview", map[string]any{"mailbox_id": f.shared.ID, "template_version_id": version.ID, "vars": map[string]string{"customer": "Client"}}, 200))
	if preview["subject"] != "Hello Client" {
		t.Fatal(preview)
	}
	// Multipart upload goes through the same permission boundary as sending.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, e := mw.CreateFormFile("file", "quote.txt")
	must(t, e)
	_, e = io.WriteString(part, "Confidential attachment test")
	must(t, e)
	must(t, mw.Close())
	req := httptest.NewRequest("POST", "/api/v1/company/mailboxes/"+shared+"/attachments", &buf)
	req.Header.Set("Authorization", "Bearer "+employee)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	attachment := r3Data[company.Attachment](t, w)
	draft := company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{To: []string{"client@recipient.test"}, BCC: []string{"hidden@recipient.test"}, Subject: "ignored", TextBody: "ignored", TemplateVersionID: &version.ID, TemplateVars: map[string]string{"customer": "Client"}, AttachmentIDs: []uuid.UUID{attachment.ID}}}
	saved := r3Data[company.Draft](t, r3HTTP(t, h, employee, "POST", "/api/v1/company/drafts", draft, 200))
	draft.ID = saved.ID
	r3HTTP(t, h, employee, "PUT", "/api/v1/company/drafts/"+saved.ID.String(), draft, 409)
	submitPath := "/api/v1/company/drafts/" + saved.ID.String() + "/submit"
	sent := r3Data[struct {
		ID uuid.UUID `json:"id"`
	}](t, r3HTTP(t, h, employee, "POST", submitPath, map[string]any{"expected_revision": saved.Revision}, 201))
	again := r3Data[struct {
		ID uuid.UUID `json:"id"`
	}](t, r3HTTP(t, h, employee, "POST", submitPath, map[string]any{"expected_revision": saved.Revision}, 200))
	if sent.ID != again.ID {
		t.Fatal("HTTP duplicate send")
	}
	svc.StartWorker(ctx)
	t.Cleanup(func() { cancel(); svc.Stop() })
	j := waitJob(t, f, sent.ID)
	if j.State != models.OutboundSent {
		t.Fatal(j.LastError)
	}
	smtp.mu.Lock()
	mime := append([]byte(nil), smtp.messages["client@recipient.test"][0]...)
	smtp.mu.Unlock()
	if !strings.Contains(string(mime), "quote.txt") || strings.Contains(string(mime), "hidden@recipient.test") {
		t.Fatalf("MIME attachment/BCC failed: %s", mime)
	}
	view := r3HTTP(t, h, admin, "GET", "/api/v1/outbound/"+j.ID.String(), nil, 200).Body.String()
	if strings.Contains(view, "hidden@recipient.test") || strings.Contains(view, "Confidential") || !strings.Contains(view, `"content_redacted":true`) {
		t.Fatal(view)
	}
	// A private original is readable, reply uses RFC-aware server parsing, and
	// soft deletion is reversible through the employee endpoints.
	raw := []byte("From: \"Client, One\" <client@recipient.test>\r\nTo: Employee <employee@company.test>\r\nCc: colleague@recipient.test\r\nSubject: Meeting\r\nMessage-ID: <original@recipient.test>\r\n\r\nPlease reply.")
	key := "http-original.eml"
	must(t, obj.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "client@recipient.test", Recipients: []string{f.personal.FullAddress}, Subject: "Meeting", RawObjectKey: key, Size: int64(len(raw))}
	must(t, f.st.CreateMessage(ctx, m))
	path := "/api/v1/company/mailboxes/" + f.personal.ID.String() + "/messages/" + m.ID.String()
	reply := r3Data[company.DraftPayload](t, r3HTTP(t, h, employee, "POST", path+"/compose", map[string]any{"mode": "reply_all", "from_mailbox_id": f.personal.ID}, 200))
	if len(reply.To) != 1 || reply.To[0] != "client@recipient.test" || reply.Headers["In-Reply-To"] != "<original@recipient.test>" {
		t.Fatalf("reply: %+v", reply)
	}
	r3HTTP(t, h, employee, "POST", path+"/actions", map[string]string{"action": "trash"}, 200)
	r3HTTP(t, h, employee, "POST", path+"/actions", map[string]string{"action": "restore"}, 200)
	// Missing object is not reported as a successfully empty body.
	must(t, obj.Delete(ctx, key))
	r3HTTP(t, h, employee, "GET", path, nil, 500)
	// The already-issued JWT is rejected once the employee is offboarded.
	r3HTTP(t, h, admin, "POST", "/api/v1/company/employees/"+f.employee.ID.String()+"/offboard", map[string]any{"successor_user_id": f.other.ID, "reason": "Employee departure and handover"}, 200)
	r3HTTP(t, h, employee, "GET", "/api/v1/company/mailboxes", nil, 401)
}

func TestR3PasswordChangeInvalidatesExistingAccess(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	hash, e := bcrypt.GenerateFromPassword([]byte("old-test-password"), bcrypt.MinCost)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, f.employee.ID, string(hash))
	must(t, e)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), nil)
	token := r3Token(t, f.employee)
	r3HTTP(t, h, token, "POST", "/api/v1/auth/change-password", map[string]string{"old_password": "incorrect", "new_password": "new-test-password"}, 403)
	r3HTTP(t, h, token, "POST", "/api/v1/auth/change-password", map[string]string{"old_password": "old-test-password", "new_password": "new-test-password"}, 200)
	r3HTTP(t, h, token, "GET", "/api/v1/company/mailboxes", nil, 401)
}

