package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

type r5OffboardingQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func r5OffboardingSeed(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("offboarding lifecycle runtime requires owned TABMAIL_TEST_DB_DSN; no skip")
	}
	return seedCompany(t)
}

func r5OffboardingActor(t *testing.T, f *companyFixture) authz.Actor {
	t.Helper()
	u, err := f.st.GetUser(context.Background(), f.admin.ID)
	must(t, err)
	v := u.SessionVersion
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, IsAdmin: true, SessionVersion: &v}
}

func r5OffboardingState(t *testing.T, q r5OffboardingQuery) string {
	t.Helper()
	var state string
	must(t, q.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM users u),
 'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM permission_profiles p),
 'plans',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM employee_offboarding_plans p),
 'mailboxes',(SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM mailboxes m),
 'drafts',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM mail_drafts d),
 'attachments',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM mail_attachments f),
 'keys',(SELECT jsonb_agg(to_jsonb(k) ORDER BY k.id) FROM tenant_api_keys k),
 'refresh',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM refresh_tokens r),
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.mailbox_id,g.user_id) FROM mailbox_grants g),
 'template_grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.template_id,g.mailbox_id,g.user_id) FROM mail_template_grants g),
 'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY j.id) FROM outbound_jobs j),
 'recipients',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.job_id,r.address) FROM outbound_recipients r),
 'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM outbox_events e))::text`).Scan(&state))
	return state
}

func r5OffboardingPreview(t *testing.T, f *companyFixture, actor authz.Actor) *company.OffboardingPlan {
	t.Helper()
	p, err := f.st.PreviewOffboarding(context.Background(), actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Current lifecycle disposition proof")
	must(t, err)
	if p == nil || p.State != "preview" || p.ID == uuid.Nil {
		t.Fatal("formal lifecycle preview missing")
	}
	return p
}

func r5OffboardingReject(t *testing.T, p *company.OffboardingPlan, err error, kind app.ErrorKind) {
	t.Helper()
	if p != nil {
		t.Fatal("rejected offboarding returned a plan or historical disposition receipt")
	}
	if authz.IsAuthzError(err) {
		err = app.FromAuthz(err)
	}
	e, ok := app.As(err)
	if !ok || e.Kind != kind {
		t.Fatalf("offboarding rejection=%v expected=%s", err, kind)
	}
}

func r5OffboardingPermissionABA(t *testing.T, f *companyFixture, actor authz.Actor, user uuid.UUID) {
	t.Helper()
	before, err := f.st.GetPermissionEditorSnapshot(context.Background(), actor, user)
	must(t, err)
	changed, err := f.st.PatchPermissionEditor(context.Background(), actor, user, company.PermissionEditorCommand{ExpectedRevision: before.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":0}`)})
	must(t, err)
	_, err = f.st.PatchPermissionEditor(context.Background(), actor, user, company.PermissionEditorCommand{ExpectedRevision: changed.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":null}`)})
	must(t, err)
}

func TestR5OffboardingLifecycleFrozenTargetAndQualifiedReplay(t *testing.T) {
	f := r5OffboardingSeed(t)
	a := r5OffboardingActor(t, f)
	draft, err := f.st.SaveMailDraft(context.Background(), f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "sealed private lifecycle draft"}})
	must(t, err)
	inactive := false
	_, err = f.st.UpdateUserGuarded(context.Background(), a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &inactive})
	must(t, err)
	legacyBefore := r5OffboardingState(t, f.pool)
	err = f.st.OffboardEmployee(context.Background(), a, f.employee.ID, f.other.ID, "Legacy one-step must retain inactive guard")
	r5OffboardingReject(t, nil, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != legacyBefore {
		t.Fatal("legacy wrapper disposed an already inactive target")
	}
	p := r5OffboardingPreview(t, f, a)
	var active bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
	if active {
		t.Fatal("frozen preview reactivated employee")
	}
	done, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, p.ID)
	must(t, err)
	var sealed bool
	var author, owner uuid.UUID
	must(t, f.pool.QueryRow(context.Background(), `SELECT sealed_at IS NOT NULL,user_id FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&sealed, &author))
	must(t, f.pool.QueryRow(context.Background(), `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
	if !sealed || author != f.employee.ID || owner != f.other.ID || done.ExecutedAt == nil {
		t.Fatal("frozen disposition lost sealed privacy, ownership or execution receipt")
	}
	// A completed receipt remains replayable after the preview deadline.
	_, err = f.pool.Exec(context.Background(), `UPDATE employee_offboarding_plans SET expires_at=clock_timestamp() WHERE id=$1`, p.ID)
	must(t, err)
	before := r5OffboardingState(t, f.pool)
	again, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, p.ID)
	must(t, err)
	if again.ExecutedAt == nil || !again.ExecutedAt.Equal(*done.ExecutedAt) || r5OffboardingState(t, f.pool) != before {
		t.Fatal("qualified replay repeated session/transfer/audit/outbox effects")
	}
	p, err = f.st.PreviewOffboarding(context.Background(), a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Do not repeat completed exact epoch")
	r5OffboardingReject(t, p, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("second same-epoch preview produced effects")
	}
}

func TestR5OffboardingLifecycleOldCompletionEpochDenied(t *testing.T) {
	for _, change := range []string{"actor-demote", "actor-freeze", "actor-session", "target-reactivate", "target-session", "target-role", "target-permission-aba", "successor-freeze", "successor-session", "successor-permission-aba", "legacy-unverified", "foreign-target"} {
		t.Run(change, func(t *testing.T) {
			f := r5OffboardingSeed(t)
			a := r5OffboardingActor(t, f)
			p := r5OffboardingPreview(t, f, a)
			_, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, p.ID)
			must(t, err)
			want, target := app.KindConflict, f.employee.ID
			switch change {
			case "actor-demote", "actor-freeze":
				u, err := f.st.GetUser(context.Background(), f.admin.ID)
				must(t, err)
				if change == "actor-demote" {
					u.Role = models.RoleUser
				} else {
					u.IsActive = false
				}
				must(t, f.st.UpdateUser(context.Background(), u))
				want = app.KindForbidden
			case "actor-session":
				must(t, f.st.UpdateUserPassword(context.Background(), f.admin.ID, "test-only-new-controller-session"))
				want = app.KindForbidden
			case "target-reactivate":
				active := true
				_, err := f.st.UpdateUserGuarded(context.Background(), a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &active})
				must(t, err)
			case "target-session":
				must(t, f.st.UpdateUserPassword(context.Background(), f.employee.ID, "test-only-new-target-session"))
			case "target-role":
				u, err := f.st.GetUser(context.Background(), f.employee.ID)
				must(t, err)
				u.Role = models.RoleAdmin
				must(t, f.st.UpdateUser(context.Background(), u))
				want = app.KindForbidden
			case "target-permission-aba":
				r5OffboardingPermissionABA(t, f, a, f.employee.ID)
			case "successor-freeze":
				inactive := false
				_, err := f.st.UpdateUserGuarded(context.Background(), a, f.tenant.ID, f.other.ID, models.UserAdminPatch{IsActive: &inactive})
				must(t, err)
				want = app.KindBadRequest
			case "successor-session":
				must(t, f.st.UpdateUserPassword(context.Background(), f.other.ID, "test-only-new-successor-session"))
			case "successor-permission-aba":
				r5OffboardingPermissionABA(t, f, a, f.other.ID)
			case "legacy-unverified":
				_, err := f.pool.Exec(context.Background(), `UPDATE employee_offboarding_plans SET fingerprint=$2 WHERE id=$1`, p.ID, company.Hash("unverified legacy preview fingerprint"))
				must(t, err)
			case "foreign-target":
				target = uuid.New()
				want = app.KindNotFound
			}
			before := r5OffboardingState(t, f.pool)
			got, err := f.st.ExecuteOffboarding(context.Background(), a, target, p.ID)
			r5OffboardingReject(t, got, err, want)
			if r5OffboardingState(t, f.pool) != before {
				t.Fatal("rejected old completion epoch changed lifecycle effects")
			}
		})
	}
}

func TestR5OffboardingLifecycleFreshReentryAndSuccessorEpoch(t *testing.T) {
	f := r5OffboardingSeed(t)
	a := r5OffboardingActor(t, f)
	old := r5OffboardingPreview(t, f, a)
	second := r5OffboardingPreview(t, f, a)
	_, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, old.ID)
	must(t, err)
	before := r5OffboardingState(t, f.pool)
	got, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, second.ID)
	r5OffboardingReject(t, got, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("another old plan repeated disposition")
	}
	active := true
	_, err = f.st.UpdateUserGuarded(context.Background(), a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &active})
	must(t, err)
	before = r5OffboardingState(t, f.pool)
	got, err = f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, old.ID)
	r5OffboardingReject(t, got, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("old plan disabled reentered identity")
	}
	fresh := r5OffboardingPreview(t, f, a) // Historical execution is not a permanent reentry ban.
	r5OffboardingPermissionABA(t, f, a, f.other.ID)
	before = r5OffboardingState(t, f.pool)
	got, err = f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, fresh.ID)
	r5OffboardingReject(t, got, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("successor permission ABA was not fenced")
	}
	fresh = r5OffboardingPreview(t, f, a)
	_, err = f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, fresh.ID)
	must(t, err)
}

func r5OffboardingAssignProfile(t *testing.T, f *companyFixture, a authz.Actor, user uuid.UUID, p *models.PermissionProfile) {
	t.Helper()
	snapshot, err := f.st.GetPermissionEditorSnapshot(context.Background(), a, user)
	must(t, err)
	command := company.PermissionAssignmentCommand{ExpectedRevision: snapshot.Revision}
	if p != nil {
		command.ProfileID, command.ProfileRevision = &p.ID, &p.Revision
	}
	_, err = f.st.AssignPermissionEditor(context.Background(), a, user, command)
	must(t, err)
}

func r5OffboardingProfile(t *testing.T, f *companyFixture) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "Lifecycle compound profile " + uuid.NewString(), CanSend: true, DailySendQuota: 31, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	p, err := f.st.GetPermissionProfile(context.Background(), p.ID)
	must(t, err)
	if p == nil || p.Revision == "" {
		t.Fatal("actual profile revision missing")
	}
	return p
}

func TestR5OffboardingLifecycleAssignedProfileContentABA(t *testing.T) {
	for _, phase := range []string{"preview", "replay"} {
		for _, subject := range []string{"target", "successor"} {
			t.Run(phase+"/"+subject, func(t *testing.T) {
				f := r5OffboardingSeed(t)
				a := r5OffboardingActor(t, f)
				user := f.employee.ID
				if subject == "successor" {
					user = f.other.ID
				}
				profile := r5OffboardingProfile(t, f)
				r5OffboardingAssignProfile(t, f, a, user, profile)
				plan := r5OffboardingPreview(t, f, a)
				if phase == "replay" {
					_, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
					must(t, err)
				}
				observed, err := f.st.GetPermissionEditorSnapshot(context.Background(), a, user)
				must(t, err)
				desired := *profile
				desired.CanSend = !profile.CanSend
				desired.Description = "Profile content changed without member revision consumption"
				changed, err := f.st.UpdatePermissionProfileCAS(context.Background(), a, &desired, profile.Revision)
				must(t, err)
				restored, err := f.st.UpdatePermissionProfileCAS(context.Background(), a, profile, changed.Revision)
				must(t, err)
				current, err := f.st.GetPermissionEditorSnapshot(context.Background(), a, user)
				must(t, err)
				if current.Revision.UserRevision != observed.Revision.UserRevision || current.Revision.ProfileID == nil || *current.Revision.ProfileID != profile.ID || current.Revision.ProfileRevision == nil || *current.Revision.ProfileRevision != restored.Revision || restored.Revision == profile.Revision || restored.Description != profile.Description || restored.CanSend != profile.CanSend {
					t.Fatal("fixture did not establish real profile-content ABA with unchanged member revision")
				}
				before := r5OffboardingState(t, f.pool)
				got, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
				r5OffboardingReject(t, got, err, app.KindConflict)
				if r5OffboardingState(t, f.pool) != before {
					t.Fatal("profile-content ABA executed/replayed old lifecycle effects")
				}
			})
		}
	}
}

func TestR5OffboardingLifecycleSharedAndNullableProfileReceipt(t *testing.T) {
	for _, kind := range []string{"shared-unchanged-profile", "explicit-null-profile-pair"} {
		t.Run(kind, func(t *testing.T) {
			f := r5OffboardingSeed(t)
			a := r5OffboardingActor(t, f)
			var profile *models.PermissionProfile
			if kind == "shared-unchanged-profile" {
				profile = r5OffboardingProfile(t, f)
			}
			for _, user := range []uuid.UUID{f.employee.ID, f.other.ID} {
				r5OffboardingAssignProfile(t, f, a, user, profile)
			}
			plan := r5OffboardingPreview(t, f, a)
			done, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
			must(t, err)
			before := r5OffboardingState(t, f.pool)
			again, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
			must(t, err)
			if done.ExecutedAt == nil || again.ExecutedAt == nil || !done.ExecutedAt.Equal(*again.ExecutedAt) || r5OffboardingState(t, f.pool) != before {
				t.Fatal("unchanged shared/null compound profile epoch failed qualified no-effect replay")
			}
		})
	}
}

func TestR5OffboardingLifecycleProfileVersionFence(t *testing.T) {
	for _, phase := range []string{"preview", "replay"} {
		for _, change := range []string{"busy-profile", "target-recreated-profile", "successor-recreated-profile"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				f := r5OffboardingSeed(t)
				a := r5OffboardingActor(t, f)
				user := f.employee.ID
				if change == "successor-recreated-profile" {
					user = f.other.ID
				}
				profile := r5OffboardingProfile(t, f)
				r5OffboardingAssignProfile(t, f, a, user, profile)
				plan := r5OffboardingPreview(t, f, a)
				if phase == "replay" {
					_, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
					must(t, err)
				}
				if change == "busy-profile" {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					gate, err := f.pool.Begin(ctx)
					must(t, err)
					defer gate.Rollback(context.Background())
					_, err = gate.Exec(ctx, `SELECT id FROM permission_profiles WHERE id=$1 FOR UPDATE`, profile.ID)
					must(t, err)
					before := r5OffboardingState(t, f.pool)
					got, err := f.st.ExecuteOffboarding(ctx, a, f.employee.ID, plan.ID)
					r5OffboardingReject(t, got, err, app.KindConflict)
					if r5OffboardingState(t, f.pool) != before {
						t.Fatal("busy profile admission leaked effects rather than NOWAIT conflict")
					}
					must(t, gate.Rollback(ctx))
					_, err = f.st.ExecuteOffboarding(ctx, a, f.employee.ID, plan.ID)
					must(t, err)
					return
				}
				preview, err := f.st.GetPermissionProfileDeletionPreview(context.Background(), a, profile.ID)
				must(t, err)
				must(t, f.st.DeletePermissionProfileCAS(context.Background(), a, profile.ID, profile.Revision, preview.Members))
				// Trusted fixture explicitly reuses the UUID, through the real
				// PgStore INSERT, never by setting a synthetic permission revision.
				replacement := *profile
				must(t, f.st.CreatePermissionProfile(context.Background(), &replacement))
				persisted, err := f.st.GetPermissionProfile(context.Background(), profile.ID)
				must(t, err)
				if persisted == nil || persisted.Revision == profile.Revision {
					t.Fatal("recreated profile UUID reused durable noncycling revision")
				}
				r5OffboardingAssignProfile(t, f, a, user, persisted)
				before := r5OffboardingState(t, f.pool)
				got, err := f.st.ExecuteOffboarding(context.Background(), a, f.employee.ID, plan.ID)
				r5OffboardingReject(t, got, err, app.KindConflict)
				if r5OffboardingState(t, f.pool) != before {
					t.Fatal("recreated profile alias revived an old lifecycle plan")
				}
			})
		}
	}
}

func r5OffboardingWait(t *testing.T, pool *pgxpool.Pool, ctx context.Context, blocker uint32) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var pid uint32
		err := pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("no actual PostgreSQL offboarding wait edge")
		case <-tick.C:
		}
	}
}

func TestR5OffboardingLifecycleAuditWaitExpiryRollsBack(t *testing.T) {
	f := r5OffboardingSeed(t)
	a := r5OffboardingActor(t, f)
	_, err := f.st.SaveMailDraft(context.Background(), f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "private rollback witness"}})
	must(t, err)
	p := r5OffboardingPreview(t, f, a)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	gate, err := f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(context.Background())
	_, err = gate.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE employee_offboarding_plans SET expires_at=clock_timestamp()+interval '500 milliseconds' WHERE id=$1`, p.ID)
	must(t, err)
	before := r5OffboardingState(t, f.pool)
	type result struct {
		plan *company.OffboardingPlan
		err  error
	}
	done := make(chan result, 1)
	go func() { plan, err := f.st.ExecuteOffboarding(ctx, a, f.employee.ID, p.ID); done <- result{plan, err} }()
	r5OffboardingWait(t, f.pool, ctx, gate.Conn().PgConn().PID())
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=expires_at FROM employee_offboarding_plans WHERE id=$1`, p.ID).Scan(&expired))
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("database deadline did not cross while audit blocked")
		case <-tick.C:
		}
	}
	must(t, gate.Commit(ctx))
	select {
	case r := <-done:
		r5OffboardingReject(t, r.plan, r.err, app.KindConflict)
	case <-ctx.Done():
		t.Fatal("offboarding audit-wait deadline command failed to terminate")
	}
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("expired audit wait committed session, draft, transfer, plan, audit or outbox effects")
	}
}

func TestR5OffboardingLifecyclePlanWaitUsesDatabaseDeadline(t *testing.T) {
	f := r5OffboardingSeed(t)
	a := r5OffboardingActor(t, f)
	p := r5OffboardingPreview(t, f, a)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	gate, err := f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(context.Background())
	_, err = gate.Exec(ctx, `SELECT id FROM employee_offboarding_plans WHERE id=$1 FOR UPDATE`, p.ID)
	must(t, err)
	type result struct {
		plan *company.OffboardingPlan
		err  error
	}
	done := make(chan result, 1)
	go func() { plan, err := f.st.ExecuteOffboarding(ctx, a, f.employee.ID, p.ID); done <- result{plan, err} }()
	r5OffboardingWait(t, f.pool, ctx, gate.Conn().PgConn().PID())
	// The source command began with a live observation but must use the
	// authoritative time/state after this exact plan-row wait.
	_, err = gate.Exec(ctx, `UPDATE employee_offboarding_plans SET expires_at=clock_timestamp() WHERE id=$1`, p.ID)
	must(t, err)
	before := r5OffboardingState(t, gate)
	must(t, gate.Commit(ctx))
	select {
	case r := <-done:
		r5OffboardingReject(t, r.plan, r.err, app.KindConflict)
	case <-ctx.Done():
		t.Fatal("expired plan-row waiter failed to terminate")
	}
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("expired plan-row waiter committed disposition effects")
	}
}

func TestR5OffboardingLifecyclePreviewExpiryBeforeReleaseRollsBack(t *testing.T) {
	f := r5OffboardingSeed(t)
	a := r5OffboardingActor(t, f)
	// A fixture-controlled deadline at the INSERT boundary tests equality
	// without a fake application clock or waiting fifteen minutes. The real
	// Preview command still owns plan/audit/outbox and must roll them all back.
	_, err := f.pool.Exec(context.Background(), `CREATE FUNCTION r5_offboarding_expiry_probe() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.expires_at=clock_timestamp(); RETURN NEW; END $$; CREATE TRIGGER r5_offboarding_expiry_probe BEFORE INSERT ON employee_offboarding_plans FOR EACH ROW EXECUTE FUNCTION r5_offboarding_expiry_probe()`)
	must(t, err)
	before := r5OffboardingState(t, f.pool)
	p, err := f.st.PreviewOffboarding(context.Background(), a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Database preview deadline equality proof")
	r5OffboardingReject(t, p, err, app.KindConflict)
	if r5OffboardingState(t, f.pool) != before {
		t.Fatal("expired preview released a plan or committed audit/outbox effects")
	}
}

func r5OffboardingHTTPData(t *testing.T, raw []byte, want int, status int) company.OffboardingPlan {
	t.Helper()
	if status != want {
		t.Fatalf("shipping offboarding HTTP status=%d want=%d", status, want)
	}
	var v struct {
		Data company.OffboardingPlan `json:"data"`
	}
	must(t, json.Unmarshal(raw, &v))
	return v.Data
}

func TestR5OffboardingLifecycleShippingHTTP(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	c := f.Companies[0]
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	status, _ := f.Request(t, ctx, f.JWT(0, "admin"), "PATCH", "/api/v1/admin/users/"+c.Users["sender"].ID.String(), map[string]any{"is_active": false}, "")
	if status != 200 {
		t.Fatal("formal freeze failed")
	}
	base := "/api/v1/company/employees/" + c.Users["sender"].ID.String() + "/offboard"
	status, raw := f.Request(t, ctx, f.JWT(0, "admin"), "POST", base+"/preview", map[string]any{"successor_user_id": c.Users["organizer"].ID, "options": map[string]any{"drafts": "seal"}, "reason": "Frozen employee actual HTTP disposition"}, "")
	p := r5OffboardingHTTPData(t, raw, 200, status)
	status, raw = f.Request(t, ctx, f.JWT(0, "admin"), "POST", base, map[string]any{"plan_id": p.ID}, "")
	done := r5OffboardingHTTPData(t, raw, 200, status)
	before := r5OffboardingState(t, f.Pool)
	status, raw = f.Request(t, ctx, f.JWT(0, "admin"), "POST", base, map[string]any{"plan_id": p.ID}, "")
	again := r5OffboardingHTTPData(t, raw, 200, status)
	if done.ExecutedAt == nil || again.ExecutedAt == nil || !done.ExecutedAt.Equal(*again.ExecutedAt) || strings.Contains(string(raw), "executed-v1:") || r5OffboardingState(t, f.Pool) != before {
		t.Fatal("HTTP completion replay exposed internal epoch or repeated effects")
	}
	status, _ = f.Request(t, ctx, f.JWT(0, "admin"), "PATCH", "/api/v1/admin/users/"+c.Users["sender"].ID.String(), map[string]any{"is_active": true}, "")
	if status != 200 {
		t.Fatal("actual reentry command failed")
	}
	before = r5OffboardingState(t, f.Pool)
	status, raw = f.Request(t, ctx, f.JWT(0, "admin"), "POST", base, map[string]any{"plan_id": p.ID}, "")
	if status != 409 || !strings.Contains(string(raw), `"code":"CONFLICT"`) || strings.Contains(string(raw), `"data"`) || r5OffboardingState(t, f.Pool) != before {
		t.Fatal("HTTP old executed plan leaked a receipt or affected reentry")
	}
}

func TestR5OffboardingLifecycleHTTPRechecksAfterTenantWait(t *testing.T) {
	for _, phase := range []string{"preview", "execute", "replay"} {
		for _, change := range []string{"demote", "freeze", "session", "successor-freeze"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				f := testpg.NewR5HTTPFixture(t)
				c := f.Companies[0]
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				base := "/api/v1/company/employees/" + c.Users["sender"].ID.String() + "/offboard"
				body := map[string]any{"successor_user_id": c.Users["organizer"].ID, "options": map[string]any{"drafts": "seal"}, "reason": "Current HTTP actor after real tenant wait"}
				path := base + "/preview"
				if phase != "preview" {
					status, raw := f.Request(t, ctx, f.JWT(0, "admin"), "POST", path, body, "")
					p := r5OffboardingHTTPData(t, raw, 200, status)
					body = map[string]any{"plan_id": p.ID}
					path = base
					if phase == "replay" {
						status, raw = f.Request(t, ctx, f.JWT(0, "admin"), "POST", path, body, "")
						r5OffboardingHTTPData(t, raw, 200, status)
					}
				}
				gate, err := f.Pool.Begin(ctx)
				must(t, err)
				defer gate.Rollback(context.Background())
				_, err = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, c.Tenant.ID)
				must(t, err)
				raw, err := json.Marshal(body)
				must(t, err)
				req, err := http.NewRequestWithContext(ctx, "POST", f.Server.URL+path, strings.NewReader(string(raw)))
				must(t, err)
				req.Header.Set("Authorization", "Bearer "+f.JWT(0, "admin"))
				req.Header.Set("Content-Type", "application/json")
				type result struct {
					status int
					body   []byte
					err    error
				}
				done := make(chan result, 1)
				go func() {
					res, err := f.Server.Client().Do(req)
					if err != nil {
						done <- result{err: err}
						return
					}
					defer res.Body.Close()
					body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
					done <- result{status: res.StatusCode, body: body, err: err}
				}()
				r5OffboardingWait(t, f.Pool, ctx, gate.Conn().PgConn().PID())
				want := 403
				if change == "session" {
					must(t, f.Store.UpdateUserPassword(ctx, c.Admin.ID, "test-only-new-waiting-admin-session"))
				} else {
					id := c.Admin.ID
					if change == "successor-freeze" {
						id = c.Users["organizer"].ID
						want = 400
					}
					u, err := f.Store.GetUser(ctx, id)
					must(t, err)
					if change == "demote" {
						u.Role = models.RoleUser
					} else {
						u.IsActive = false
					}
					must(t, f.Store.UpdateUser(ctx, u))
				}
				before := r5OffboardingState(t, f.Pool)
				must(t, gate.Commit(ctx))
				select {
				case r := <-done:
					must(t, r.err)
					if r.status != want {
						t.Fatalf("post-wait HTTP status=%d want=%d", r.status, want)
					}
					var envelope struct {
						Error struct {
							Code string `json:"code"`
						} `json:"error"`
						Data json.RawMessage `json:"data"`
					}
					must(t, json.Unmarshal(r.body, &envelope))
					code := "FORBIDDEN"
					if want == 400 {
						code = "BAD_REQUEST"
					}
					if envelope.Error.Code != code || envelope.Data != nil {
						t.Fatal("post-wait rejection leaked a plan or used the wrong error class")
					}
				case <-ctx.Done():
					t.Fatal("post-wait HTTP offboarding failed to terminate")
				}
				if r5OffboardingState(t, f.Pool) != before {
					t.Fatal("stale HTTP controller/successor produced preview/execute/replay effects")
				}
			})
		}
	}
}
