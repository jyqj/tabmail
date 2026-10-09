package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type frozenMailboxGrantFixture struct {
	*companyFixture
	grant models.MailboxGrant
}

func newFrozenMailboxGrantFixture(t *testing.T) *frozenMailboxGrantFixture {
	t.Helper()
	f := seedCompany(t)
	g := models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true, CanSend: true}
	must(t, grantCurrent(f.st, context.Background(), f.a, g))
	return &frozenMailboxGrantFixture{companyFixture: f, grant: g}
}

func (f *frozenMailboxGrantFixture) setActive(t *testing.T, active bool) {
	t.Helper()
	u, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &active})
	must(t, err)
	if u == nil || u.IsActive != active {
		t.Fatal("employee state change did not persist")
	}
	f.employee = u
	version := u.SessionVersion
	f.u = authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, SessionVersion: &version}
}

func (f *frozenMailboxGrantFixture) revision(t *testing.T) int64 {
	t.Helper()
	v, err := f.st.ListWorkGrants(context.Background(), f.a, f.shared.ID)
	must(t, err)
	return v.Revision
}

func (f *frozenMailboxGrantFixture) state(t *testing.T) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.mailbox_id,g.user_id) FROM mailbox_grants g WHERE g.tenant_id=$1),
 'mailboxes',(SELECT jsonb_agg(jsonb_build_array(m.id,m.lifecycle_revision) ORDER BY m.id) FROM mailboxes m WHERE m.tenant_id=$1),
 'audits',(SELECT count(*) FROM audit_log WHERE tenant_id=$1),
 'outbox',(SELECT count(*) FROM outbox_events WHERE tenant_id=$1))::text`, f.tenant.ID).Scan(&state))
	return state
}

func (f *frozenMailboxGrantFixture) assertGrant(t *testing.T, mailbox, user uuid.UUID, want bool) {
	t.Helper()
	var exists bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3)`, f.tenant.ID, mailbox, user).Scan(&exists))
	if exists != want {
		t.Errorf("exact mailbox/user grant exists=%v, want %v", exists, want)
	}
}

func frozenMailboxGrantWantKind(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	if v, ok := app.As(err); !ok || v.Kind != kind {
		t.Fatalf("got %v, want application error %s", err, kind)
	}
}

func TestR5FrozenMailboxGrantRevocation(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("frozen mailbox grant acceptance requires TABMAIL_TEST_DB_DSN; no skip")
	}
	ctx := context.Background()
	t.Run("frozen-full-revoke-survives-fresh-session-reactivation", func(t *testing.T) {
		f := newFrozenMailboxGrantFixture(t)
		otherMailbox, err := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "other-support", Kind: "shared"})
		must(t, err)
		must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.other.ID, CanRead: true}))
		must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: otherMailbox.ID, UserID: f.employee.ID, CanSend: true}))
		f.setActive(t, false)
		revision := f.revision(t)
		must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, revision))
		f.assertGrant(t, f.shared.ID, f.employee.ID, false)
		f.assertGrant(t, f.shared.ID, f.other.ID, true)
		f.assertGrant(t, otherMailbox.ID, f.employee.ID, true)
		if got := f.revision(t); got != revision+1 {
			t.Fatalf("successful revoke revision=%d, want %d", got, revision+1)
		}
		var audited int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='mailbox.grant' AND resource_id=$2 AND details->>'user_id'=$3 AND details->>'read'='false' AND details->>'organize'='false' AND details->>'send'='false' AND (details->>'revision')::bigint=$4`, f.tenant.ID, f.shared.ID, f.employee.ID.String(), revision+1).Scan(&audited))
		if audited != 1 {
			t.Fatalf("successful full revoke audit count=%d, want 1", audited)
		}
		f.setActive(t, true)
		_, err = f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
		frozenMailboxGrantWantKind(t, err, app.KindNotFound)
		_, err = f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "must stay revoked"}})
		frozenMailboxGrantWantKind(t, err, app.KindForbidden)
		// A real new session still has the unrelated mailbox's send right.
		access, err := f.st.GetWorkMailbox(ctx, f.u, otherMailbox.ID)
		must(t, err)
		if !access.CanSend {
			t.Fatal("revoke removed unrelated mailbox rights")
		}
	})
	t.Run("active-full-revoke-retains-existing-behavior", func(t *testing.T) {
		f := newFrozenMailboxGrantFixture(t)
		revision := f.revision(t)
		must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, revision))
		f.assertGrant(t, f.shared.ID, f.employee.ID, false)
		if got := f.revision(t); got != revision+1 {
			t.Fatalf("revision=%d, want %d", got, revision+1)
		}
	})
	t.Run("stale-frozen-revoke-cannot-consume-revision", func(t *testing.T) {
		f := newFrozenMailboxGrantFixture(t)
		f.setActive(t, false)
		revision, before := f.revision(t), f.state(t)
		err := f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, revision-1)
		frozenMailboxGrantWantKind(t, err, app.KindConflict)
		if after := f.state(t); after != before {
			t.Fatal("stale revoke changed grant, revision, audit or outbox")
		}
	})
	t.Run("required-audit-failure-rolls-back-revoke-and-revision", func(t *testing.T) {
		f := newFrozenMailboxGrantFixture(t)
		f.setActive(t, false)
		revision, before := f.revision(t), f.state(t)
		_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT frozen_mailbox_grant_audit_fault CHECK(action<>'mailbox.grant') NOT VALID`)
		must(t, err)
		err = f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, revision)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "frozen_mailbox_grant_audit_fault" {
			t.Fatalf("revoke did not reach required audit fault: %v", err)
		}
		f.assertGrant(t, f.shared.ID, f.employee.ID, true)
		if after := f.state(t); after != before {
			t.Fatal("failed audit committed grant deletion, revision or other effects")
		}
		_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT frozen_mailbox_grant_audit_fault`)
		must(t, err)
		must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, revision))
		f.assertGrant(t, f.shared.ID, f.employee.ID, false)
	})
	for name, rights := range map[string]models.MailboxGrant{
		"read": {CanRead: true}, "send": {CanSend: true}, "organize": {CanRead: true, CanOrganize: true},
		"template-send": {CanSend: true, TemplateOnly: true}, "all": {CanRead: true, CanOrganize: true, CanSend: true},
	} {
		t.Run("frozen-positive-grant-denied/"+name, func(t *testing.T) {
			f := newFrozenMailboxGrantFixture(t)
			f.setActive(t, false)
			rights.MailboxID, rights.UserID = f.shared.ID, f.employee.ID
			revision, before := f.revision(t), f.state(t)
			err := f.st.SetWorkGrant(ctx, f.a, rights, revision)
			frozenMailboxGrantWantKind(t, err, app.KindBadRequest)
			if after := f.state(t); after != before {
				t.Fatal("rejected positive grant changed grant, revision, audit or outbox")
			}
		})
	}
	for _, boundary := range []string{"missing-user", "foreign-user", "owner-intrinsic", "non-admin", "template-without-send"} {
		t.Run("revoke-boundary/"+boundary, func(t *testing.T) {
			f := newFrozenMailboxGrantFixture(t)
			g, actor, want := models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, f.a, app.KindBadRequest
			switch boundary {
			case "missing-user":
				g.UserID = uuid.New()
			case "foreign-user":
				foreign := &models.Tenant{Name: "Foreign company", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreign))
				user := &models.User{TenantID: foreign.ID, Email: "foreign@grant.test", Role: models.RoleUser, IsActive: false, PasswordHash: "test-only"}
				must(t, f.st.CreateUser(ctx, user))
				g.UserID = user.ID
			case "owner-intrinsic":
				g.MailboxID = f.personal.ID
			case "non-admin":
				actor = authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
				want = app.KindForbidden
			case "template-without-send":
				g.TemplateOnly = true
			}
			f.setActive(t, false)
			revision, before := f.revision(t), f.state(t)
			err := f.st.SetWorkGrant(ctx, actor, g, revision)
			frozenMailboxGrantWantKind(t, err, want)
			if after := f.state(t); after != before {
				t.Fatal("rejected revoke changed grant, revision, audit or outbox")
			}
		})
	}
}
