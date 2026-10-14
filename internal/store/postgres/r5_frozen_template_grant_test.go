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

type frozenTemplateFixture struct {
	*companyFixture
	grant   company.TemplateGrant
	version *company.TemplateVersion
}

func newFrozenTemplateFixture(t *testing.T) *frozenTemplateFixture {
	t.Helper()
	f := seedCompany(t)
	template, err := f.st.SaveMailTemplate(context.Background(), f.a, company.Template{Name: "Employee template", Draft: templateDraft()})
	must(t, err)
	version, err := f.st.PublishMailTemplate(context.Background(), f.a, template.ID, template.Revision)
	must(t, err)
	grant := company.TemplateGrant{TemplateID: template.ID, MailboxID: f.personal.ID, UserID: f.employee.ID}
	must(t, f.st.SetTemplateGrant(context.Background(), f.a, grant, true))
	return &frozenTemplateFixture{f, grant, version}
}

func (f *frozenTemplateFixture) setActive(t *testing.T, active bool) {
	t.Helper()
	u, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &active})
	must(t, err)
	if u == nil || u.IsActive != active {
		t.Fatal("formal employee state change did not persist")
	}
	// Use a fresh employee session after reactivation. Stale-session rejection
	// must not be mistaken for proof that a template grant stayed revoked.
	f.employee = u
	version := u.SessionVersion
	f.u = authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, SessionVersion: &version}
}

func (f *frozenTemplateFixture) assertGrant(t *testing.T, grant company.TemplateGrant, want bool) {
	t.Helper()
	var exists bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM mail_template_grants WHERE tenant_id=$1 AND template_id=$2 AND mailbox_id=$3 AND user_id=$4)`, f.tenant.ID, grant.TemplateID, grant.MailboxID, grant.UserID).Scan(&exists))
	if exists != want {
		t.Errorf("exact template/mailbox/user grant exists=%v, want %v", exists, want)
	}
}

// Compare actual scoped grant rows and required effects. The fixture is one
// owned database; unrelated tenant/user setup is completed before this snapshot.
func (f *frozenTemplateFixture) state(t *testing.T) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.tenant_id,g.template_id,g.mailbox_id,g.user_id) FROM mail_template_grants g),
 'audits',(SELECT count(*) FROM audit_log),
 'outbox',(SELECT count(*) FROM outbox_events))::text`).Scan(&state))
	return state
}

func (f *frozenTemplateFixture) assertUsage(t *testing.T, want bool) {
	t.Helper()
	ctx := context.Background()
	mailbox, err := f.st.GetWorkMailbox(ctx, f.u, f.personal.ID)
	must(t, err)
	if mailbox == nil || !mailbox.CanSend {
		t.Fatal("fixture lost current mailbox send rights")
	}
	listed, err := f.st.ListUsableTemplates(ctx, f.u, f.personal.ID)
	must(t, err)
	found := false
	for _, version := range listed {
		found = found || version.ID == f.version.ID
	}
	if found != want {
		t.Errorf("usable template list contains version=%v, want %v", found, want)
	}
	version, _, _, err := f.st.TemplateForSend(ctx, f.tenant.ID, &f.employee.ID, nil, f.personal.ID, f.version.ID)
	if want {
		if err != nil || version == nil || version.ID != f.version.ID {
			t.Errorf("granted template send lookup failed: %v", err)
		}
	} else if !authz.IsAuthzError(err) || err.Error() != "published template unavailable or not granted" {
		t.Errorf("revoked grant did not produce the template eligibility denial: %v", err)
	}
}

func TestR5TemplateGrantFrozenEmployeeRevocation(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("frozen template grant acceptance requires TABMAIL_TEST_DB_DSN; no skip")
	}
	ctx := context.Background()
	t.Run("active-grant-revoke", func(t *testing.T) {
		f := newFrozenTemplateFixture(t)
		f.assertUsage(t, true)
		must(t, f.st.SetTemplateGrant(ctx, f.a, f.grant, false))
		f.assertGrant(t, f.grant, false)
		f.assertUsage(t, false)
	})
	t.Run("frozen-revoke-survives-reactivation", func(t *testing.T) {
		f := newFrozenTemplateFixture(t)
		f.assertUsage(t, true)
		otherUser := f.grant
		otherUser.UserID = f.other.ID
		otherMailbox := f.grant
		otherMailbox.MailboxID = f.shared.ID
		otherTemplate, err := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Retained template", Draft: templateDraft()})
		must(t, err)
		otherDefinition := f.grant
		otherDefinition.TemplateID = otherTemplate.ID
		for _, grant := range []company.TemplateGrant{otherUser, otherMailbox, otherDefinition} {
			must(t, f.st.SetTemplateGrant(ctx, f.a, grant, true))
		}
		f.setActive(t, false)
		f.assertGrant(t, f.grant, true)
		must(t, f.st.SetTemplateGrant(ctx, f.a, f.grant, false))
		// Repeated revocation remains a successful no-op on the grant relation.
		must(t, f.st.SetTemplateGrant(ctx, f.a, f.grant, false))
		f.assertGrant(t, f.grant, false)
		for _, grant := range []company.TemplateGrant{otherUser, otherMailbox, otherDefinition} {
			f.assertGrant(t, grant, true)
		}
		var audited int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='template.grant' AND resource_id=$2 AND details->>'mailbox_id'=$3 AND details->>'user_id'=$4 AND details->>'enabled'='false'`, f.tenant.ID, f.grant.TemplateID, f.grant.MailboxID.String(), f.grant.UserID.String()).Scan(&audited))
		if audited != 2 {
			t.Errorf("successful revocations required audit rows=%d, want 2", audited)
		}
		f.setActive(t, true)
		f.assertUsage(t, false)
	})
	t.Run("frozen-grant-still-denied", func(t *testing.T) {
		f := newFrozenTemplateFixture(t)
		must(t, f.st.SetTemplateGrant(ctx, f.a, f.grant, false))
		f.setActive(t, false)
		before := f.state(t)
		err := f.st.SetTemplateGrant(ctx, f.a, f.grant, true)
		if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindBadRequest {
			t.Errorf("inactive employee received a grant: %v", err)
		}
		if f.state(t) != before {
			t.Error("rejected grant changed grant/audit/outbox rows")
		}
		f.assertGrant(t, f.grant, false)
	})
	t.Run("required-audit-failure-rolls-back", func(t *testing.T) {
		f := newFrozenTemplateFixture(t)
		f.setActive(t, false)
		before := f.state(t)
		_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT frozen_template_grant_audit_fault CHECK(action<>'template.grant') NOT VALID`)
		must(t, err)
		err = f.st.SetTemplateGrant(ctx, f.a, f.grant, false)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "frozen_template_grant_audit_fault" {
			t.Errorf("revocation did not reach the injected required audit failure: %v", err)
		}
		f.assertGrant(t, f.grant, true)
		if f.state(t) != before {
			t.Error("failed required audit committed grant deletion or effects")
		}
		_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT frozen_template_grant_audit_fault`)
		must(t, err)
		must(t, f.st.SetTemplateGrant(ctx, f.a, f.grant, false))
		f.assertGrant(t, f.grant, false)
	})
	for _, boundary := range []string{"foreign-user", "missing-user", "foreign-mailbox", "foreign-template", "non-admin"} {
		t.Run(boundary, func(t *testing.T) {
			f := newFrozenTemplateFixture(t)
			grant, actor, want := f.grant, f.a, app.KindBadRequest
			if boundary == "foreign-user" || boundary == "foreign-mailbox" || boundary == "foreign-template" {
				foreign := &models.Tenant{Name: "Foreign company", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreign))
				user := &models.User{TenantID: foreign.ID, Email: "foreign@contact.test", Role: models.RoleAdmin, IsActive: true, PasswordHash: "test-only-foreign"}
				must(t, f.st.CreateUser(ctx, user))
				switch boundary {
				case "foreign-user":
					grant.UserID = user.ID
				case "foreign-mailbox":
					zone := &models.DomainZone{TenantID: foreign.ID, Domain: "foreign.test", IsVerified: true, MXVerified: true}
					must(t, f.st.CreateZone(ctx, zone))
					mailbox := &models.Mailbox{TenantID: foreign.ID, ZoneID: zone.ID, LocalPart: "employee", ResolvedDomain: zone.Domain, FullAddress: "employee@foreign.test", AccessMode: models.AccessToken, OwnerUserID: &user.ID}
					must(t, f.st.CreateMailbox(ctx, mailbox))
					grant.MailboxID, want = mailbox.ID, app.KindNotFound
				case "foreign-template":
					foreignActor := authz.Actor{Type: authz.PrincipalUser, ID: user.ID, TenantID: foreign.ID, Role: models.RoleAdmin, IsAdmin: true}
					template, err := f.st.SaveMailTemplate(ctx, foreignActor, company.Template{Name: "Foreign template", Draft: templateDraft()})
					must(t, err)
					grant.TemplateID, want = template.ID, app.KindNotFound
				}
			} else if boundary == "missing-user" {
				grant.UserID = uuid.New()
			} else {
				actor = authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
				want = app.KindForbidden
			}
			// Keep the local target active here: a foreign resource must be
			// rejected by its own tenant/authority gate, not an unrelated freeze.
			before := f.state(t)
			err := f.st.SetTemplateGrant(ctx, actor, grant, false)
			if appErr, ok := app.As(err); !ok || appErr.Kind != want {
				t.Errorf("revocation boundary result=%v, want %s", err, want)
			}
			f.assertGrant(t, f.grant, true)
			if f.state(t) != before {
				t.Error("rejected revocation changed grant/audit/outbox rows")
			}
		})
	}
}
