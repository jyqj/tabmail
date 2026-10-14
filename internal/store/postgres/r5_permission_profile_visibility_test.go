package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func r5VisibilityProfile(t *testing.T, f *companyFixture, tenant *uuid.UUID, name string) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: tenant, Name: name, Description: "scope-private-" + uuid.NewString(), CanSend: true, DailySendQuota: 17, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	return p
}

func r5VisibilityExpected(t *testing.T, f *companyFixture, scope *uuid.UUID) []*models.PermissionProfile {
	t.Helper()
	all, err := f.st.ListPermissionProfiles(context.Background(), nil)
	must(t, err)
	out := []*models.PermissionProfile{}
	for _, p := range all {
		if scope == nil || p.TenantID == nil || *p.TenantID == *scope {
			out = append(out, p)
		}
	}
	// Preserve the database's name collation; compare UUIDs only within each
	// equal-name group in the existing ORDER BY name observer.
	for first := 0; first < len(out); {
		last := first + 1
		for last < len(out) && out[last].Name == out[first].Name {
			last++
		}
		group := out[first:last]
		sort.Slice(group, func(i, j int) bool { return group[i].ID.String() < group[j].ID.String() })
		first = last
	}
	return out
}

func r5VisibilityRead(t *testing.T, f *companyFixture, a authz.Actor, selected, visible *uuid.UUID) {
	t.Helper()
	before := r5PermissionAuthorityState(t, f)
	got, err := permissions.New(f.st).ListProfiles(context.Background(), a, selected)
	must(t, err)
	if !reflect.DeepEqual(got, r5VisibilityExpected(t, f, visible)) {
		t.Fatal("qualified list fields/order differ from its visible scope")
	}
	if r5PermissionAuthorityState(t, f) != before {
		t.Fatal("profile read changed durable state/audit/outbox/allocator")
	}
}

func r5VisibilityDenied(t *testing.T, f *companyFixture, a authz.Actor, selected *uuid.UUID, kind app.ErrorKind) {
	t.Helper()
	before := r5PermissionAuthorityState(t, f)
	got, err := permissions.New(f.st).ListProfiles(context.Background(), a, selected)
	r5EditorRequireKind(t, err, kind)
	if got != nil {
		t.Fatal("denied profile list returned fields")
	}
	if r5PermissionAuthorityState(t, f) != before {
		t.Fatal("denied profile read produced durable effects")
	}
}

func TestR5PermissionProfileVisibilityScope(t *testing.T) {
	f := r5PermissionAuthoritySeed(t)
	ctx := context.Background()
	root := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
	super := r5PermissionAuthorityActor(root, f.tenant.ID)
	other := &models.Tenant{Name: "Visibility other", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, other))
	foreignUser := r5PermissionAuthorityUser(t, f, other.ID, models.RoleUser)
	foreignAdmin := r5PermissionAuthorityUser(t, f, other.ID, models.RoleAdmin)
	name := "Visibility " + uuid.NewString()
	global := r5VisibilityProfile(t, f, nil, name)
	r5VisibilityProfile(t, f, &f.tenant.ID, name)
	r5VisibilityProfile(t, f, &other.ID, name)
	// Actual global reference exists exclusively in B. Neither that reference
	// nor a producer audience limits A's globally reusable visible profile.
	selected := super
	selected.TenantID = other.ID
	snap, err := f.st.GetPermissionEditorSnapshot(ctx, selected, foreignUser.ID)
	must(t, err)
	_, err = f.st.AssignPermissionEditor(ctx, selected, foreignUser.ID, company.PermissionAssignmentCommand{ExpectedRevision: snap.Revision, ProfileID: &global.ID, ProfileRevision: &global.Revision})
	must(t, err)
	admin := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
	for _, tc := range []struct {
		name              string
		a                 authz.Actor
		selected, visible *uuid.UUID
	}{
		{"super-default", super, nil, nil},
		{"super-selected-a", super, &f.tenant.ID, nil},
		{"super-selected-b", super, &other.ID, nil},
		{"tenant-admin-a", admin, &f.tenant.ID, &f.tenant.ID},
		{"tenant-admin-b", r5PermissionAuthorityActor(foreignAdmin, other.ID), &other.ID, &other.ID},
	} {
		t.Run(tc.name, func(t *testing.T) { r5VisibilityRead(t, f, tc.a, tc.selected, tc.visible) })
	}
	t.Run("cross-tenant-admin", func(t *testing.T) { r5VisibilityDenied(t, f, admin, &other.ID, app.KindForbidden) })
	t.Run("admin-no-selection", func(t *testing.T) { r5VisibilityDenied(t, f, admin, nil, app.KindForbidden) })
	t.Run("reader-global-reference", func(t *testing.T) {
		r5VisibilityDenied(t, f, r5PermissionAuthorityActor(foreignUser, other.ID), &other.ID, app.KindForbidden)
	})
	t.Run("missing-selected-company", func(t *testing.T) { id := uuid.New(); r5VisibilityDenied(t, f, super, &id, app.KindNotFound) })
	t.Run("zero-selected-company", func(t *testing.T) { id := uuid.Nil; r5VisibilityDenied(t, f, super, &id, app.KindForbidden) })
	for _, owner := range []*uuid.UUID{nil, &root.ID, &f.admin.ID} {
		a := authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: f.tenant.ID, OwnerUserID: owner, IsSuperAdmin: true, IsAdmin: true}
		t.Run("api-key", func(t *testing.T) {
			r5VisibilityDenied(t, f, a, &f.tenant.ID, app.KindForbidden)
			got, err := f.st.ListVisiblePermissionProfiles(ctx, a, &f.tenant.ID)
			r5EditorRequireKind(t, err, app.KindForbidden)
			if got != nil {
				t.Fatal("direct key returned profiles")
			}
		})
	}
}

func TestR5PermissionProfileVisibilityCurrentRole(t *testing.T) {
	f := r5PermissionAuthoritySeed(t)
	root := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
	super := r5PermissionAuthorityActor(root, f.tenant.ID)
	// Flags/Role supplied by a caller do not alter the current stored role.
	forged := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
	forged.IsSuperAdmin, forged.IsAdmin, forged.Role = true, false, models.RoleSuperAdmin
	r5VisibilityRead(t, f, forged, &f.tenant.ID, &f.tenant.ID)
	stale := super
	stale.IsSuperAdmin, stale.IsAdmin, stale.Role = false, false, models.RoleUser
	r5VisibilityRead(t, f, stale, &f.tenant.ID, nil)
	old := r5PermissionAuthorityActor(f.employee, f.tenant.ID)
	adminRole := models.RoleAdmin
	promoted, err := f.st.UpdateUserGuarded(context.Background(), super, f.tenant.ID, f.employee.ID, models.UserAdminPatch{Role: &adminRole})
	must(t, err)
	r5VisibilityDenied(t, f, old, &f.tenant.ID, app.KindForbidden)
	fresh := r5PermissionAuthorityActor(promoted, f.tenant.ID)
	fresh.IsAdmin, fresh.Role = false, models.RoleUser
	r5VisibilityRead(t, f, fresh, &f.tenant.ID, &f.tenant.ID)
	staleAdmin := r5PermissionAuthorityActor(promoted, f.tenant.ID)
	userRole := models.RoleUser
	demoted, err := f.st.UpdateUserGuarded(context.Background(), super, f.tenant.ID, promoted.ID, models.UserAdminPatch{Role: &userRole})
	must(t, err)
	r5VisibilityDenied(t, f, staleAdmin, &f.tenant.ID, app.KindForbidden)
	// A trusted actor without JWT epoch still cannot keep its old role.
	trusted := staleAdmin
	trusted.SessionVersion = nil
	r5VisibilityDenied(t, f, trusted, &f.tenant.ID, app.KindForbidden)
	freshUser := r5PermissionAuthorityActor(demoted, f.tenant.ID)
	freshUser.IsSuperAdmin = true
	r5VisibilityDenied(t, f, freshUser, &f.tenant.ID, app.KindForbidden)

	other := &models.Tenant{Name: "Current-role foreign", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(context.Background(), other))
	platformUser := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
	platform := r5PermissionAuthorityActor(platformUser, f.tenant.ID)
	r5VisibilityRead(t, f, platform, &other.ID, nil)
	currentAdmin, err := f.st.UpdateUserGuarded(context.Background(), super, f.tenant.ID, platformUser.ID, models.UserAdminPatch{Role: &adminRole})
	must(t, err)
	r5VisibilityDenied(t, f, platform, &other.ID, app.KindForbidden)
	// Even with a current epoch, stale platform flags do not retain platform
	// scope after the real role switch. Own selection narrows; foreign denies.
	current := r5PermissionAuthorityActor(currentAdmin, f.tenant.ID)
	current.IsSuperAdmin, current.Role = true, models.RoleSuperAdmin
	r5VisibilityRead(t, f, current, &f.tenant.ID, &f.tenant.ID)
	r5VisibilityDenied(t, f, current, &other.ID, app.KindForbidden)
}

func TestR5PermissionProfileVisibilityCredentialChanges(t *testing.T) {
	for _, change := range []string{"freeze-reactivate", "password-epoch", "deleted"} {
		t.Run(change, func(t *testing.T) {
			f := r5PermissionAuthoritySeed(t)
			root := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
			super := r5PermissionAuthorityActor(root, f.tenant.ID)
			old := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
			switch change {
			case "freeze-reactivate":
				inactive := false
				_, err := f.st.UpdateUserGuarded(context.Background(), super, f.tenant.ID, f.admin.ID, models.UserAdminPatch{IsActive: &inactive})
				must(t, err)
				r5VisibilityDenied(t, f, old, &f.tenant.ID, app.KindForbidden)
				active := true
				_, err = f.st.UpdateUserGuarded(context.Background(), super, f.tenant.ID, f.admin.ID, models.UserAdminPatch{IsActive: &active})
				must(t, err)
			case "password-epoch":
				must(t, f.st.UpdateUserPassword(context.Background(), f.admin.ID, "visibility-test-only-password"))
			case "deleted":
				must(t, f.st.DeleteUserGuarded(context.Background(), super, f.tenant.ID, f.admin.ID))
			}
			r5VisibilityDenied(t, f, old, &f.tenant.ID, app.KindForbidden)
			if change != "deleted" {
				current, err := f.st.GetUser(context.Background(), f.admin.ID)
				must(t, err)
				r5VisibilityRead(t, f, r5PermissionAuthorityActor(current, f.tenant.ID), &f.tenant.ID, &f.tenant.ID)
			}
			r5VisibilityRead(t, f, super, &f.tenant.ID, nil)
		})
	}
}

// SQL is exclusively a lock gate and passive observer. The actor mutation is
// the production UpdateUserGuarded command, paused at its required audit after
// its user update. No SQL manufactures an authority change for the test.
func r5VisibilityWaitPID(t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, done <-chan error) uint32 {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("operation escaped observed lock edge: %v", err)
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
			t.Fatal("exact actor/read PostgreSQL wait edge missing")
		case <-ticker.C:
		}
	}
}

func TestR5PermissionProfileVisibilityLockWait(t *testing.T) {
	for _, change := range []string{"demote", "freeze"} {
		t.Run(change, func(t *testing.T) {
			f := r5PermissionAuthoritySeed(t)
			root := r5PermissionAuthorityUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
			super := r5PermissionAuthorityActor(root, f.tenant.ID)
			old := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			_, err = gate.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			must(t, err)
			changeDone := make(chan error, 1)
			go func() {
				patch := models.UserAdminPatch{}
				if change == "demote" {
					role := models.RoleUser
					patch.Role = &role
				} else {
					active := false
					patch.IsActive = &active
				}
				_, err := f.st.UpdateUserGuarded(ctx, super, f.tenant.ID, f.admin.ID, patch)
				changeDone <- err
			}()
			writerPID := r5VisibilityWaitPID(t, f, ctx, gate.Conn().PgConn().PID(), changeDone)
			readDone := make(chan error, 1)
			go func() {
				out, err := permissions.New(f.st).ListProfiles(ctx, old, &f.tenant.ID)
				if out != nil {
					err = errors.New("waited stale actor returned profile fields")
				}
				readDone <- err
			}()
			r5VisibilityWaitPID(t, f, ctx, writerPID, readDone)
			must(t, gate.Commit(ctx))
			select {
			case err := <-changeDone:
				must(t, err)
			case <-ctx.Done():
				t.Fatal("production actor update did not finish")
			}
			select {
			case err := <-readDone:
				r5EditorRequireKind(t, err, app.KindForbidden)
			case <-ctx.Done():
				t.Fatal("waited list did not finish")
			}
			r5VisibilityRead(t, f, super, &f.tenant.ID, nil)
		})
	}
}
