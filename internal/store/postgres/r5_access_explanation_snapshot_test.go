package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// All observations use a new testpg database and production commands. A waiter
// or actual completion is observed; a sleep is never evidence of lock ordering.
func TestR5AccessExplanationWaitsForTargetWrites(t *testing.T) {
	for _, kind := range []string{"frozen", "profile-assignment", "override-insert", "override-clear", "role-change"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			r5SnapshotProfile(t, f, kind != "override-clear" && kind != "role-change", false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if kind == "override-clear" {
				yes := true
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &yes}))
			}
			if kind == "role-change" {
				_, e := f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.employee.ID)
				must(t, e)
			}
			denied := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "Denied " + uuid.NewString(), CanSend: false}
			if kind == "profile-assignment" {
				must(t, f.st.CreatePermissionProfile(ctx, denied))
			}
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			// This is the stable user fence used by the existing override writer.
			_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, f.employee.ID)
			must(t, e)
			switch kind {
			case "frozen":
				_, e = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
			case "profile-assignment":
				_, e = hold.Exec(ctx, `UPDATE users SET permission_profile_id=$2 WHERE id=$1`, f.employee.ID, denied.ID)
			case "override-insert":
				_, e = hold.Exec(ctx, `INSERT INTO user_permission_overrides(id,user_id,can_send) VALUES($1,$2,false)`, uuid.New(), f.employee.ID)
			case "override-clear":
				_, e = hold.Exec(ctx, `DELETE FROM user_permission_overrides WHERE user_id=$1`, f.employee.ID)
			case "role-change":
				_, e = hold.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.employee.ID)
			}
			must(t, e)
			var got *company.AccessExplanation
			done := make(chan error, 1)
			go func() {
				var err error
				got, err = f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
				done <- err
			}()
			waited, result := r5SnapshotWaitOrDone(t, f, ctx, hold.Conn().PgConn().PID(), done)
			must(t, hold.Commit(ctx))
			if waited {
				result = r5ConcurrentResult(t, ctx, done)
			}
			must(t, result)
			if !waited || got == nil || got.CanSend || got.Active != (kind != "frozen") {
				t.Fatalf("explanation crossed an in-progress %s: waited=%v result=%+v", kind, waited, got)
			}
			if kind == "frozen" && (got.CanRead || got.CanOrganize || !slices.Contains(got.Reasons, "inactive")) {
				t.Fatal("inactive target retained effective content rights")
			}
			if kind != "frozen" && (!got.CanRead || !slices.Contains(got.Reasons, "profile_send_disabled")) {
				t.Fatal("explanation did not use the new target permission")
			}
		})
	}
}

func TestR5AccessExplanationRejectsBusyTargetProfile(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "tenant"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, global)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `UPDATE permission_profiles SET can_send=false WHERE id=$1`, p.ID)
			must(t, e)
			v, e := f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
			err, ok := app.As(e)
			if !ok || err.Kind != app.KindConflict || v != nil {
				t.Fatalf("busy target profile produced a successful/partial explanation: value=%+v err=%v", v, e)
			}
			must(t, hold.Commit(ctx))
			v, e = f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
			must(t, e)
			if v.CanSend {
				t.Fatal("committed profile restriction was ignored")
			}
		})
	}
}

func TestR5AccessExplanationOrdersPermissionChanges(t *testing.T) {
	for _, kind := range []string{"profile-update", "profile-delete", "override-insert", "override-update", "override-clear"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, kind != "override-clear", false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if kind == "override-update" || kind == "override-clear" {
				yes := true
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &yes}))
			}
			must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true, CanSend: true}))
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			// Block the mailbox-grant query after the target snapshot is taken.
			// The baseline reads mailbox grants first, before protecting the target.
			_, e = hold.Exec(ctx, `LOCK TABLE mailbox_grants IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			var got *company.AccessExplanation
			done := make(chan error, 1)
			go func() {
				var err error
				got, err = f.st.ExplainMailboxAccess(ctx, f.a, f.shared.ID, f.employee.ID)
				done <- err
			}()
			reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailbox_grants")
			changed := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "profile-update":
					p.CanSend = false
					err = f.st.UpdatePermissionProfile(ctx, p)
				case "profile-delete":
					err = f.st.DeletePermissionProfile(ctx, p.ID, p.TenantID)
				case "override-clear":
					err = f.st.DeleteUserPermissionOverride(ctx, f.employee.ID)
				default:
					no := false
					err = f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &no})
				}
				changed <- err
			}()
			ordered, changeErr := r5SnapshotWaitOrDone(t, f, ctx, reader, changed)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
			if ordered {
				changeErr = r5ConcurrentResult(t, ctx, changed)
			}
			must(t, changeErr)
			if !ordered || got == nil || !got.CanSend || got.Source != "grant" {
				t.Fatalf("permission source was not protected before mailbox wait: ordered=%v value=%+v", ordered, got)
			}
			fresh, e := f.st.ExplainMailboxAccess(ctx, f.a, f.shared.ID, f.employee.ID)
			must(t, e)
			if fresh.CanSend || !slices.Contains(fresh.Reasons, "profile_send_disabled") {
				t.Fatal("next explanation ignored completed permission change")
			}
		})
	}
}

// Before: active user with send denied. After one atomic writer: inactive user
// with a permissive override. Neither committed state permits sending. The old
// explanation can combine the first Active flag with the second permission.
func TestR5AccessExplanationNeverCombinesTargetGenerations(t *testing.T) {
	f := seedCompany(t)
	r5SnapshotProfile(t, f, false, false)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE user_permission_overrides IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	var got *company.AccessExplanation
	done := make(chan error, 1)
	go func() {
		var err error
		got, err = f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
		done <- err
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "user_permission_overrides")
	_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, f.employee.ID)
	if e != nil {
		// The candidate protects the target before its permission query. Abort
		// the competing writer without waiting in the inverse test-only order.
		r5RequireLockConflict(t, e)
		must(t, hold.Rollback(ctx))
	} else {
		_, e = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
		must(t, e)
		_, e = hold.Exec(ctx, `INSERT INTO user_permission_overrides(id,user_id,can_send) VALUES($1,$2,true)`, uuid.New(), f.employee.ID)
		must(t, e)
		must(t, hold.Commit(ctx))
	}
	r5AwaitOperation(t, ctx, done)
	if got == nil || got.CanSend {
		t.Fatalf("explanation combined target generations into send authority that never existed: %+v", got)
	}
}

func TestR5AccessExplanationFailureReturnsNoProjection(t *testing.T) {
	for _, kind := range []string{"missing-mailbox", "missing-user", "foreign-user", "foreign-mailbox", "employee", "frozen-actor", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			actor := f.a
			mailbox, user := f.personal.ID, f.employee.ID
			switch kind {
			case "missing-mailbox":
				mailbox = uuid.New()
			case "missing-user":
				user = uuid.New()
			case "foreign-user", "foreign-mailbox":
				tenant := &models.Tenant{Name: "Other explanation company", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, tenant))
				if kind == "foreign-user" {
					u := &models.User{TenantID: tenant.ID, Email: "foreign@explanation.test", Role: models.RoleUser, IsActive: true, PasswordHash: "fixture"}
					must(t, f.st.CreateUser(ctx, u))
					user = u.ID
				} else {
					zone := &models.DomainZone{TenantID: tenant.ID, Domain: "foreign-explanation.test", IsVerified: true, MXVerified: true}
					must(t, f.st.CreateZone(ctx, zone))
					mb := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "foreign", FullAddress: "foreign@foreign-explanation.test", AccessMode: models.AccessAPIKey}
					must(t, f.st.CreateMailbox(ctx, mb))
					mailbox = mb.ID
				}
			case "employee":
				actor = f.u
			case "frozen-actor":
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
				must(t, e)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			v, e := f.st.ExplainMailboxAccess(ctx, actor, mailbox, user)
			if e == nil || v != nil {
				t.Fatalf("failed explanation returned a partial/success value: %v %v", v, e)
			}
		})
	}
}

func TestR5AccessExplanationCancellationReleasesSnapshot(t *testing.T) {
	f := seedCompany(t)
	p := r5SnapshotProfile(t, f, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE mailbox_grants IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	var got *company.AccessExplanation
	done := make(chan error, 1)
	go func() {
		var err error
		got, err = f.st.ExplainMailboxAccess(runCtx, f.a, f.shared.ID, f.employee.ID)
		done <- err
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailbox_grants")
	stop()
	e = r5ConcurrentResult(t, ctx, done)
	if !errors.Is(e, context.Canceled) || got != nil {
		t.Fatalf("cancelled explanation returned value=%v err=%v", got, e)
	}
	must(t, hold.Rollback(ctx))
	probe, e := f.pool.Begin(ctx)
	must(t, e)
	defer probe.Rollback(context.Background())
	// A cancelled pgx call can return before server-side connection teardown.
	// Reacquire under the original deadline; immediate NOWAIT races that cleanup
	// rather than proving a leak. No timeout extension or arbitrary sleep.
	for _, q := range []struct {
		sql string
		id  uuid.UUID
	}{{`SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID}, {`SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID}, {`SELECT id FROM permission_profiles WHERE id=$1 FOR UPDATE`, p.ID}} {
		_, e = probe.Exec(ctx, q.sql, q.id)
		must(t, e)
	}
	must(t, probe.Rollback(ctx))
	_, e = f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
	must(t, e)
}

func TestR5AccessExplanationPreservesCanonicalDecisions(t *testing.T) {
	for _, kind := range []string{"owner", "grant", "empty-grant", "none", "admin", "inactive", "zone-restricted", "template-only"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			r5SnapshotProfile(t, f, true, false)
			mailbox := f.personal
			target := f.u
			source := "owner"
			if kind == "grant" || kind == "empty-grant" || kind == "none" || kind == "template-only" {
				mailbox = f.shared
				source = "none"
			}
			if kind == "grant" || kind == "empty-grant" || kind == "template-only" {
				g := &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mailbox.ID, UserID: target.ID, CanRead: kind != "empty-grant", CanSend: kind != "empty-grant", TemplateOnly: kind == "template-only"}
				must(t, f.st.SetMailboxGrant(ctx, g))
				source = "grant"
			}
			if kind == "admin" {
				target = f.a
				source = "none"
			}
			if kind == "inactive" {
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, target.ID)
				must(t, e)
			}
			if kind == "zone-restricted" {
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: target.ID, AllowedZoneIDs: []uuid.UUID{uuid.New()}}))
			}
			got, e := f.st.ExplainMailboxAccess(ctx, f.a, mailbox.ID, target.ID)
			must(t, e)
			if got.Source != source {
				t.Fatalf("wrong provenance: %+v", got)
			}
			if kind == "inactive" {
				if got.Active || got.CanRead || got.CanSend || got.CanOrganize {
					t.Fatal("inactive target has rights")
				}
				return
			}
			if !target.IsTenantAdmin() {
				target.Permission, e = f.st.EffectivePermission(ctx, target.ID)
				must(t, e)
			}
			mb, e := f.st.GetMailbox(ctx, mailbox.ID)
			must(t, e)
			grant, e := f.st.GetMailboxGrant(ctx, f.tenant.ID, mailbox.ID, target.ID)
			must(t, e)
			want := authz.EvaluateMailboxAccess(target, mb, grant)
			if got.CanRead != want.CanRead || got.CanSend != want.CanSend || got.CanOrganize != want.CanOrganize || got.TemplateOnly != want.TemplateOnly {
				t.Fatalf("explanation diverged from canonical decision: %+v %+v", got, want)
			}
		})
	}
}
