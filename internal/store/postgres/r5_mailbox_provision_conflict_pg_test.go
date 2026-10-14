package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/authn"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// This fixed regression is overlaid unchanged on the pre-fix source in the
// owned PostgreSQL CI job. Setup errors, missing DSN and skips cannot stand in
// for observing a real address collision through the shipping HTTP handler.
func TestR5MailboxProvisionConflictPostgres(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required")
	}
	f := seedCompany(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `
		CREATE FUNCTION finish_reject_unrelated_mailbox() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		  IF NEW.local_part = 'unrelated-unique' THEN
		    RAISE EXCEPTION 'private unrelated storage diagnostic' USING ERRCODE='23505', CONSTRAINT='finish_unrelated_unique';
		  END IF;
		  RETURN NEW;
		END $$;
		CREATE TRIGGER finish_reject_unrelated_mailbox BEFORE INSERT ON mailboxes
		FOR EACH ROW EXECUTE FUNCTION finish_reject_unrelated_mailbox();
		CREATE FUNCTION finish_reject_mailbox_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		  IF NEW.action = 'mailbox.provision' AND NEW.details->>'local_part' = 'audit-failure' THEN
		    RAISE EXCEPTION 'private mailbox audit diagnostic' USING ERRCODE='23514';
		  END IF;
		  IF NEW.action = 'mailbox.provision' AND NEW.details->>'local_part' = 'audit-unique-failure' THEN
		    RAISE EXCEPTION 'private audit uniqueness diagnostic' USING ERRCODE='23505', CONSTRAINT='mailboxes_full_address_key';
		  END IF;
		  RETURN NEW;
		END $$;
		CREATE TRIGGER finish_reject_mailbox_audit BEFORE INSERT ON audit_log
		FOR EACH ROW EXECUTE FUNCTION finish_reject_mailbox_audit();`)
	if err != nil {
		t.Fatalf("owned fault fixture: %v", err)
	}
	const secret = "finish-mailbox-owned-test-secret"
	state := middleware.NewAuthState(nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := state.StopContext(ctx); err != nil {
			t.Errorf("owned auth cleanup: %v", err)
		}
	})
	h := handlers.NewMailboxAdminHandler(f.st, zerolog.Nop())
	endpoint := middleware.Auth(f.st, secret, f.tenant.ID.String(), state)(middleware.RequireAuth(middleware.RequireAdmin(http.HandlerFunc(h.CreateMailbox))))
	snapshot := func() [3]int {
		t.Helper()
		var counts [3]int
		for i, query := range []string{
			`SELECT count(*) FROM mailboxes`,
			`SELECT count(*) FROM audit_log WHERE action='mailbox.provision'`,
			`SELECT count(*) FROM outbox_events`,
		} {
			if err := f.pool.QueryRow(ctx, query).Scan(&counts[i]); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
		}
		return counts
	}
	for _, tc := range []struct {
		name, local string
		user        *models.User
		status      int
	}{
		{"shared_duplicate", "support", f.admin, 409},
		{"normalized_duplicate", " SUPPORT ", f.admin, 409},
		{"personal_duplicate", "employee", f.admin, 409},
		{"new_address", "finish-unique", f.admin, 200},
		{"invalid_address", "bad@address", f.admin, 400},
		{"employee_denied", "employee-attempt", f.employee, 403},
		{"unknown_unique_error", "unrelated-unique", f.admin, 500},
		{"audit_rollback", "audit-failure", f.admin, 500},
		{"audit_unique_failure", "audit-unique-failure", f.admin, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshot()
			body, err := json.Marshal(map[string]any{"local_part": tc.local, "kind": "shared"})
			if err != nil {
				t.Fatal(err)
			}
			token, err := authn.IssueAccessToken(secret, tc.user)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/company/mailboxes", strings.NewReader(string(body)))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, r)
			if w.Code != tc.status {
				if tc.status == 409 {
					t.Errorf("address conflict must be HTTP409; actual=%d", w.Code)
				} else {
					if tc.name == "unknown_unique_error" || tc.name == "audit_unique_failure" {
						t.Errorf("unrelated storage failure must be HTTP500; actual=%d", w.Code)
					}
					t.Errorf("HTTP status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
				}
			}
			if tc.status == 409 && w.Code == 409 && !strings.Contains(w.Body.String(), `"code":"CONFLICT"`) {
				t.Errorf("wrong conflict envelope: %s", w.Body.String())
			}
			for _, private := range []string{"private", "23505", "23514", "mailboxes_full_address_key", "finish_unrelated_unique"} {
				if strings.Contains(w.Body.String(), private) {
					t.Errorf("storage detail leaked: %s", private)
				}
			}
			after := snapshot()
			want := before
			if tc.status == 200 {
				for i := range want {
					want[i]++
				}
			}
			if after != want {
				t.Errorf("mailbox/audit/outbox atomicity: before=%v after=%v want=%v", before, after, want)
			}
		})
	}
	t.Run("real_cause_preserved", func(t *testing.T) {
		before := snapshot()
		_, err := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "support", Kind: "shared"})
		var storageError *pgconn.PgError
		if !errors.As(err, &storageError) || storageError.Code != "23505" || storageError.ConstraintName != "mailboxes_full_address_key" {
			t.Errorf("actual address conflict lost original PostgreSQL cause: %v", err)
		}
		if domainError, ok := app.As(err); !ok || domainError.Kind != app.KindConflict {
			t.Errorf("actual duplicate did not retain typed conflict: %v", err)
		}
		if after := snapshot(); after != before {
			t.Errorf("cause inspection changed mailbox/audit/outbox state: before=%v after=%v", before, after)
		}
	})
}
