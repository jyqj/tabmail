//go:build r5protocol

package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

const r5UIJWT = "r5-ui-disposable-test-secret"

type r5UICase struct {
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
}
type r5UITrace struct {
	Method           string   `json:"method"`
	Path             string   `json:"path"`
	Status           int      `json:"status"`
	ResponseSHA256   string   `json:"response_sha256"`
	RequestFields    []string `json:"request_fields,omitempty"`
	ErrorCode        string   `json:"error_code,omitempty"`
	TransportAborted bool     `json:"transport_aborted_after_commit,omitempty"`
	IdempotencySHA   string   `json:"idempotency_key_sha256,omitempty"`
}
type r5UIFixture struct {
	st                         *postgres.PgStore
	pool                       *pgxpool.Pool
	tenant                     *models.Tenant
	admin, employee, successor *models.User
	zone                       *models.DomainZone
	personal, shared           *models.Mailbox
	actor, member              authz.Actor
	server                     *httptest.Server
	mu                         sync.Mutex
	trace                      []r5UITrace
	cancelTransport            context.CancelFunc
	activeRequests             sync.WaitGroup
	afterCommitFault           func(*http.Request, *httptest.ResponseRecorder) (bool, error)
}

func r5UIMust(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func r5UIToken(t *testing.T, u *models.User) string {
	t.Helper()
	v, e := authn.IssueAccessToken(r5UIJWT, u)
	r5UIMust(t, e)
	return v
}
func r5UIInput[T any](t *testing.T, c r5UICase, key string) T {
	t.Helper()
	var m map[string]json.RawMessage
	r5UIMust(t, json.Unmarshal(c.Input, &m))
	var v T
	if raw, ok := m[key]; ok {
		r5UIMust(t, json.Unmarshal(raw, &v))
	}
	return v
}
func r5UISeed(t *testing.T) *r5UIFixture {
	t.Helper()
	ctx := context.Background()
	st, pool, _ := testpg.NewPostgres(t)
	f := &r5UIFixture{st: st, pool: pool}
	f.tenant = &models.Tenant{Name: "Shared component isolated company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	r5UIMust(t, st.CreateTenant(ctx, f.tenant))
	f.admin = &models.User{TenantID: f.tenant.ID, Email: "admin@ui-fixture.test", DisplayName: "UI administrator", Role: models.RoleAdmin, IsActive: true, PasswordHash: "test-only"}
	r5UIMust(t, st.CreateUser(ctx, f.admin))
	f.actor = authz.Actor{Type: authz.PrincipalUser, ID: f.admin.ID, TenantID: f.tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	f.zone = &models.DomainZone{TenantID: f.tenant.ID, Domain: "ui-fixture.test", IsVerified: true, MXVerified: true}
	r5UIMust(t, st.CreateZone(ctx, f.zone))
	_, e := st.ConfigureCompany(ctx, f.actor, company.Settings{Name: "Shared UI company", PrimaryZoneID: f.zone.ID})
	r5UIMust(t, e)
	for i, local := range []string{"employee", "successor"} {
		email := local + "@contact.ui-fixture.test"
		hash := company.Hash("private-ui-invite-" + local)
		_, e := st.InviteEmployee(ctx, f.actor, company.InvitationInput{Email: email, LocalPart: local, DisplayName: local}, hash)
		r5UIMust(t, e)
		r5UIMust(t, st.ActivateEmployee(ctx, hash, "test-only"))
		u, e := st.GetUserByEmail(ctx, email)
		r5UIMust(t, e)
		if i == 0 {
			f.employee = u
			f.member = authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
		} else {
			f.successor = u
		}
	}
	f.personal, e = st.GetMailboxByAddress(ctx, "employee@ui-fixture.test")
	r5UIMust(t, e)
	f.shared, e = st.CreateWorkMailbox(ctx, f.actor, company.MailboxInput{LocalPart: "shared", Kind: "shared"})
	r5UIMust(t, e)
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, st, st, zerolog.Nop())
	svc.SetObjectStore(obj)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: st, CompanyRepository: st, ObjectStore: obj, RawObjects: rawobject.NewStore(obj, st), JWTSecret: r5UIJWT, MailboxTokenSecret: "ui-fixture-only", PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), OutboundService: svc, Logger: zerolog.Nop(), Readiness: st.Readiness})
	transportContext, cancelTransport := context.WithCancel(context.Background())
	f.cancelTransport = cancelTransport
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.activeRequests.Add(1)
		defer f.activeRequests.Done()
		raw, e := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if e != nil {
			http.Error(w, "fixture transport failed", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		// Only the authorized lost-submit-response fault buffers a finite JSON
		// command. All other routes reach the real socket writer immediately.
		faultRoute := f.afterCommitFault != nil && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/submit")
		var captured *httptest.ResponseRecorder
		destination := w
		if faultRoute {
			captured = httptest.NewRecorder()
			destination = captured
		}
		observed := &r5UIResponseWriter{ResponseWriter: destination, digest: sha256.New()}
		router.ServeHTTP(observed, r)
		if observed.status == 0 {
			observed.status = http.StatusOK
		}
		trace := r5UITrace{Method: r.Method, Path: r.URL.Path, Status: observed.status, ResponseSHA256: hex.EncodeToString(observed.digest.Sum(nil))}
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) == nil {
			for k := range body {
				trace.RequestFields = append(trace.RequestFields, k)
			}
		}
		var envelope struct{ Error struct{ Code string } }
		_ = json.Unmarshal(observed.jsonBody.Bytes(), &envelope)
		trace.ErrorCode = envelope.Error.Code
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			sum := sha256.Sum256([]byte(key))
			trace.IdempotencySHA = hex.EncodeToString(sum[:])
		}
		aborted := false
		if faultRoute {
			var faultError error
			aborted, faultError = f.afterCommitFault(r, captured)
			if faultError != nil {
				panic(faultError)
			}
		}
		trace.TransportAborted = aborted
		f.mu.Lock()
		f.trace = append(f.trace, trace)
		f.mu.Unlock()
		if aborted {
			panic(http.ErrAbortHandler)
		}
		if !faultRoute {
			return
		}
		for k, vs := range captured.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(captured.Code)
		_, _ = w.Write(captured.Body.Bytes())
	}))
	f.server.Config.BaseContext = func(net.Listener) context.Context { return transportContext }
	f.server.Start()
	t.Cleanup(f.closeTransport)
	return f
}

// Unwrap preserves ResponseController deadlines and flush on the real writer.
// Evidence hashes only bytes successfully written; SSE payloads are never retained.
type r5UIResponseWriter struct {
	http.ResponseWriter
	status   int
	digest   hash.Hash
	jsonBody bytes.Buffer
}

func (w *r5UIResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *r5UIResponseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *r5UIResponseWriter) Flush() { _ = w.FlushError() }
func (w *r5UIResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *r5UIResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	_, _ = w.digest.Write(p[:n])
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") && w.jsonBody.Len()+n <= 2<<20 {
		_, _ = w.jsonBody.Write(p[:n])
	}
	return n, err
}
func (f *r5UIFixture) closeTransport() {
	f.cancelTransport()
	f.server.CloseClientConnections()
	f.server.Close()
	f.activeRequests.Wait()
	f.server.Client().CloseIdleConnections()
}

// Seed raw intent through the shipping versioned command, then independently
// reload it. Effective permissions alone cannot prove an override was persisted.
func r5UIReadPermissionEditor(t *testing.T, f *r5UIFixture, token string) company.PermissionEditorSnapshot {
	t.Helper()
	raw := r5UICall(t, f, token, "GET", "/api/v1/admin/users/"+f.employee.ID.String()+"/permission-editor", nil, 200)
	var env struct {
		Data company.PermissionEditorSnapshot
	}
	r5UIMust(t, json.Unmarshal(raw, &env))
	r5UIMust(t, env.Data.Revision.Validate())
	if env.Data.UserID != f.employee.ID || env.Data.TenantID != f.tenant.ID || env.Data.Revision.UserID != f.employee.ID || env.Data.Revision.TenantID != f.tenant.ID {
		t.Fatal("permission seed readback identity differs")
	}
	return env.Data
}

func r5UISeedPermissionOverrides(t *testing.T, f *r5UIFixture, token string) {
	t.Helper()
	before := r5UIReadPermissionEditor(t, f, token)
	r5UICall(t, f, token, "PATCH", "/api/v1/admin/users/"+f.employee.ID.String()+"/permission-editor", map[string]any{
		"expected_revision": before.Revision,
		"patch":             map[string]any{"can_send": false, "domain_access": company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{f.zone.ID}}, "daily_send_quota": 19},
	}, 200)
	after := r5UIReadPermissionEditor(t, f, token)
	raw := after.Overrides
	if raw == nil || raw.CanSend == nil || *raw.CanSend || raw.DailySendQuota == nil || *raw.DailySendQuota != 19 || raw.DomainAccess.Mode != "list" || len(raw.DomainAccess.ZoneIDs) != 1 || raw.DomainAccess.ZoneIDs[0] != f.zone.ID || len(raw.AllowedZoneIDs) != 1 || raw.AllowedZoneIDs[0] != f.zone.ID {
		t.Fatal("permission seed raw readback differs")
	}
	if before.Revision.Equal(after.Revision) || after.FieldSources["can_send"] != "override" || after.FieldSources["daily_send_quota"] != "override" || after.FieldSources["domain_access"] != "override" {
		t.Fatal("permission seed revision/source readback differs")
	}
}

func r5UICall(t *testing.T, f *r5UIFixture, token, method, path string, body any, status int) []byte {
	t.Helper()
	raw, e := json.Marshal(body)
	r5UIMust(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, f.server.URL+path, bytes.NewReader(raw))
	r5UIMust(t, e)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := f.server.Client().Do(req)
	r5UIMust(t, e)
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	r5UIMust(t, e)
	if res.StatusCode != status {
		t.Fatalf("fixture actual %s %s status=%d expected=%d", method, path, res.StatusCode, status)
	}
	return b
}
func r5UISetup(t *testing.T, f *r5UIFixture, c r5UICase, variant, caseHash string) map[string]any {
	t.Helper()
	ctx := context.Background()
	token := r5UIToken(t, f.admin)
	data := map[string]any{"schema_version": 1, "case_id": c.ID, "variant": variant, "case_sha256": caseHash, "api_url": f.server.URL, "auth": map[string]any{"token": token, "user": f.admin}, "employee_id": f.employee.ID, "employee_email": f.employee.Email, "successor_id": f.successor.ID, "zone_id": f.zone.ID, "input": c.Input}
	switch {
	case c.ID == "RC02":
		payload := company.DraftPayload{To: []string{"visible@replay.test"}, CC: []string{"copy@replay.test"}, BCC: []string{"private@replay.test"}, Subject: "Owned legacy compatibility receipt", TextBody: "PRIVATE_COMPATIBILITY_BODY"}
		data["private_addresses"] = payload.BCC
		data["private_values"] = append(append(append([]string{payload.Subject, payload.TextBody}, payload.To...), payload.CC...), payload.BCC...)
		data["receipt_counts"] = company.OutboundReceiptCounts{Total: 3, Pending: 3}
		data["receipt_state"] = models.OutboundSent
		data["receipt_subject"] = payload.Subject
		employeeToken := r5UIToken(t, f.employee)
		data["auth"] = map[string]any{"token": employeeToken, "user": f.employee}
		if variant == "submit_replay" {
			data["receipt_state"] = models.OutboundPending
			draft, e := f.st.SaveMailDraft(ctx, f.member, company.Draft{MailboxID: f.personal.ID, Payload: payload})
			r5UIMust(t, e)
			raw := r5UICall(t, f, employeeToken, "GET", "/api/v1/company/drafts/"+draft.ID.String(), nil, 200)
			var env struct{ Data json.RawMessage }
			r5UIMust(t, json.Unmarshal(raw, &env))
			data["draft"] = env.Data
			rights, e := f.st.GetWorkMailbox(ctx, f.member, f.personal.ID)
			r5UIMust(t, e)
			data["mailboxes"] = []*company.MailboxAccess{rights}
			var first sync.Once
			f.afterCommitFault = func(request *http.Request, response *httptest.ResponseRecorder) (bool, error) {
				if request.Method != "POST" || !strings.HasSuffix(request.URL.Path, "/drafts/"+draft.ID.String()+"/submit") {
					return false, nil
				}
				aborted := false
				var faultError error
				first.Do(func() {
					if response.Code != 201 {
						faultError = fmt.Errorf("first submit did not really create a job")
						return
					}
					var body struct{ Data struct{ ID uuid.UUID } }
					if e := json.Unmarshal(response.Body.Bytes(), &body); e != nil {
						faultError = e
						return
					}
					var jobs int
					if e := f.pool.QueryRow(request.Context(), `SELECT count(*) FROM outbound_jobs WHERE id=$1 AND draft_id=$2`, body.Data.ID, draft.ID).Scan(&jobs); e != nil || jobs != 1 {
						faultError = fmt.Errorf("real commit not established")
						return
					}
					if _, e := f.pool.Exec(request.Context(), `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, body.Data.ID); e != nil {
						faultError = e
						return
					}
					aborted = true
				})
				return aborted, faultError
			}
		} else {
			j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: payload.To, CC: payload.CC, BCC: payload.BCC, RcptTo: append(append(append([]string{}, payload.To...), payload.CC...), payload.BCC...), Subject: payload.Subject, TextBody: payload.TextBody, State: models.OutboundSent}
			r5UIMust(t, f.st.CreateOutboundJob(ctx, j))
			_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
			r5UIMust(t, e)
			data["submission_id"] = j.ID
		}
	case c.ID == "RC01":
		j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"visible@recipient.ui-fixture.test"}, BCC: []string{"private@recipient.ui-fixture.test"}, RcptTo: []string{"visible@recipient.ui-fixture.test", "private@recipient.ui-fixture.test"}, Subject: "Safe UI receipt", TextBody: "PRIVATE_UI_BODY", State: models.OutboundSent}
		r5UIMust(t, f.st.CreateOutboundJob(ctx, j))
		_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
		r5UIMust(t, e)
		data["submission_id"] = j.ID
		data["private_addresses"] = j.BCC
		data["private_values"] = append(append([]string{j.Subject, j.TextBody}, j.To...), j.BCC...)
		data["receipt_counts"] = company.OutboundReceiptCounts{Total: 2, Pending: 2}
		data["receipt_state"] = j.State
		data["auth"] = map[string]any{"token": r5UIToken(t, f.employee), "user": f.employee}
	case strings.HasPrefix(c.ID, "RC"):
		recipients := r5UIInput[[]struct{ Category, Address, State string }](t, c, "recipients")
		j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, State: r5UIInput[models.OutboundState](t, c, "job_state"), InFlightDomain: r5UIInput[string](t, c, "in_flight_domain"), Subject: r5UIInput[string](t, c, "subject"), TextBody: r5UIInput[string](t, c, "text_body"), SMTPResponse: r5UIInput[string](t, c, "smtp_response")}
		if len(recipients) == 0 || j.State == "" {
			t.Fatal("real ledger/state shared receipt input missing")
		}
		for _, r := range recipients {
			j.RcptTo = append(j.RcptTo, r.Address)
			switch r.Category {
			case "to":
				j.To = append(j.To, r.Address)
			case "cc":
				j.CC = append(j.CC, r.Address)
			case "bcc":
				j.BCC = append(j.BCC, r.Address)
			default:
				t.Fatal("unknown structured recipient category")
			}
		}
		tokenText := r5UIInput[string](t, c, "delivery_token")
		if tokenText != "" {
			v, e := uuid.Parse(tokenText)
			r5UIMust(t, e)
			j.DeliveryToken = &v
		}
		r5UIMust(t, f.st.CreateOutboundJob(ctx, j))
		for _, r := range recipients {
			_, e := f.pool.Exec(ctx, `UPDATE outbound_recipients SET state=$3 WHERE job_id=$1 AND address=$2`, j.ID, r.Address, r.State)
			r5UIMust(t, e)
		}
		if r5UIInput[bool](t, c, "content_allowed") {
			t.Fatal("redacted shared receipt setup cannot widen content")
		}
		_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
		r5UIMust(t, e)
		data["submission_id"] = j.ID
		data["private_addresses"] = j.BCC
		data["auth"] = map[string]any{"token": r5UIToken(t, f.employee), "user": f.employee}
	case strings.HasPrefix(c.ID, "LF"):
		if c.ID == "LF01" {
			r5UICall(t, f, token, "PATCH", "/api/v1/admin/users/"+f.employee.ID.String(), map[string]bool{"is_active": false}, 200)
		}
		if c.ID == "LF03" {
			p, e := f.st.PreviewOffboarding(ctx, f.actor, f.employee.ID, f.successor.ID, company.OffboardingOptions{Drafts: "seal"}, "Actual second-plan component control")
			r5UIMust(t, e)
			data["control_plan_id"] = p.ID
		}
		if c.ID == "LF04" {
			d, e := f.st.SaveMailDraft(ctx, f.member, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "before preview"}})
			r5UIMust(t, e)
			data["draft"] = d
			data["external_employee_token"] = r5UIToken(t, f.employee)
		}
		if c.ID == "LF05" && variant == "higher_role" {
			_, e := f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.employee.ID)
			r5UIMust(t, e)
		}
		if c.ID == "LF05" && variant == "foreign_tenant" {
			tenant := &models.Tenant{Name: "UI foreign successor", PlanID: f.tenant.PlanID}
			r5UIMust(t, f.st.CreateTenant(ctx, tenant))
			u := &models.User{TenantID: tenant.ID, Email: "foreign@foreign.ui-fixture.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
			r5UIMust(t, f.st.CreateUser(ctx, u))
			data["foreign_user_id"] = u.ID
		}
		if c.ID == "LF07" {
			_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT ui_required_audit_failure CHECK(action<>'employee.offboard')`)
			r5UIMust(t, e)
		}
	case c.ID == "PE03":
		raw := r5UICall(t, f, token, "POST", "/api/v1/admin/permissions", map[string]any{"name": "Shared UI stale profile", "can_send": true}, 201)
		var env struct{ Data models.PermissionProfile }
		r5UIMust(t, json.Unmarshal(raw, &env))
		data["profile_id"] = env.Data.ID
		data["profile_name"] = env.Data.Name
	case c.ID == "PE05":
		mb, e := f.st.GetWorkMailbox(ctx, f.actor, f.shared.ID)
		r5UIMust(t, e)
		r5UIMust(t, f.st.SetWorkGrant(ctx, f.actor, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}, mb.Revision))
		d, e := f.st.SaveMailDraft(ctx, f.member, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "before authorization wait"}})
		r5UIMust(t, e)
		rights, e := f.st.GetWorkMailbox(ctx, f.member, f.shared.ID)
		r5UIMust(t, e)
		wire := r5UICall(t, f, r5UIToken(t, f.employee), "GET", "/api/v1/company/drafts/"+d.ID.String(), nil, 200)
		var envelope struct{ Data json.RawMessage }
		r5UIMust(t, json.Unmarshal(wire, &envelope))
		// Initial props come from the actual HTTP DTO, including production
		// required-list normalization, not a raw store model bypassing wire.
		data["draft"] = envelope.Data
		data["mailboxes"] = []*company.MailboxAccess{rights}
		data["auth"] = map[string]any{"token": r5UIToken(t, f.employee), "user": f.employee}
		r5UIGrantBarrier(t, f, d)
	default:
		r5UISeedPermissionOverrides(t, f, token)
	}
	return data
}

// The component's real PUT must reach the real draft row wait. Only then does
// the administrator command race it; pg_blocking_pids, not sleeps, orders them.
func r5UIGrantBarrier(t *testing.T, f *r5UIFixture, d *company.Draft) r5UIBarrierOwner {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	hold, e := f.pool.Begin(ctx)
	if e != nil {
		cancel()
		r5UIMust(t, e)
	}
	pid := int32(hold.Conn().PgConn().PID())
	ready := make(chan error, 1)
	var txMu sync.Mutex
	owner := r5UINewGrantOwner(ctx, cancel, func() error {
		txMu.Lock()
		defer txMu.Unlock()
		return hold.Rollback(context.Background())
	}, func(o *r5UIGrantOwner) error {
		txMu.Lock()
		_, e := hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, d.ID)
		txMu.Unlock()
		if e != nil {
			ready <- e
			return e
		}
		mb, e := f.st.GetWorkMailbox(ctx, f.actor, f.shared.ID)
		ready <- e
		if e != nil {
			return e
		}
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var writer int32
		for {
			e := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%UPDATE mail_drafts%' LIMIT 1`, pid).Scan(&writer)
			if e == nil {
				break
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("actual component draft lock wait not observed: %w", ctx.Err())
			case <-ticker.C:
			}
		}
		o.startRevoker(func(ctx context.Context) error {
			return f.st.SetWorkGrant(ctx, f.actor, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}, mb.Revision)
		})
		// Peek completion without consuming it: the owner always joins and retains
		// the child result. This also handles revocation completing before polling.
		for {
			select {
			case <-o.revoked:
				goto released
			default:
			}
			var blocked bool
			if e := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)))`, writer).Scan(&blocked); e != nil {
				return e
			}
			if blocked {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("actual component authority order not observed: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	released:
		if e := o.releaseHeld(); e != nil {
			return e
		}
		// Successful ordering must allow the revoker to finish before cancellation.
		// The owner retains its actual result after the final join.
		select {
		case <-o.revoked:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("revocation did not finish after actual writer release: %w", ctx.Err())
		}
	})
	t.Cleanup(func() {
		if e := owner.ownerClose(); e != nil {
			t.Errorf("PE05 barrier owner cleanup failed: %T", e)
		}
	})
	r5UIMust(t, <-ready)
	return owner
}

func r5UIState(t *testing.T, f *r5UIFixture) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var active bool
	var version, audits int64
	r5UIMust(t, f.pool.QueryRow(ctx, `SELECT is_active,session_version FROM users WHERE id=$1`, f.employee.ID).Scan(&active, &version))
	r5UIMust(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='employee.offboard' AND resource_id=$1`, f.employee.ID).Scan(&audits))
	perm, e := f.st.EffectivePermission(ctx, f.employee.ID)
	r5UIMust(t, e)
	return map[string]any{"employee_active": active, "employee_session_version": version, "disposition_audits": audits, "effective_can_send": perm.CanSend, "effective_zone_count": len(perm.AllowedZoneIDs)}
}

// Both producers resolve the same externally pinned CLI. The helper performs
// full descriptor-bound inventory checks before launch and after termination.
// This workspace contract does not prevent hostile transient concurrent writes.
func r5UIExternalRuntime(t *testing.T, root string) (string, string, string, int) {
	t.Helper()
	path, pin := os.Getenv("TABMAIL_R5_EXTERNAL_MANIFEST"), os.Getenv("TABMAIL_R5_EXTERNAL_MANIFEST_SHA256")
	if path == "" || pin == "" || !filepath.IsAbs(path) {
		t.Fatal("explicit external runtime manifest and independent SHA256 required")
	}
	raw, err := os.ReadFile(path)
	r5UIMust(t, err)
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != pin {
		t.Fatal("external runtime manifest pin differs")
	}
	selectedVersion := 2
	if value, present := os.LookupEnv("TABMAIL_R5_SELECTED_BINDING_VERSION"); present {
		switch value {
		case "2":
		case "3":
			selectedVersion = 3
		default:
			t.Fatal("external runtime selector must be integer 2 or 3")
		}
	}
	type tool struct {
		Path   string
		SHA256 string
	}
	var manifest struct {
		Policy, Status    string
		Source            struct{ Path string }
		Node, CLI, Python tool
		HelperSHA256      string          `json:"helper_sha256"`
		SchemaVersion     json.RawMessage `json:"schema_version"`
		SelectedVersion   json.RawMessage `json:"selected_binding_version"`
		AdmittedSelection json.RawMessage `json:"admitted_selection"`
	}
	r5UIMust(t, json.Unmarshal(raw, &manifest))
	if string(manifest.SchemaVersion) != fmt.Sprint(selectedVersion) || manifest.Policy != fmt.Sprintf("r5_external_dependency_runtime_v%d", selectedVersion) || manifest.Status != "UNADOPTED" || manifest.Source.Path != root {
		t.Fatal("external runtime policy, status or source differs")
	}
	if selectedVersion == 2 && (len(manifest.SelectedVersion) != 0 || len(manifest.AdmittedSelection) != 0) || selectedVersion == 3 && string(manifest.SelectedVersion) != "3" {
		t.Fatal("external runtime selected version differs")
	}
	helper := filepath.Join(root, "scripts/preparation/r5_external_runtime.py")
	for _, file := range []tool{manifest.Node, manifest.CLI, manifest.Python, {Path: helper, SHA256: manifest.HelperSHA256}} {
		if !filepath.IsAbs(file.Path) {
			t.Fatal("absolute pinned executable required")
		}
		info, e := os.Lstat(file.Path)
		r5UIMust(t, e)
		if !info.Mode().IsRegular() {
			t.Fatal("regular pinned executable required")
		}
		bytes, e := os.ReadFile(file.Path)
		r5UIMust(t, e)
		digest := sha256.Sum256(bytes)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			t.Fatal("external runtime executable drift")
		}
	}
	cmd := exec.Command(manifest.Python.Path, helper, "validate", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion))
	cmd.Dir = filepath.Join(root, "web")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external runtime validation rejected: %v: %s", err, output)
	}
	return manifest.Python.Path, manifest.Node.Path, manifest.CLI.Path, selectedVersion
}

func r5UIExternalVitestCommand(ctx context.Context, root, launcher, node, cli, config, report string, selectedVersion int) *exec.Cmd {
	cmd := exec.CommandContext(ctx, launcher, filepath.Join(root, "scripts/preparation/r5_external_runtime.py"), "launch", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion), "--", node, cli, "run", "--cache=false", "--experimental.fsModuleCache=false", "--config", config, "--reporter=json", "--outputFile", report)
	cmd.Dir = filepath.Join(root, "web")
	return cmd
}

func TestR5ProtocolComponentObservations(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" || os.Getenv("TABMAIL_R5_PROTOCOL_COMPONENT_EVIDENCE") == "" {
		t.Fatal("Explicit disposable DSN and fresh component evidence root required; no skip")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	launcher, node, cli, selectedVersion := r5UIExternalRuntime(t, root)
	raw, e := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, e)
	hash := sha256.Sum256(raw)
	caseHash := hex.EncodeToString(hash[:])
	var manifest struct{ Cases []r5UICase }
	r5UIMust(t, json.Unmarshal(raw, &manifest))
	wanted := map[string]bool{"RC01": true, "RC02": true, "RC03": true, "RC04": true, "RC05": true, "LF01": true, "LF02": true, "LF03": true, "LF04": true, "LF05": true, "LF06": true, "LF07": true, "PE01": true, "PE02": true, "PE03": true, "PE04": true, "PE05": true}
	executed := 0
	for _, c := range manifest.Cases {
		if !wanted[c.ID] {
			continue
		}
		executed++
		variants := []string{"default"}
		if c.ID == "RC02" {
			variants = r5UIInput[[]string](t, c, "operation")
		}
		if c.ID == "PE02" {
			variants = []string{"omitted", "null", "false", "0", "[]"}
		}
		if c.ID == "LF05" {
			variants = r5UIInput[[]string](t, c, "successor_state")
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				r5UIObserveCase(t, root, c, variant, caseHash, func(ctx context.Context, private, report string) ([]byte, error) {
					cmd := r5UIExternalVitestCommand(ctx, root, launcher, node, cli, "vitest.r5protocol.config.ts", report, selectedVersion)
					cmd.Env = append(os.Environ(), "TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE="+private)
					return cmd.CombinedOutput()
				})
			})
		}
	}
	if executed != 17 {
		t.Fatalf("shared component case coverage drift: %d", executed)
	}
}

// Shared assertions remain unchanged; the new batch entry supplies only its owned executor.
func r5UIObserveCase(t *testing.T, root string, c r5UICase, variant, caseHash string, execute func(context.Context, string, string) ([]byte, error)) {
	f := r5UISeed(t)
	fixture := r5UISetup(t, f, c, variant, caseHash)
	out := filepath.Join(os.Getenv("TABMAIL_R5_PROTOCOL_COMPONENT_EVIDENCE"), c.ID, strings.ReplaceAll(variant, "[]", "empty_array"))
	r5UIMust(t, os.MkdirAll(out, 0700))
	reportPath := filepath.Join(out, "vitest.json")
	if _, e := os.Stat(reportPath); !os.IsNotExist(e) {
		t.Fatal("fresh component evidence required")
	}
	private := filepath.Join(t.TempDir(), "private-fixture.json")
	b, e := json.Marshal(fixture)
	r5UIMust(t, e)
	r5UIMust(t, os.WriteFile(private, b, 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	f.mu.Lock()
	f.trace = nil
	f.mu.Unlock()
	logs, runErr := execute(ctx, private, reportPath)
	r5UIMust(t, os.WriteFile(filepath.Join(out, "vitest.log"), logs, 0600))
	if ctx.Err() != nil {
		t.Fatal("component process timed out; never target red")
	}
	var result struct {
		NumTotalTests, NumFailedTests, NumPendingTests, NumRuntimeErrorTestSuites int
		TestResults                                                               []struct {
			AssertionResults []struct {
				FullName, Status string
				FailureMessages  []string
			}
		}
	}
	b, e = os.ReadFile(reportPath)
	r5UIMust(t, e)
	r5UIMust(t, json.Unmarshal(b, &result))
	expectedName := "R5 protocol component " + c.ID + " " + variant + " secure behavior"
	if result.NumTotalTests != 1 || result.NumPendingTests != 0 || result.NumRuntimeErrorTestSuites != 0 || len(result.TestResults) != 1 || len(result.TestResults[0].AssertionResults) != 1 {
		t.Fatal("missing/setup/skipped component execution")
	}
	assertion := result.TestResults[0].AssertionResults[0]
	if assertion.FullName != expectedName {
		t.Fatal("unexpected real component test identity")
	}
	f.mu.Lock()
	trace := append([]r5UITrace(nil), f.trace...)
	f.mu.Unlock()
	childExit := 0
	if runErr != nil {
		var childError interface{ ExitCode() int }
		if errors.As(runErr, &childError) {
			childExit = childError.ExitCode()
		} else {
			childExit = -1
		}
	}
	observations := map[string]any{"schema_version": 1, "case_id": c.ID, "variant": variant, "case_sha256": caseHash, "component_process_exit_code": childExit, "scope": "actual shipping component/API client/fetch/HTTP/PostgreSQL; host auth context only is supplied", "trace": trace, "state": r5UIState(t, f)}
	b, e = json.MarshalIndent(observations, "", "  ")
	r5UIMust(t, e)
	r5UIMust(t, os.WriteFile(filepath.Join(out, "observations.json"), b, 0600))
	if assertion.Status == "passed" && runErr == nil && result.NumFailedTests == 0 && len(trace) > 0 {
		if c.ID == "RC02" && variant == "submit_replay" {
			submits := []r5UITrace{}
			for _, v := range trace {
				if v.Method == "POST" && strings.HasSuffix(v.Path, "/submit") {
					submits = append(submits, v)
				}
			}
			var jobs, consumed int
			r5UIMust(t, f.pool.QueryRow(context.Background(), `SELECT count(*),count(DISTINCT draft_id) FROM outbound_jobs WHERE tenant_id=$1`, f.tenant.ID).Scan(&jobs, &consumed))
			if len(submits) != 2 || !submits[0].TransportAborted || submits[0].Status != 201 || submits[1].Status != 200 || submits[1].TransportAborted || submits[0].IdempotencySHA == "" || submits[0].IdempotencySHA != submits[1].IdempotencySHA || jobs != 1 || consumed != 1 {
				t.Fatal("actual lost-response replay did not preserve command identity/once effect")
			}
		}
		if c.ID == "LF02" {
			executes := []r5UITrace{}
			for _, v := range trace {
				if v.Method == "POST" && strings.HasSuffix(v.Path, "/offboard") {
					executes = append(executes, v)
				}
			}
			state := observations["state"].(map[string]any)
			if len(executes) != 2 || executes[0].Status != 200 || executes[1].Status != 200 || executes[0].ResponseSHA256 != executes[1].ResponseSHA256 || state["disposition_audits"] != int64(1) || state["employee_session_version"] != f.employee.SessionVersion+1 {
				t.Fatal("actual same-plan replay changed receipt/effects")
			}
		}
		return
	}
	allowed := map[string][]string{
		"RC01": {"R5_PROTOCOL_UI_TARGET_RC01_BCC"}, "RC02": {"R5_PROTOCOL_UI_TARGET_RC02_LEGACY_BYPASS"}, "RC03": {"R5_PROTOCOL_UI_TARGET_RC03_BCC"}, "LF01": {"R5_PROTOCOL_UI_TARGET_LF01_FROZEN"}, "LF06": {"R5_PROTOCOL_UI_TARGET_LF06_LIFECYCLE"},
		"PE01": {"R5_PROTOCOL_UI_TARGET_PE01_OMITTED"}, "PE02": {"R5_PROTOCOL_UI_TARGET_PE02_NULL", "R5_PROTOCOL_UI_TARGET_PE02_OMITTED"}, "PE03": {"R5_PROTOCOL_UI_TARGET_PE03_STALE"}, "PE04": {"R5_PROTOCOL_UI_TARGET_PE04_ABA"},
	}
	failure := strings.Join(assertion.FailureMessages, "\n")
	matches := regexp.MustCompile(`(?m)^Error: (R5_PROTOCOL_UI_TARGET_[A-Z0-9_]+): `).FindAllStringSubmatch(failure, -1)
	marker := ""
	if len(matches) == 1 {
		for _, candidate := range allowed[c.ID] {
			if matches[0][1] == candidate {
				marker = candidate
			}
		}
	}
	var exit interface{ ExitCode() int }
	if assertion.Status != "failed" || result.NumFailedTests != 1 || !errors.As(runErr, &exit) || exit.ExitCode() != 1 || marker == "" || len(trace) == 0 {
		t.Fatalf("unexpected component failure (not target); inspect isolated %s", out)
	}
	t.Errorf("%s: actual component and Go-owned HTTP/PG evidence at %s", marker, out)
}
