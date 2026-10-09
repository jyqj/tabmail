package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// These additions cover authority boundaries, not a second permission policy.
// The existing companyTx/currentMemberActor and database revision owners remain
// the only command implementation. SQL below only observes state/holds fences.
func r5PermissionAuthoritySeed(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("R5 authority matrix requires owned TABMAIL_TEST_DB_DSN; no skip")
	}
	return seedCompany(t)
}

func r5PermissionAuthorityState(t *testing.T, f *companyFixture) string {
	t.Helper()
	var state string
	// A sequence has no composite row type. Observe only its explicit durable
	// allocation state, never nextval (which would itself consume a revision).
	// This helper is used for denials/reads/no-ops before mutation, not for an
	// audit/outbox rollback: PostgreSQL nextval may leave a valid rollback gap.
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM users u),'overrides',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM user_permission_overrides o),'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM permission_profiles p),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM outbox_events o),'allocator',(SELECT jsonb_build_object('last_value',last_value,'is_called',is_called) FROM permission_editor_revision_seq))::text`).Scan(&state))
	return state
}

func r5PermissionAuthorityUser(t *testing.T, f *companyFixture, tenant uuid.UUID, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{TenantID: tenant, Email: uuid.NewString() + "@authority.test", Role: role, IsActive: true, PasswordHash: "test-only-authority-password"}
	must(t, f.st.CreateUser(context.Background(), u))
	return u
}

func r5PermissionAuthorityActor(u *models.User, tenant uuid.UUID) authz.Actor {
	v := u.SessionVersion
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: tenant, Role: u.Role, IsAdmin: u.Role == models.RoleAdmin, IsSuperAdmin: u.Role == models.RoleSuperAdmin, SessionVersion: &v}
}

func r5PermissionAuthoritySnapshot(t *testing.T, f *companyFixture, super authz.Actor, target uuid.UUID, tenant uuid.UUID) company.PermissionRevision {
	t.Helper()
	super.TenantID = tenant
	s, err := f.st.GetPermissionEditorSnapshot(context.Background(), super, target)
	if err != nil {
		if e, ok := app.As(err); ok && e.Kind == app.KindNotFound {
			return company.PermissionRevision{UserID: target, TenantID: tenant, UserRevision: "1"}
		}
		must(t, err)
	}
	return s.Revision
}

func r5PermissionAuthorityProfile(t *testing.T, f *companyFixture, super authz.Actor, target uuid.UUID, tenant uuid.UUID) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: &tenant, Name: "Authority " + uuid.NewString(), CanSend: true, DailySendQuota: 31, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	p, err := f.st.GetPermissionProfile(context.Background(), p.ID)
	must(t, err)
	r := r5PermissionAuthoritySnapshot(t, f, super, target, tenant)
	super.TenantID = tenant
	_, err = f.st.AssignPermissionEditor(context.Background(), super, target, company.PermissionAssignmentCommand{ExpectedRevision: r, ProfileID: &p.ID, ProfileRevision: &p.Revision})
	must(t, err)
	return p
}

// Calls have valid observations and valid payloads. A forbidden result therefore
// cannot be satisfied accidentally by malformed input or an earlier CAS 409.
func r5PermissionAuthorityCommand(t *testing.T, f *companyFixture, actor authz.Actor, op string, target uuid.UUID, revision company.PermissionRevision, p *models.PermissionProfile, members []company.PermissionRevision) error {
	t.Helper()
	ctx := context.Background()
	switch op {
	case "get":
		out, err := f.st.GetPermissionEditorSnapshot(ctx, actor, target)
		if err != nil && out != nil {
			t.Fatal("denied get returned snapshot")
		}
		return err
	case "patch":
		out, err := f.st.PatchPermissionEditor(ctx, actor, target, company.PermissionEditorCommand{ExpectedRevision: revision})
		if err != nil && out != nil {
			t.Fatal("denied patch returned snapshot")
		}
		return err
	case "assignment":
		out, err := f.st.AssignPermissionEditor(ctx, actor, target, company.PermissionAssignmentCommand{ExpectedRevision: revision, ProfileID: revision.ProfileID, ProfileRevision: revision.ProfileRevision})
		if err != nil && out != nil {
			t.Fatal("denied assignment returned snapshot")
		}
		return err
	case "profile-cas":
		desired := *p
		desired.Description = "authority CAS observation"
		out, err := f.st.UpdatePermissionProfileCAS(ctx, actor, &desired, p.Revision)
		if err != nil && out != nil {
			t.Fatal("denied profile CAS returned profile")
		}
		return err
	case "preview":
		out, err := f.st.GetPermissionProfileDeletionPreview(ctx, actor, p.ID)
		if err != nil && out != nil {
			t.Fatal("denied preview returned affected members")
		}
		return err
	case "delete":
		return f.st.DeletePermissionProfileCAS(ctx, actor, p.ID, p.Revision, members)
	default:
		t.Fatal("unknown authority operation")
		return nil
	}
}

func TestR5PermissionAuthorityMatrixHierarchy(t *testing.T) {
	for _, role := range []models.UserRole{models.RoleUser, models.RoleAdmin, models.RoleSuperAdmin} {
		t.Run(string(role), func(t *testing.T) {
			f := r5PermissionAuthoritySeed(t)
			root := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
			super := r5PermissionAuthorityActor(root, f.tenant.ID)
			principal := r5PermissionAuthorityUser(t, f, f.tenant.ID, role)
			actor := r5PermissionAuthorityActor(principal, f.tenant.ID)
			otherTenant := &models.Tenant{Name: "Authority foreign", PlanID: f.tenant.PlanID}
			must(t, f.st.CreateTenant(context.Background(), otherTenant))
			lower, upper := models.RoleUser, models.RoleAdmin
			if role == models.RoleAdmin {
				upper = models.RoleSuperAdmin
			}
			if role == models.RoleSuperAdmin {
				lower, upper = models.RoleAdmin, models.RoleSuperAdmin
			}
			// RoleUser has no lower role and RoleSuperAdmin has no upper role.
			// Those two slots exercise the extremal peer, not an invented role.
			targets := []struct {
				name    string
				user    *models.User
				tenant  uuid.UUID
				missing bool
			}{
				{"self", principal, f.tenant.ID, false},
				{"lower-or-minimum-peer", r5PermissionAuthorityUser(t, f, f.tenant.ID, lower), f.tenant.ID, false},
				{"peer", r5PermissionAuthorityUser(t, f, f.tenant.ID, role), f.tenant.ID, false},
				{"upper-or-maximum-peer", r5PermissionAuthorityUser(t, f, f.tenant.ID, upper), f.tenant.ID, false},
				{"cross-tenant", r5PermissionAuthorityUser(t, f, otherTenant.ID, models.RoleUser), otherTenant.ID, false},
				{"missing", &models.User{ID: uuid.New()}, f.tenant.ID, true},
			}
			for _, target := range targets {
				t.Run(target.name, func(t *testing.T) {
					p := &models.PermissionProfile{ID: uuid.New(), TenantID: &f.tenant.ID, Name: "Missing authority profile", Revision: "1"}
					if !target.missing {
						p = r5PermissionAuthorityProfile(t, f, super, target.user.ID, target.tenant)
					}
					want := app.ErrorKind("")
					if role == models.RoleUser {
						want = app.KindForbidden
					} else if target.missing || target.tenant != actor.TenantID {
						want = app.KindNotFound
					} else if role == models.RoleAdmin && target.user.Role != models.RoleUser {
						want = app.KindForbidden
					}
					for _, op := range []string{"get", "patch", "assignment", "profile-cas", "preview", "delete"} {
						t.Run(op, func(t *testing.T) {
							r := r5PermissionAuthoritySnapshot(t, f, super, target.user.ID, target.tenant)
							// Bind to selected scope even for a real foreign target.
							r.TenantID = actor.TenantID
							members := []company.PermissionRevision{}
							if !target.missing {
								var err error
								p, err = f.st.GetPermissionProfile(context.Background(), p.ID)
								must(t, err)
								scope := super
								scope.TenantID = target.tenant
								preview, err := f.st.GetPermissionProfileDeletionPreview(context.Background(), scope, p.ID)
								must(t, err)
								members = preview.Members
							}
							before := r5PermissionAuthorityState(t, f)
							err := r5PermissionAuthorityCommand(t, f, actor, op, target.user.ID, r, p, members)
							if want == "" {
								must(t, err)
							} else {
								r5EditorRequireKind(t, err, want)
							}
							if (want != "" || op == "get" || op == "patch" || op == "assignment" || op == "preview") && r5PermissionAuthorityState(t, f) != before {
								t.Fatal("denial/read/authorized empty command changed state, audit, outbox or allocator")
							}
						})
					}
					// Legitimate cross-tenant super-admin selection is a positive
					// control, not blanket rejection of every foreign identity.
					if role == models.RoleSuperAdmin && target.tenant != actor.TenantID {
						selected := actor
						selected.TenantID = target.tenant
						_, err := f.st.GetPermissionEditorSnapshot(context.Background(), selected, target.user.ID)
						must(t, err)
					}
				})
			}
		})
	}
}

func TestR5PermissionAuthorityMatrixNonInteractive(t *testing.T) {
	f := r5PermissionAuthoritySeed(t)
	super := r5PermissionAuthorityActor(r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin), f.tenant.ID)
	p := r5PermissionAuthorityProfile(t, f, super, f.employee.ID, f.tenant.ID)
	r := r5PermissionAuthoritySnapshot(t, f, super, f.employee.ID, f.tenant.ID)
	preview, err := f.st.GetPermissionProfileDeletionPreview(context.Background(), super, p.ID)
	must(t, err)
	for _, tc := range []struct {
		name  string
		actor authz.Actor
	}{
		{"owned-admin-key", authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: f.tenant.ID, OwnerUserID: &f.admin.ID, IsAdmin: true, Role: models.RoleAdmin}},
		{"ownerless-key", authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: f.tenant.ID, TenantWide: true, IsAdmin: true}},
		{"no-principal", authz.Actor{TenantID: f.tenant.ID, IsAdmin: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, op := range []string{"get", "patch", "assignment", "profile-cas", "preview", "delete"} {
				t.Run(op, func(t *testing.T) {
					before := r5PermissionAuthorityState(t, f)
					r5EditorRequireKind(t, r5PermissionAuthorityCommand(t, f, tc.actor, op, f.employee.ID, r, p, preview.Members), app.KindForbidden)
					if r5PermissionAuthorityState(t, f) != before {
						t.Fatal("noninteractive rejection changed durable state")
					}
				})
			}
		})
	}
}

func TestR5PermissionAuthorityMatrixEmptyCASAndNullableIntent(t *testing.T) {
	f := r5PermissionAuthoritySeed(t)
	super := r5PermissionAuthorityActor(r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin), f.tenant.ID)
	r5PermissionAuthorityProfile(t, f, super, f.employee.ID, f.tenant.ID)
	old := r5PermissionAuthoritySnapshot(t, f, super, f.employee.ID, f.tenant.ID)
	first, err := f.st.PatchPermissionEditor(context.Background(), f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old, Patch: r5EditorPatch(t, `{"can_send":false,"domain_access":{"mode":"none","zone_ids":[]},"daily_receive_quota":null}`)})
	must(t, err)
	quota, err := f.st.PatchPermissionEditor(context.Background(), f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: first.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":23}`)})
	must(t, err)
	if quota.Overrides == nil || quota.Overrides.CanSend == nil || *quota.Overrides.CanSend || quota.Overrides.DomainAccess.Mode != "none" || quota.Overrides.DailyReceiveQuota != nil || quota.FieldSources["daily_receive_quota"] != "profile" || quota.Effective.CanSend || quota.Effective.AllowsZone(f.zone.ID) || quota.Effective.DailySendQuota != 23 {
		t.Fatal("quota-only patch altered false/domain-none/nullable inheritance intent")
	}
	before := r5PermissionAuthorityState(t, f)
	_, err = f.st.PatchPermissionEditor(context.Background(), f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old})
	r5EditorRequireKind(t, err, app.KindConflict)
	_, err = f.st.PatchPermissionEditor(context.Background(), f.u, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: quota.Revision})
	r5EditorRequireKind(t, err, app.KindForbidden)
	unchanged, err := f.st.PatchPermissionEditor(context.Background(), f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: quota.Revision})
	must(t, err)
	if !unchanged.Revision.Equal(quota.Revision) || r5PermissionAuthorityState(t, f) != before {
		t.Fatal("empty patch bypassed authority/CAS or consumed revision/audit/outbox")
	}
}

func r5PermissionAuthorityWait(t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, done <-chan error) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("command completed before actual lock wait: %v", err)
		default:
		}
		var pid uint32
		err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("no actual PostgreSQL authority wait edge")
		case <-tick.C:
		}
	}
}

func TestR5PermissionAuthorityMatrixLockWaitRecheck(t *testing.T) {
	for _, change := range []string{"demote", "freeze", "session-version"} {
		for _, op := range []string{"patch", "assignment", "profile-cas", "delete"} {
			t.Run(change+"/"+op, func(t *testing.T) {
				f := r5PermissionAuthoritySeed(t)
				super := r5PermissionAuthorityActor(r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin), f.tenant.ID)
				p := r5PermissionAuthorityProfile(t, f, super, f.employee.ID, f.tenant.ID)
				r := r5PermissionAuthoritySnapshot(t, f, super, f.employee.ID, f.tenant.ID)
				preview, err := f.st.GetPermissionProfileDeletionPreview(context.Background(), super, p.ID)
				must(t, err)
				u, err := f.st.GetUser(context.Background(), f.admin.ID)
				must(t, err)
				actor := r5PermissionAuthorityActor(u, f.tenant.ID)
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				gate, err := f.pool.Begin(ctx)
				must(t, err)
				defer gate.Rollback(context.Background())
				_, err = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
				must(t, err)
				patch := r5EditorPatch(t, `{"daily_send_quota":73}`)
				done := make(chan error, 1)
				go func() {
					switch op {
					case "patch":
						_, err := f.st.PatchPermissionEditor(ctx, actor, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: r, Patch: patch})
						done <- err
					case "assignment":
						_, err := f.st.AssignPermissionEditor(ctx, actor, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: r})
						done <- err
					case "profile-cas":
						desired := *p
						desired.CanSend = false
						_, err := f.st.UpdatePermissionProfileCAS(ctx, actor, &desired, p.Revision)
						done <- err
					case "delete":
						done <- f.st.DeletePermissionProfileCAS(ctx, actor, p.ID, p.Revision, preview.Members)
					}
				}()
				r5PermissionAuthorityWait(t, f, ctx, gate.Conn().PgConn().PID(), done)
				switch change {
				case "demote":
					u.Role = models.RoleUser
					must(t, f.st.UpdateUser(ctx, u))
				case "freeze":
					u.IsActive = false
					must(t, f.st.UpdateUser(ctx, u))
				case "session-version":
					must(t, f.st.UpdateUserPassword(ctx, u.ID, "test-only-new-authority-password"))
				}
				before := r5PermissionAuthorityState(t, f)
				must(t, gate.Commit(ctx))
				select {
				case err := <-done:
					r5EditorRequireKind(t, err, app.KindForbidden)
				case <-ctx.Done():
					t.Fatal("authority command never reached terminal result")
				}
				if r5PermissionAuthorityState(t, f) != before {
					t.Fatal("post-wait stale authority produced command effects")
				}
			})
		}
	}
}
