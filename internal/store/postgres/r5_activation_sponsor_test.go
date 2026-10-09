package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type r5ActivationSponsorFixture struct {
	f         *companyFixture
	home      *models.Tenant
	sponsor   *models.User
	operator  authz.Actor
	invite    *company.Invitation
	tokenHash string
}

func r5ActivationSponsorSeed(t *testing.T, foreign bool) *r5ActivationSponsorFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN is required for activation sponsor regression")
	}
	f := seedCompany(t)
	s := &r5ActivationSponsorFixture{f: f, home: f.tenant, sponsor: f.admin, operator: f.a}
	actor := f.a
	if foreign {
		s.home = &models.Tenant{Name: "Sponsor home company", PlanID: f.tenant.PlanID}
		must(t, f.st.CreateTenant(t.Context(), s.home))
		s.sponsor = &models.User{TenantID: s.home.ID, Email: "foreign-sponsor@fixture.test", DisplayName: "Foreign sponsor", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "synthetic-sponsor-hash"}
		must(t, f.st.CreateUser(t.Context(), s.sponsor))
		operator := &models.User{TenantID: s.home.ID, Email: "sponsor-supervisor@fixture.test", DisplayName: "Sponsor supervisor", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "synthetic-supervisor-hash"}
		must(t, f.st.CreateUser(t.Context(), operator))
		s.operator = authz.Actor{ID: operator.ID, Type: authz.PrincipalUser, TenantID: s.home.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true}
		actor = authz.Actor{ID: s.sponsor.ID, Type: authz.PrincipalUser, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true}
	}
	s.tokenHash = company.Hash("r5-activation-sponsor-" + uuid.NewString())
	var err error
	s.invite, err = f.st.InviteEmployee(t.Context(), actor, company.InvitationInput{Email: "sponsored-employee@fixture.test", LocalPart: "sponsored-employee", DisplayName: "Sponsored employee"}, s.tokenHash)
	must(t, err)
	return s
}

// Observe only the target company. The concurrent supervisor must be allowed
// to commit its own home-company audit without falsifying activation rollback.
func r5ActivationSponsorState(t *testing.T, s *r5ActivationSponsorFixture) string {
	t.Helper()
	var state string
	must(t, s.f.pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'invitation',(SELECT to_jsonb(i) FROM employee_invitations i WHERE id=$1),
 'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE tenant_id=$4 AND email=$2),
 'mailboxes',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM mailboxes m WHERE tenant_id=$4 AND full_address=$3),
 'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM permission_profiles p WHERE tenant_id=$4),
 'audit',(SELECT count(*) FROM audit_log WHERE tenant_id=$4),
 'outbox',(SELECT count(*) FROM outbox_events WHERE payload->>'tenant_id'=$4::text))::text`, s.invite.ID, s.invite.Email, s.invite.Address, s.f.tenant.ID).Scan(&state))
	return state
}

func r5ActivationSponsorChange(t *testing.T, s *r5ActivationSponsorFixture, ctx context.Context, change string) {
	t.Helper()
	patch := models.UserAdminPatch{}
	switch change {
	case "freeze":
		active := false
		patch.IsActive = &active
	case "demote_admin":
		role := models.RoleAdmin
		patch.Role = &role
	case "demote_user":
		role := models.RoleUser
		patch.Role = &role
	case "delete":
		must(t, s.f.st.DeleteUserGuarded(ctx, s.operator, s.home.ID, s.sponsor.ID))
		return
	default:
		t.Fatal("unknown sponsor fixture change")
	}
	_, err := s.f.st.UpdateUserGuarded(ctx, s.operator, s.home.ID, s.sponsor.ID, patch)
	must(t, err)
}

func r5ActivationSponsorExpectError(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	actual, ok := app.As(app.FromAuthz(err))
	if !ok || actual.Kind != kind {
		t.Errorf("activation error kind=%s expected; actual=%v", kind, err)
	}
}

func r5ActivationSponsorExpectSuccess(t *testing.T, s *r5ActivationSponsorFixture) {
	t.Helper()
	u, err := s.f.st.GetUserByEmail(t.Context(), s.invite.Email)
	must(t, err)
	mb, err := s.f.st.GetMailboxByAddress(t.Context(), s.invite.Address)
	must(t, err)
	if u == nil || mb == nil || u.TenantID != s.f.tenant.ID || u.Role != models.RoleUser || !u.IsActive || mb.Kind != "personal" || mb.OwnerUserID == nil || *mb.OwnerUserID != u.ID {
		t.Fatal("valid sponsor did not create the target company's private employee mailbox")
	}
	var consumed bool
	var audits, events int
	must(t, s.f.pool.QueryRow(t.Context(), `SELECT
 (SELECT consumed_at IS NOT NULL FROM employee_invitations WHERE id=$1),
 (SELECT count(*) FROM audit_log WHERE tenant_id=$2 AND action='employee.activate' AND resource_id=$3),
 (SELECT count(*) FROM outbox_events WHERE payload->>'tenant_id'=$2::text AND payload->'metadata'->>'action'='employee.activate' AND payload->'metadata'->>'resource_id'=$3::text)`, s.invite.ID, s.f.tenant.ID, u.ID).Scan(&consumed, &audits, &events))
	if !consumed || audits != 1 || events != 1 {
		t.Fatal("valid activation lost exactly-once invitation, required audit or outbox effect")
	}
}

func TestR5ActivationSponsorPostgresRevokedDuringOutboxWait(t *testing.T) {
	for _, change := range []string{"freeze", "demote_admin", "demote_user", "delete"} {
		t.Run(change, func(t *testing.T) {
			s := r5ActivationSponsorSeed(t, true)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			before := r5ActivationSponsorState(t, s)
			hold, err := s.f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `LOCK TABLE outbox_events IN SHARE MODE`)
			must(t, err)
			done := make(chan error, 1)
			go func() { done <- s.f.st.ActivateEmployee(ctx, s.tokenHash, "synthetic-sponsored-password") }()
			r5WaitBlockedBy(t, s.f, ctx, hold.Conn().PgConn().PID(), "outbox_events")
			// The target company lock does not cover this independent home.
			// This complete production command must actually commit first.
			r5ActivationSponsorChange(t, s, ctx, change)
			current, err := s.f.st.GetUser(ctx, s.sponsor.ID)
			must(t, err)
			if current != nil && current.IsActive && current.Role == models.RoleSuperAdmin {
				t.Fatal("production sponsor withdrawal did not commit")
			}
			must(t, hold.Rollback(ctx))
			result := r5ConcurrentResult(t, ctx, done)
			r5ActivationSponsorExpectError(t, result, app.KindForbidden)
			if after := r5ActivationSponsorState(t, s); after != before {
				t.Error("withdrawn sponsor activation committed employee/mailbox/profile/consumption/audit/outbox effects")
			}
		})
	}
}

// The production supervisor holds the actual user row while its UPDATE waits
// at a test-only trigger. The final sponsor check must return while that writer
// remains blocked, rather than waiting late in the reverse lock order.
func TestR5ActivationSponsorPostgresBusyWriterRejectsWithoutWaiting(t *testing.T) {
	s := r5ActivationSponsorSeed(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err := s.f.pool.Exec(ctx, `CREATE FUNCTION r5_sponsor_writer_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.email='foreign-sponsor@fixture.test' THEN PERFORM pg_advisory_xact_lock(714600246); END IF; RETURN NEW; END $$; CREATE TRIGGER r5_sponsor_writer_barrier AFTER UPDATE ON users FOR EACH ROW EXECUTE FUNCTION r5_sponsor_writer_barrier()`)
	must(t, err)
	before := r5ActivationSponsorState(t, s)
	outboxHold, err := s.f.pool.Begin(ctx)
	must(t, err)
	defer outboxHold.Rollback(context.Background())
	_, err = outboxHold.Exec(ctx, `LOCK TABLE outbox_events IN SHARE MODE`)
	must(t, err)
	writerHold, err := s.f.pool.Begin(ctx)
	must(t, err)
	defer writerHold.Rollback(context.Background())
	_, err = writerHold.Exec(ctx, `SELECT pg_advisory_xact_lock(714600246)`)
	must(t, err)
	activationDone := make(chan error, 1)
	go func() { activationDone <- s.f.st.ActivateEmployee(ctx, s.tokenHash, "synthetic-sponsored-password") }()
	r5WaitBlockedBy(t, s.f, ctx, outboxHold.Conn().PgConn().PID(), "outbox_events")
	writerDone := make(chan error, 1)
	go func() {
		name := "Current sponsor display name"
		_, err := s.f.st.UpdateUserGuarded(ctx, s.operator, s.home.ID, s.sponsor.ID, models.UserAdminPatch{DisplayName: &name})
		writerDone <- err
	}()
	r5WaitBlockedBy(t, s.f, ctx, writerHold.Conn().PgConn().PID(), "UPDATE users SET display_name")
	must(t, outboxHold.Rollback(ctx))
	activationErr := r5ConcurrentResult(t, ctx, activationDone)
	// The writer is still blocked here; activation must already be terminal.
	r5ActivationSponsorExpectError(t, activationErr, app.KindConflict)
	if after := r5ActivationSponsorState(t, s); after != before {
		t.Error("busy sponsor activation left transactional effects")
	}
	must(t, writerHold.Rollback(ctx))
	r5AwaitOperation(t, ctx, writerDone)
	current, err := s.f.st.GetUser(ctx, s.sponsor.ID)
	must(t, err)
	if current == nil || current.DisplayName != "Current sponsor display name" {
		t.Fatal("production writer failed to commit after activation released its locks")
	}
}

func TestR5ActivationSponsorPostgresCurrentSponsor(t *testing.T) {
	for _, mode := range []string{"local_admin", "foreign_super", "foreign_frozen", "foreign_demoted", "foreign_reenabled"} {
		t.Run(mode, func(t *testing.T) {
			s := r5ActivationSponsorSeed(t, mode != "local_admin")
			if mode == "foreign_frozen" || mode == "foreign_reenabled" {
				r5ActivationSponsorChange(t, s, t.Context(), "freeze")
			}
			if mode == "foreign_demoted" {
				r5ActivationSponsorChange(t, s, t.Context(), "demote_admin")
			}
			if mode == "foreign_reenabled" {
				active := true
				_, err := s.f.st.UpdateUserGuarded(t.Context(), s.operator, s.home.ID, s.sponsor.ID, models.UserAdminPatch{IsActive: &active})
				must(t, err)
			}
			before := r5ActivationSponsorState(t, s)
			err := s.f.st.ActivateEmployee(t.Context(), s.tokenHash, "synthetic-sponsored-password")
			if mode == "foreign_frozen" || mode == "foreign_demoted" {
				r5ActivationSponsorExpectError(t, err, app.KindForbidden)
				if r5ActivationSponsorState(t, s) != before {
					t.Fatal("unqualified current sponsor left activation effects")
				}
			} else {
				must(t, err)
				r5ActivationSponsorExpectSuccess(t, s)
			}
		})
	}
}

func TestR5ActivationSponsorPostgresCancellationDuringOutboxWait(t *testing.T) {
	s := r5ActivationSponsorSeed(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	before := r5ActivationSponsorState(t, s)
	hold, err := s.f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `LOCK TABLE outbox_events IN SHARE MODE`)
	must(t, err)
	activationCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- s.f.st.ActivateEmployee(activationCtx, s.tokenHash, "synthetic-sponsored-password") }()
	r5WaitBlockedBy(t, s.f, ctx, hold.Conn().PgConn().PID(), "outbox_events")
	stop()
	if err := r5ConcurrentResult(t, ctx, done); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled activation lost context cause: %v", err)
	}
	must(t, hold.Rollback(ctx))
	if r5ActivationSponsorState(t, s) != before {
		t.Fatal("canceled activation left target-company effects")
	}
}
