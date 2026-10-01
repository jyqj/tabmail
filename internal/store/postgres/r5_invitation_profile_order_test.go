package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type r5InviteProfileFixture struct {
	f                        *companyFixture
	profile                  *models.PermissionProfile
	super                    authz.Actor
	userBefore, userDetached string
	profileBefore            string
}

func r5InviteProfileSeed(t *testing.T) *r5InviteProfileFixture {
	t.Helper()
	f := seedCompany(t)
	ctx := context.Background()
	version := f.admin.SessionVersion
	f.a.SessionVersion = &version
	profile, err := permissions.New(f.st).CreateProfile(ctx, f.a, &f.tenant.ID, permissions.CreateInput{Name: "AF assigned admin " + uuid.NewString(), CanSend: true, MaxMailboxes: 10, MaxDomains: 2, CanCreateAPIKeys: true})
	must(t, err)
	if profile.IsSystem || profile.TenantID == nil || *profile.TenantID != f.tenant.ID {
		t.Fatal("fixture is not a same-tenant non-system profile")
	}
	superUser := &models.User{TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, Email: uuid.NewString() + "@test.invalid", IsActive: true, PasswordHash: "test-only-super-hash"}
	must(t, f.st.CreateUser(ctx, superUser))
	sv := superUser.SessionVersion
	super := authz.Actor{Type: authz.PrincipalUser, ID: superUser.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsAdmin: true, IsSuperAdmin: true, SessionVersion: &sv}
	_, err = f.st.UpdateUserGuarded(ctx, super, f.tenant.ID, f.admin.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &profile.ID})
	must(t, err)
	s := &r5InviteProfileFixture{f: f, profile: profile, super: super}
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(u)::text,(to_jsonb(u)||jsonb_build_object('permission_profile_id',NULL))::text FROM users u WHERE id=$1 AND permission_profile_id=$2`, f.admin.ID, profile.ID).Scan(&s.userBefore, &s.userDetached))
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM permission_profiles p WHERE id=$1`, profile.ID).Scan(&s.profileBefore))
	// Actual catalog semantics differ: users detach, existing invitations
	// retain their parent through NO ACTION. Do not invent invitation SET NULL.
	var userAction, invitationAction string
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT confdeltype::text FROM pg_constraint WHERE conname='users_permission_profile_id_fkey' AND conrelid='users'::regclass AND confrelid='permission_profiles'::regclass),(SELECT confdeltype::text FROM pg_constraint WHERE conname='employee_invitations_permission_profile_id_fkey' AND conrelid='employee_invitations'::regclass AND confrelid='permission_profiles'::regclass)`).Scan(&userAction, &invitationAction))
	if userAction != "n" || invitationAction != "a" {
		t.Fatal("profile fixture does not match actual users SET NULL / invitation NO ACTION contract")
	}
	return s
}

// The formal permission service may remove an assigned non-system profile;
// its FK SET NULL does not demote or freeze the administrator. This normal
// viability is required before constructing any invitation/delete overlap.
func TestR5InvitationProfileNormalAssignedDelete(t *testing.T) {
	s := r5InviteProfileSeed(t)
	f := s.f
	must(t, permissions.New(f.st).DeleteProfile(context.Background(), f.a, &f.tenant.ID, s.profile.ID))
	r5InviteProfileEffects(t, s, "", false, true)
}

func TestR5InvitationProfileForeignTenantDeleteDenied(t *testing.T) {
	s := r5InviteProfileSeed(t)
	f := s.f
	ctx := context.Background()
	other := &models.Tenant{Name: "AF foreign profile", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, other))
	foreignActor := s.super
	foreignActor.TenantID = other.ID
	foreign, err := permissions.New(f.st).CreateProfile(ctx, foreignActor, &other.ID, permissions.CreateInput{Name: "AF foreign " + uuid.NewString(), TenantID: &other.ID, MaxMailboxes: 10, MaxDomains: 2})
	must(t, err)
	var foreignBefore string
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM permission_profiles p WHERE id=$1`, foreign.ID).Scan(&foreignBefore))
	err = permissions.New(f.st).DeleteProfile(ctx, f.a, &f.tenant.ID, foreign.ID)
	e, ok := app.As(err)
	// requireProfileTenant deliberately hides another tenant's profile.
	// This is the existing scoped NotFound contract, not an arbitrary error.
	if !ok || e.Kind != app.KindNotFound {
		t.Fatalf("tenant admin foreign-profile deletion did not deny: %v", err)
	}
	still, err := f.st.GetPermissionProfile(ctx, foreign.ID)
	must(t, err)
	if still == nil {
		t.Fatal("foreign profile was deleted despite ownership denial")
	}
	var foreignAfter string
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM permission_profiles p WHERE id=$1`, foreign.ID).Scan(&foreignAfter))
	if foreignAfter != foreignBefore {
		t.Fatal("scoped NotFound changed the foreign profile's fields")
	}
	r5InviteProfileEffects(t, s, "", false, false)
}

func TestR5InvitationProfileCompleteCommandsLockOrder(t *testing.T) {
	for _, order := range []string{"invite-first", "profile-delete-first"} {
		t.Run(order, func(t *testing.T) {
			// A fresh normal command proves this exact assigned-admin shape is
			// legal, not a system/foreign-profile fixture that policy forbids.
			probe := r5InviteProfileSeed(t)
			must(t, permissions.New(probe.f.st).DeleteProfile(context.Background(), probe.f.a, &probe.f.tenant.ID, probe.profile.ID))
			r5InviteProfileEffects(t, probe, "", false, true)
			s := r5InviteProfileSeed(t)
			f := s.f
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			email := "af-" + uuid.NewString() + "@test.invalid"
			local := "af-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
			input := company.InvitationInput{Email: email, LocalPart: local, DisplayName: "AF candidate", PermissionProfileID: &s.profile.ID}
			hash := company.Hash("test-only-AF-invitation-" + uuid.NewString())
			beforeAudits, beforeOutbox := r5InviteProfileCounts(t, f, email)
			name := "r5_invite_profile_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			key := "r5-invite-profile:" + s.profile.ID.String()
			var sql string
			if order == "invite-first" {
				sql = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.email='` + email + `' AND NEW.permission_profile_id='` + s.profile.ID.String() + `'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER ` + name + ` BEFORE INSERT ON employee_invitations FOR EACH ROW EXECUTE FUNCTION ` + name + `()`
			} else {
				sql = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='` + s.profile.ID.String() + `'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN OLD; END $$; CREATE TRIGGER ` + name + ` BEFORE DELETE ON permission_profiles FOR EACH ROW EXECUTE FUNCTION ` + name + `()`
			}
			_, err := f.pool.Exec(ctx, sql)
			must(t, err)
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
			must(t, err)
			type invited struct {
				value *company.Invitation
				err   error
			}
			inviteDone := make(chan invited, 1)
			deleteDone := make(chan error, 1)
			invite := func() { v, e := f.st.InviteEmployee(ctx, f.a, input, hash); inviteDone <- invited{v, e} }
			remove := func() { deleteDone <- permissions.New(f.st).DeleteProfile(ctx, f.a, &f.tenant.ID, s.profile.ID) }
			if order == "invite-first" {
				go invite()
				firstPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "INSERT INTO employee_invitations")
				go remove()
				secondPID := r5WaitBlockedBy(t, f, ctx, firstPID, "DELETE FROM permission_profiles")
				r5MaintenanceTrace(t, f, ctx, gate.Conn().PgConn().PID(), firstPID, secondPID)
			} else {
				go remove()
				firstPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "DELETE FROM permission_profiles")
				go invite()
				secondPID := r5InviteProfileWaitOrDone(t, f, ctx, firstPID, inviteDone)
				r5MaintenanceTrace(t, f, ctx, gate.Conn().PgConn().PID(), firstPID, secondPID)
			}
			must(t, gate.Rollback(ctx))
			deleteErr := r5ConcurrentResult(t, ctx, deleteDone)
			var result invited
			select {
			case result = <-inviteDone:
			case <-ctx.Done():
				t.Fatal("invitation terminal missing; timeout is not deadlock evidence")
			}
			inviteDead, deleteDead := r5SQLState(result.err) == "40P01", r5SQLState(deleteErr) == "40P01"
			// When this exact invitation commits first, its real NO ACTION FK
			// correctly refuses deletion of the referenced profile. Accept only
			// that constraint in that phase; all its partial SET NULL effects
			// must roll back with the parent deletion statement below.
			referenceRefusal := false
			if order == "invite-first" && result.err == nil && result.value != nil && result.value.ID != uuid.Nil {
				var pg *pgconn.PgError
				referenceRefusal = errors.As(deleteErr, &pg) && pg.Code == "23503" && pg.ConstraintName == "employee_invitations_permission_profile_id_fkey"
			}
			if deleteErr != nil && !deleteDead && !referenceRefusal {
				t.Fatalf("normal assigned-profile deletion became an unknown error: SQLSTATE=%s err=%v", r5SQLState(deleteErr), deleteErr)
			}
			if result.err != nil && !inviteDead {
				e, ok := app.As(app.FromAuthz(result.err))
				if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindBadRequest && e.Kind != app.KindNotFound) {
					t.Fatalf("unexpected invitation terminal SQLSTATE=%s err=%v", r5SQLState(result.err), result.err)
				}
			}
			if result.err != nil && deleteErr != nil {
				t.Fatal("neither complete command committed")
			}
			invitedOK, deletedOK := result.err == nil, deleteErr == nil
			r5InviteProfileEffects(t, s, email, invitedOK, deletedOK)
			audits, outbox := r5InviteProfileCounts(t, f, email)
			want := 0
			if invitedOK {
				want = 1
				if result.value == nil || result.value.ID == uuid.Nil {
					t.Fatal("successful invitation lost its receipt")
				}
				var stored uuid.UUID
				must(t, f.pool.QueryRow(ctx, `SELECT id FROM employee_invitations WHERE email=$1 AND tenant_id=$2`, email, f.tenant.ID).Scan(&stored))
				if stored != result.value.ID {
					t.Fatal("invitation receipt names no committed exact resource")
				}
			}
			if audits != beforeAudits+want || outbox != beforeOutbox+want {
				t.Fatal("failed invitation retained audit/outbox or success lost required effects")
			}
			if referenceRefusal {
				t.Log("verified invite-first committed receipt; exact invitation-profile NO ACTION 23503 refused profile deletion with full P/U/source/audit/outbox effects preserved")
			}
			if inviteDead || deleteDead {
				t.Errorf("actual invitation/profile-delete 40P01; profile/U SETNULL/invitation/audit/outbox victim atomicity verified: invite=%s delete=%s", r5SQLState(result.err), r5SQLState(deleteErr))
			}
		})
	}
}

// Keep the result channel's exact type local to the helper via a generic
// adapter; every actual response is put back for the terminal assertions.
func r5InviteProfileWaitOrDone[T any](t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, done chan T) uint32 {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-done:
			done <- result
			t.Log("invitation terminal observed before controller release; terminal/effects still checked")
			return 0
		default:
		}
		var pid uint32
		err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("no invitation response or actual blocker observed; not deadlock evidence")
		case <-ticker.C:
		}
	}
}

// Count action-wide deltas in the fresh fixture; source-ID joins would hide
// orphan audit/outbox prefixes if the invitation row itself rolled back.
func r5InviteProfileCounts(t *testing.T, f *companyFixture, email string) (int, int) {
	t.Helper()
	var audits, outbox int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='employee.invite'),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='employee.invite')`).Scan(&audits, &outbox))
	return audits, outbox
}
func r5InviteProfileEffects(t *testing.T, s *r5InviteProfileFixture, email string, invited, deleted bool) {
	t.Helper()
	f := s.f
	ctx := context.Background()
	var profiles int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM permission_profiles WHERE id=$1`, s.profile.ID).Scan(&profiles))
	wantP := 1
	if deleted {
		wantP = 0
	}
	if profiles != wantP {
		t.Fatal("profile delete/victim rollback has wrong parent existence")
	}
	if !deleted {
		var profile string
		must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM permission_profiles p WHERE id=$1`, s.profile.ID).Scan(&profile))
		if profile != s.profileBefore {
			t.Fatal("profile deletion refusal or victim rollback rewrote parent fields")
		}
	}
	var user string
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(u)::text FROM users u WHERE id=$1`, f.admin.ID).Scan(&user))
	wantUser := s.userBefore
	if deleted {
		wantUser = s.userDetached
	}
	if user != wantUser {
		t.Fatal("profile SET NULL or victim rollback changed administrator fields beyond its profile reference")
	}
	if email == "" {
		return
	}
	var count, profileNull, profileMatches int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE permission_profile_id IS NULL),count(*) FILTER(WHERE permission_profile_id=$2) FROM employee_invitations WHERE email=$1 AND tenant_id=$3`, email, s.profile.ID, f.tenant.ID).Scan(&count, &profileNull, &profileMatches))
	wantI := 0
	if invited {
		wantI = 1
	}
	if count != wantI {
		t.Fatal("failed invitation retained a row or success lost its source")
	}
	if invited && (deleted || profileNull != 0 || profileMatches != 1) {
		t.Fatal("committed invitation violated its actual NO ACTION profile parent protection")
	}
}
