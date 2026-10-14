package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/handlers"
	"tabmail/internal/app"
	"tabmail/internal/app/credentials"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type r5ActivationErrorsFixture struct {
	f      *companyFixture
	invite *company.Invitation
	raw    string
	hash   string
}

func r5ActivationErrorsSeed(t *testing.T) *r5ActivationErrorsFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN is required for activation error regressions")
	}
	f := seedCompany(t)
	raw, hash, err := credentials.IssueInvitation()
	must(t, err)
	invite, err := f.st.InviteEmployee(t.Context(), f.a, company.InvitationInput{Email: "activation-error@fixture.test", LocalPart: "activation-error", DisplayName: "Activation employee"}, hash)
	must(t, err)
	return &r5ActivationErrorsFixture{f: f, invite: invite, raw: raw, hash: hash}
}

func r5ActivationErrorsState(t *testing.T, s *r5ActivationErrorsFixture) string {
	t.Helper()
	var state string
	must(t, s.f.pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'invitation',(SELECT to_jsonb(i) FROM employee_invitations i WHERE id=$1),
 'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE lower(email)=lower($2)),
 'mailboxes',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM mailboxes m WHERE full_address=$3),
 'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM permission_profiles p WHERE tenant_id=$4),
 'audit',(SELECT count(*) FROM audit_log),
 'outbox',(SELECT count(*) FROM outbox_events))::text`, s.invite.ID, s.invite.Email, s.invite.Address, s.f.tenant.ID).Scan(&state))
	return state
}

func r5ActivationErrorsHTTP(t *testing.T, s *r5ActivationErrorsFixture, status int, message string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": s.raw, "password": "activation-regression-password"})
	must(t, err)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/company/activate", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handlers.NewCompanySetupHandler(s.f.st, zerolog.Nop()).Activate(w, r)
	if w.Code != status {
		t.Errorf("activation HTTP status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	if message != "" && !strings.Contains(w.Body.String(), message) {
		t.Errorf("activation HTTP message missing %q: %s", message, w.Body.String())
	}
	if status == http.StatusConflict && !strings.Contains(w.Body.String(), `"code":"CONFLICT"`) {
		t.Errorf("activation conflict envelope missing: %s", w.Body.String())
	}
	if status == http.StatusInternalServerError && !strings.Contains(w.Body.String(), "internal server error") {
		t.Errorf("activation server error was not sanitized: %s", w.Body.String())
	}
	for _, private := range []string{"private activation", "23505", "23503", "23514", "40001", "55P03", "40P01", "idx_users_email", "mailboxes_full_address_key", "r5_activation_check", "users_pkey", "mailboxes_pkey", "users_permission_profile_id_fkey", "mailboxes_zone_id_fkey"} {
		if strings.Contains(w.Body.String(), private) {
			t.Errorf("activation HTTP leaked storage diagnostic %q", private)
		}
	}
}

func r5ActivationErrorsCause(t *testing.T, err error, code, constraint string, kind app.ErrorKind, duplicateMessage string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code || pg.ConstraintName != constraint {
		t.Errorf("activation lost original PostgreSQL cause: code=%s constraint=%s actual=%v", code, constraint, err)
	}
	ae, ok := app.As(app.FromAuthz(err))
	if !ok || ae.Kind != kind {
		t.Errorf("activation error kind=%s expected, actual=%v", kind, err)
	}
	if duplicateMessage != "" {
		if !ok || ae.Message != duplicateMessage {
			t.Errorf("activation address conflict message=%q expected, actual=%v", duplicateMessage, err)
		}
	} else if ok && strings.Contains(ae.Message, "already exists") {
		t.Errorf("unrelated activation error mislabeled as an address duplicate: %v", err)
	}
}

// CHECK, FK and primary-key failures below are enforced by real constraints.
// The diagnostic modes deliberately raise server SQLSTATEs to exercise the
// application boundary. In particular, deadlock_diagnostic is not evidence of
// an engine-detected lock cycle; actual cancellation waits are tested below.
func r5ActivationErrorsFault(t *testing.T, s *r5ActivationErrorsFixture, stage, mode string) (code, constraint string, kind app.ErrorKind) {
	t.Helper()
	var predicate, foreignKey, existingID string
	switch stage {
	case "users":
		predicate = "NEW.email='activation-error@fixture.test'"
		foreignKey = "permission_profile_id"
		existingID = s.f.admin.ID.String()
	case "mailboxes":
		predicate = "NEW.full_address='activation-error@company.test'"
		foreignKey = "zone_id"
		existingID = s.f.shared.ID.String()
	default:
		t.Fatal("invalid activation fault stage")
	}
	kind = app.KindInternal
	if mode == "check" {
		column, value := "email", "activation-error@fixture.test"
		if stage == "mailboxes" {
			column, value = "full_address", "activation-error@company.test"
		}
		_, err := s.f.pool.Exec(t.Context(), fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT r5_activation_check CHECK(%s<>'%s') NOT VALID`, stage, column, value))
		must(t, err)
		return "23514", "r5_activation_check", kind
	}
	var operation string
	switch mode {
	case "foreign_key":
		operation = fmt.Sprintf("NEW.%s := '00000000-0000-0000-0000-000000000249'::uuid;", foreignKey)
		code, constraint = "23503", stage+"_"+foreignKey+"_fkey"
	case "primary_key":
		operation = fmt.Sprintf("NEW.id := '%s'::uuid;", existingID)
		code, constraint = "23505", stage+"_pkey"
	case "other_stage_unique":
		code, constraint = "23505", "idx_users_email"
		if stage == "users" {
			constraint = "mailboxes_full_address_key"
		}
	case "serialization_diagnostic":
		code, kind = "40001", app.KindConflict
	case "lock_diagnostic":
		code, kind = "55P03", app.KindConflict
	case "deadlock_diagnostic":
		code = "40P01"
	default:
		t.Fatal("invalid activation fault mode")
	}
	if operation == "" {
		operation = fmt.Sprintf("RAISE EXCEPTION 'private activation diagnostic' USING ERRCODE='%s', CONSTRAINT='%s';", code, constraint)
	}
	// Identifiers and SQL literals are fixed test modes, never request input.
	_, err := s.f.pool.Exec(t.Context(), fmt.Sprintf(`CREATE FUNCTION r5_activation_insert_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN %s END IF; RETURN NEW; END $$;
 CREATE TRIGGER r5_activation_insert_fault BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION r5_activation_insert_fault()`, predicate, operation, stage))
	must(t, err)
	return code, constraint, kind
}

func TestR5ActivationErrorsPostgresInsertFailures(t *testing.T) {
	for _, stage := range []string{"users", "mailboxes"} {
		t.Run(stage, func(t *testing.T) {
			for _, mode := range []string{"check", "foreign_key", "primary_key", "other_stage_unique", "serialization_diagnostic", "lock_diagnostic", "deadlock_diagnostic"} {
				t.Run(mode, func(t *testing.T) {
					s := r5ActivationErrorsSeed(t)
					code, constraint, kind := r5ActivationErrorsFault(t, s, stage, mode)
					before := r5ActivationErrorsState(t, s)
					err := s.f.st.ActivateEmployee(t.Context(), s.hash, "synthetic-activation-hash")
					r5ActivationErrorsCause(t, err, code, constraint, kind, "")
					if after := r5ActivationErrorsState(t, s); after != before {
						t.Error("failed activation left employee/mailbox/profile/consumption/audit/outbox effects")
					}
					status := http.StatusInternalServerError
					if kind == app.KindConflict {
						status = http.StatusConflict
					}
					r5ActivationErrorsHTTP(t, s, status, "")
					if after := r5ActivationErrorsState(t, s); after != before {
						t.Error("failed HTTP activation left transactional effects")
					}
				})
			}
		})
	}
}

func TestR5ActivationErrorsPostgresRelatedUniqueConflicts(t *testing.T) {
	for _, stage := range []string{"users", "mailboxes"} {
		t.Run(stage, func(t *testing.T) {
			s := r5ActivationErrorsSeed(t)
			constraint, message := "idx_users_email", "employee email already exists"
			if stage == "users" {
				duplicate := &models.User{TenantID: s.f.tenant.ID, Email: strings.ToUpper(s.invite.Email), DisplayName: "Existing employee", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic-existing-hash"}
				must(t, s.f.st.CreateUser(t.Context(), duplicate))
			} else {
				_, err := s.f.st.CreateWorkMailbox(t.Context(), s.f.a, company.MailboxInput{LocalPart: "activation-error", Kind: "shared"})
				must(t, err)
				constraint, message = "mailboxes_full_address_key", "employee mailbox already exists"
			}
			before := r5ActivationErrorsState(t, s)
			err := s.f.st.ActivateEmployee(t.Context(), s.hash, "synthetic-activation-hash")
			r5ActivationErrorsCause(t, err, "23505", constraint, app.KindConflict, message)
			if after := r5ActivationErrorsState(t, s); after != before {
				t.Error("real duplicate activation left transactional effects")
			}
			r5ActivationErrorsHTTP(t, s, http.StatusConflict, message)
			if after := r5ActivationErrorsState(t, s); after != before {
				t.Error("real duplicate HTTP activation left transactional effects")
			}
		})
	}
}

func TestR5ActivationErrorsPostgresCancellationDuringInsertWait(t *testing.T) {
	for _, stage := range []string{"users", "mailboxes"} {
		t.Run(stage, func(t *testing.T) {
			s := r5ActivationErrorsSeed(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			before := r5ActivationErrorsState(t, s)
			hold, err := s.f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, "LOCK TABLE "+stage+" IN SHARE MODE")
			must(t, err)
			activationCtx, stop := context.WithCancel(ctx)
			defer stop()
			done := make(chan error, 1)
			go func() { done <- s.f.st.ActivateEmployee(activationCtx, s.hash, "synthetic-activation-hash") }()
			r5WaitBlockedBy(t, s.f, ctx, hold.Conn().PgConn().PID(), "INSERT INTO "+stage)
			stop()
			err = r5ConcurrentResult(t, ctx, done)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("canceled INSERT lost context cancellation cause: %v", err)
			}
			if ae, ok := app.As(app.FromAuthz(err)); !ok || ae.Kind != app.KindInternal {
				t.Errorf("canceled INSERT mislabeled as an address conflict: %v", err)
			}
			must(t, hold.Rollback(ctx))
			if after := r5ActivationErrorsState(t, s); after != before {
				t.Error("canceled INSERT committed employee/mailbox/profile/consumption/audit/outbox effects")
			}
		})
	}
}

func TestR5ActivationErrorsPostgresLateAuditUniqueIsInternal(t *testing.T) {
	s := r5ActivationErrorsSeed(t)
	_, err := s.f.pool.Exec(t.Context(), `CREATE FUNCTION r5_activation_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='employee.activate' THEN RAISE EXCEPTION 'private activation audit diagnostic' USING ERRCODE='23505', CONSTRAINT='idx_users_email'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER r5_activation_audit_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION r5_activation_audit_fault()`)
	must(t, err)
	before := r5ActivationErrorsState(t, s)
	err = s.f.st.ActivateEmployee(t.Context(), s.hash, "synthetic-activation-hash")
	r5ActivationErrorsCause(t, err, "23505", "idx_users_email", app.KindInternal, "")
	if after := r5ActivationErrorsState(t, s); after != before {
		t.Error("late audit failure left employee/mailbox/profile/consumption/audit/outbox effects")
	}
	r5ActivationErrorsHTTP(t, s, http.StatusInternalServerError, "")
	if after := r5ActivationErrorsState(t, s); after != before {
		t.Error("late HTTP audit failure left transactional effects")
	}
}

func TestR5ActivationErrorsPostgresSuccessfulActivation(t *testing.T) {
	s := r5ActivationErrorsSeed(t)
	r5ActivationErrorsHTTP(t, s, http.StatusOK, `"activated":true`)
	u, err := s.f.st.GetUserByEmail(t.Context(), s.invite.Email)
	must(t, err)
	mb, err := s.f.st.GetMailboxByAddress(t.Context(), s.invite.Address)
	must(t, err)
	if u == nil || mb == nil || u.TenantID != s.f.tenant.ID || !u.IsActive || u.Role != models.RoleUser || mb.Kind != "personal" || mb.OwnerUserID == nil || *mb.OwnerUserID != u.ID {
		t.Fatal("successful activation did not create the private employee mailbox")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("activation-regression-password")); err != nil {
		t.Error("successful activation did not store the supplied password securely")
	}
	var consumed bool
	var audits, events int
	must(t, s.f.pool.QueryRow(t.Context(), `SELECT
 (SELECT consumed_at IS NOT NULL FROM employee_invitations WHERE id=$1),
 (SELECT count(*) FROM audit_log WHERE tenant_id=$2 AND action='employee.activate' AND resource_id=$3),
 (SELECT count(*) FROM outbox_events WHERE payload->>'tenant_id'=$2::text AND payload->'metadata'->>'action'='employee.activate' AND payload->'metadata'->>'resource_id'=$3::text)`, s.invite.ID, s.f.tenant.ID, u.ID).Scan(&consumed, &audits, &events))
	if !consumed || audits != 1 || events != 1 {
		t.Fatal("successful activation lost invitation, required audit or outbox effects")
	}
	before := r5ActivationErrorsState(t, s)
	r5ActivationErrorsHTTP(t, s, http.StatusBadRequest, "invalid or expired invitation")
	if after := r5ActivationErrorsState(t, s); after != before {
		t.Error("replayed activation changed committed employee assets")
	}
}
