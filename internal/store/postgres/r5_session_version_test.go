package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func r5SessionRequest(ctx context.Context, h http.Handler, method, path, token string, body []byte) (<-chan error, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { h.ServeHTTP(rr, req); done <- nil }()
	return done, rr
}

func r5SessionDraftCounts(t *testing.T, f *companyFixture) [2]int {
	t.Helper()
	var v [2]int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM mail_drafts),(SELECT count(*) FROM draft_creation_receipts)`).Scan(&v[0], &v[1]))
	return v
}

// A real Router request passes middleware's plain MVCC user read and then
// waits on the transaction's user SHARE. Version changes commit before that
// final authorization read resumes. No test middleware or time-based sleep.
func TestR5SessionVersionInFlightRouterAuthorization(t *testing.T) {
	for _, route := range []string{"receipt", "draft-save"} {
		for _, transition := range []string{"password-change", "session-revoke", "freeze-reactivate", "unchanged"} {
			t.Run(route+"/"+transition, func(t *testing.T) {
				f := seedCompany(t)
				j := r5LegacyJob(t, f)
				svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
				h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
				token := r3Token(t, f.employee) // Explicitly exercises sv=0 too.
				method, path := http.MethodGet, "/api/v1/outbound/"+j.ID.String()
				var body []byte
				if route == "draft-save" {
					method, path = http.MethodPost, "/api/v1/company/drafts"
					var e error
					body, e = json.Marshal(company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "session boundary synthetic"}})
					must(t, e)
				}
				before := r5SessionDraftCounts(t, f)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				hold, e := f.pool.Begin(ctx)
				must(t, e)
				defer hold.Rollback(context.Background())
				var passwordDone chan error
				blocker := hold.Conn().PgConn().PID()
				if transition == "password-change" {
					// The production password command holds the user update while
					// this table barrier prevents its mandatory audit from committing.
					_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
					must(t, e)
					passwordDone = make(chan error, 1)
					go func() {
						passwordDone <- f.st.ChangePasswordAtomic(ctx, f.employee.ID, f.employee.PasswordHash, "new-test-only-hash")
					}()
					blocker = r5WaitBlockedBy(t, f, ctx, blocker, "audit_log")
				} else {
					_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
					must(t, e)
				}
				done, rr := r5SessionRequest(ctx, h, method, path, token, body)
				r5WaitBlockedBy(t, f, ctx, blocker, "FOR SHARE")
				if transition == "session-revoke" {
					_, e = hold.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.employee.ID)
					must(t, e)
				} else if transition == "freeze-reactivate" {
					// SQL arranges a deterministic ABA fixture while retaining the
					// actual freeze/reactivate version increments. The Router and
					// final guarded command are unmodified production entry points.
					_, e = hold.Exec(ctx, `UPDATE users SET is_active=false,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
					must(t, e)
					_, e = hold.Exec(ctx, `UPDATE users SET is_active=true,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
					must(t, e)
				}
				must(t, hold.Commit(ctx))
				if passwordDone != nil {
					r5AwaitOperation(t, ctx, passwordDone)
				}
				r5AwaitOperation(t, ctx, done)
				if transition == "unchanged" {
					if rr.Code != http.StatusOK {
						t.Fatalf("unchanged in-flight JWT lost access: %d %s", rr.Code, rr.Body.String())
					}
					return
				}
				// Receipt access deliberately hides existence with 404; draft
				// writes expose an explicit 403 authority rejection. The initial
				// candidate expectation of 403 for both was a test-design error,
				// not a reason to change the established production mapping.
				expectedDenial := http.StatusForbidden
				if route == "receipt" {
					expectedDenial = http.StatusNotFound
				}
				if rr.Code != expectedDenial {
					t.Fatalf("revoked in-flight JWT must fail at transaction boundary: status=%d body=%s", rr.Code, rr.Body.String())
				}
				if after := r5SessionDraftCounts(t, f); after != before {
					t.Fatalf("revoked request wrote draft/creation receipt: before=%v after=%v", before, after)
				}
				current, e := f.st.GetUser(ctx, f.employee.ID)
				must(t, e)
				if !current.IsActive || current.SessionVersion <= f.employee.SessionVersion {
					t.Fatal("fixture did not retain an active user with advanced session version")
				}
				// Old JWT rejected at middleware; current JWT succeeds through
				// the identical endpoint. Neither deletion nor active=false can
				// explain the preceding in-flight denial.
				r3HTTP(t, h, token, method, path, json.RawMessage(body), http.StatusUnauthorized)
				r3HTTP(t, h, r3Token(t, current), method, path, json.RawMessage(body), http.StatusOK)
			})
		}
	}
}
