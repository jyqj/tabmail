package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// These invoke the same EmployeeInvitations port used by company_setup.Activate.
// SQL controllers arrange row/table waits only; no parser/authz stub is used.
func r5ActivationInvite(t *testing.T, f *companyFixture, profile *uuid.UUID) (*company.Invitation, string) {
	t.Helper()
	hash := company.Hash("r5-activation-" + uuid.NewString())
	inv, err := f.st.InviteEmployee(context.Background(), f.a, company.InvitationInput{Email: "activation@fixture.test", LocalPart: "activation", DisplayName: "Activation fixture", PermissionProfileID: profile}, hash)
	must(t, err)
	return inv, hash
}
func r5ActivationState(t *testing.T, f *companyFixture, inv *company.Invitation) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('invitation',(SELECT to_jsonb(i) FROM employee_invitations i WHERE id=$1),'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE email=$2),'mailboxes',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM mailboxes m WHERE full_address=$3),'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM permission_profiles p WHERE tenant_id=$4),'audit',(SELECT count(*) FROM audit_log),'outbox',(SELECT count(*) FROM outbox_events))::text`, inv.ID, inv.Email, inv.Address, f.tenant.ID).Scan(&state))
	return state
}
func r5ActivationKind(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	e, ok := app.As(app.FromAuthz(err))
	if !ok || e.Kind != kind {
		t.Fatalf("activation error kind=%s expected; SQLSTATE=%s err=%v", kind, r5SQLState(err), err)
	}
}

// Check all elapsed-time boundaries, not only an invitation already expired
// before Begin. The audit case reaches user/mailbox/consumption before waiting,
// so a late rejection must roll back every state owner, including derived data.
func TestR5ActivationExpiryAfterRealWaitRollsBack(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, wait := range []string{"tenant", "invitation", "audit"} {
		t.Run(wait, func(t *testing.T) {
			f := seedCompany(t)
			inv, hash := r5ActivationInvite(t, f, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var deadline time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE employee_invitations SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, inv.ID).Scan(&deadline))
			before := r5ActivationState(t, f, inv)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			query := ""
			switch wait {
			case "tenant":
				_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
				query = "tenants"
			case "invitation":
				_, err = hold.Exec(ctx, `SELECT id FROM employee_invitations WHERE id=$1 FOR UPDATE`, inv.ID)
				query = "employee_invitations"
			default:
				_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
				query = "audit_log"
			}
			must(t, err)
			done := make(chan error, 1)
			go func() { done <- f.st.ActivateEmployee(ctx, hash, "r5-activation-password-test-only") }()
			pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), query)
			r5AwaitSentDeadline(t, f, ctx, pid, deadline)
			must(t, hold.Rollback(ctx))
			r5ActivationKind(t, r5ConcurrentResult(t, ctx, done), app.KindBadRequest)
			if after := r5ActivationState(t, f, inv); after != before {
				t.Fatal("expired activation left user/mailbox/profile/consumption/audit/outbox effects")
			}
		})
	}
}

func TestR5ActivationDuplicateTokenCommandsCommitOnce(t *testing.T) {
	f := seedCompany(t)
	inv, hash := r5ActivationInvite(t, f, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var initialAudit, initialOutbox int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events)`).Scan(&initialAudit, &initialOutbox))
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, err)
	first := make(chan error, 1)
	go func() { first <- f.st.ActivateEmployee(ctx, hash, "r5-duplicate-first-test-only") }()
	pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
	second := make(chan error, 1)
	go func() { second <- f.st.ActivateEmployee(ctx, hash, "r5-duplicate-second-test-only") }()
	r5WaitBlockedBy(t, f, ctx, pid, "tenants")
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, first)
	r5ActivationKind(t, r5ConcurrentResult(t, ctx, second), app.KindBadRequest)
	var users, mailboxes, audits, outbox int
	var consumed, owner bool
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE email=$1),(SELECT count(*) FROM mailboxes WHERE full_address=$2),(SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events),(SELECT consumed_at IS NOT NULL FROM employee_invitations WHERE id=$3),EXISTS(SELECT 1 FROM mailboxes m JOIN users u ON u.id=m.owner_user_id WHERE u.email=$1 AND m.full_address=$2 AND u.password_hash='r5-duplicate-first-test-only' AND u.role='user' AND m.mailbox_kind='personal')`, inv.Email, inv.Address, inv.ID).Scan(&users, &mailboxes, &audits, &outbox, &consumed, &owner))
	if users != 1 || mailboxes != 1 || audits != initialAudit+1 || outbox != initialOutbox+1 || !consumed || !owner {
		t.Fatal("duplicate activation lost exactly-once ownership/consumption/audit/event invariant")
	}
}

func TestR5ActivationCurrentSponsorAndDomainBoundary(t *testing.T) {
	for _, boundary := range []string{"sponsor-frozen", "domain-unverified", "domain-changed"} {
		t.Run(boundary, func(t *testing.T) {
			f := seedCompany(t)
			inv, hash := r5ActivationInvite(t, f, nil)
			ctx := context.Background()
			expected := app.KindBadRequest
			switch boundary {
			case "sponsor-frozen":
				supervisor := &models.User{TenantID: f.tenant.ID, Email: "activation-supervisor@fixture.test", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "test-only"}
				must(t, f.st.CreateUser(ctx, supervisor))
				actor := authz.Actor{Type: authz.PrincipalUser, ID: supervisor.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, IsAdmin: true}
				inactive := false
				_, err := f.st.UpdateUserGuarded(ctx, actor, f.tenant.ID, f.admin.ID, models.UserAdminPatch{IsActive: &inactive})
				must(t, err)
				expected = app.KindForbidden
			case "domain-unverified":
				zone := *f.zone
				zone.IsVerified = false
				must(t, f.st.UpdateZone(ctx, &zone))
			default:
				zone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "replacement.fixture.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, zone))
				settings, err := f.st.GetCompanySettings(ctx, f.tenant.ID)
				must(t, err)
				settings.PrimaryZoneID = zone.ID
				_, err = f.st.ConfigureCompany(ctx, f.a, *settings)
				must(t, err)
				expected = app.KindConflict
			}
			before := r5ActivationState(t, f, inv)
			r5ActivationKind(t, f.st.ActivateEmployee(ctx, hash, "r5-boundary-test-only"), expected)
			if r5ActivationState(t, f, inv) != before {
				t.Fatal("rejected current sponsor/domain boundary changed activation state")
			}
		})
	}
}

func TestR5ActivationReadsCurrentProfileAfterTenantWait(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	profile := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "Activation explicit profile", CanSend: true, DailySendQuota: 7, MaxMailboxes: 1}
	must(t, f.st.CreatePermissionProfile(ctx, profile))
	inv, hash := r5ActivationInvite(t, f, &profile.ID)
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.ActivateEmployee(ctx, hash, "r5-current-profile-test-only") }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "tenants")
	// The existing profile writer does not acquire tenant; use that real port,
	// and preserve its current scope rather than inventing a future CAS contract.
	profile.CanSend = false
	profile.DailySendQuota = 0
	must(t, f.st.UpdatePermissionProfile(ctx, profile))
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	user, err := f.st.GetUserByEmail(ctx, inv.Email)
	must(t, err)
	if user == nil || user.PermissionProfileID == nil || *user.PermissionProfileID != profile.ID {
		t.Fatal("activation did not retain explicit current profile")
	}
	permission, err := f.st.EffectivePermission(ctx, user.ID)
	must(t, err)
	if permission.CanSend || permission.DailySendQuota != 0 {
		t.Fatal("activation restored stale invitation-time profile permissions")
	}
}

// Zone verification is maintained by the real UpdateZone port without taking
// the activation tenant lock. Its non-key UPDATE may commit while activation
// waits at the mandatory audit after validating the domain and inserting its
// mailbox. Final eligibility must not use that stale domain authorization.
func TestR5ActivationDomainWithdrawalDuringAuditWaitRollsBack(t *testing.T) {
	for _, field := range []string{"verified", "mx"} {
		t.Run(field, func(t *testing.T) {
			f := seedCompany(t)
			inv, hash := r5ActivationInvite(t, f, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			before := r5ActivationState(t, f, inv)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			must(t, err)
			done := make(chan error, 1)
			go func() { done <- f.st.ActivateEmployee(ctx, hash, "r5-domain-withdrawal-test-only") }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			zone := *f.zone
			if field == "verified" {
				zone.IsVerified = false
			} else {
				zone.MXVerified = false
			}
			must(t, f.st.UpdateZone(ctx, &zone))
			// Independently prove the production writer committed the actual withdrawal
			// before releasing the controller, rather than guessing from a goroutine.
			var verified bool
			must(t, f.pool.QueryRow(ctx, `SELECT CASE WHEN $2='verified' THEN is_verified ELSE mx_verified END FROM domain_zones WHERE id=$1`, zone.ID, field).Scan(&verified))
			if verified {
				t.Fatal("production domain withdrawal did not commit")
			}
			must(t, hold.Rollback(ctx))
			r5ActivationKind(t, r5ConcurrentResult(t, ctx, done), app.KindBadRequest)
			if r5ActivationState(t, f, inv) != before {
				t.Fatal("withdrawn domain activation left user/mailbox/profile/consumption/audit/outbox effects")
			}
		})
	}
}

// The writer is the production UpdateZone command, held AFTER its real UPDATE
// by a fresh-database trigger. The trigger only exposes an otherwise tiny
// transaction interval: the controller never fabricates a domain write. Its
// non-key lock is compatible with activation's mailbox FK KEY SHARE, but the
// final domain SHARE must fail fast rather than creating a late wait edge.
func TestR5ActivationBusyFinalDomainFenceRollsBackWithoutWait(t *testing.T) {
	f := seedCompany(t)
	inv, hash := r5ActivationInvite(t, f, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION r5_activation_zone_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(714500070); RETURN NEW; END $$; CREATE TRIGGER r5_activation_zone_barrier AFTER UPDATE ON domain_zones FOR EACH ROW EXECUTE FUNCTION r5_activation_zone_barrier()`)
	must(t, err)
	before := r5ActivationState(t, f, inv)
	auditHold, err := f.pool.Begin(ctx)
	must(t, err)
	defer auditHold.Rollback(context.Background())
	_, err = auditHold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, err)
	writerHold, err := f.pool.Begin(ctx)
	must(t, err)
	defer writerHold.Rollback(context.Background())
	_, err = writerHold.Exec(ctx, `SELECT pg_advisory_xact_lock(714500070)`)
	must(t, err)
	activationDone := make(chan error, 1)
	go func() { activationDone <- f.st.ActivateEmployee(ctx, hash, "r5-domain-busy-test-only") }()
	r5WaitBlockedBy(t, f, ctx, auditHold.Conn().PgConn().PID(), "audit_log")
	zone := *f.zone
	zone.TXTRecord = "r5-activation-domain-writer-test-only"
	writerDone := make(chan error, 1)
	go func() { writerDone <- f.st.UpdateZone(ctx, &zone) }()
	r5WaitBlockedBy(t, f, ctx, writerHold.Conn().PgConn().PID(), "UPDATE domain_zones")
	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM domain_zones WHERE id=$1 FOR KEY SHARE NOWAIT`, zone.ID)
	must(t, err)
	_, err = probe.Exec(ctx, `SELECT id FROM domain_zones WHERE id=$1 FOR SHARE NOWAIT`, zone.ID)
	r5RequireLockConflict(t, err)
	must(t, probe.Rollback(ctx))
	must(t, auditHold.Rollback(ctx))
	// Keep the writer barrier held until the cache-independent activation has
	// actually responded; NOWAIT's 55P03 must be mapped to bounded Conflict.
	activationErr := r5ConcurrentResult(t, ctx, activationDone)
	r5ActivationKind(t, activationErr, app.KindConflict)
	if r5SQLState(activationErr) == "40P01" {
		t.Fatal("deadlock is not a bounded busy-domain conflict")
	}
	if r5ActivationState(t, f, inv) != before {
		t.Fatal("busy final domain fence left activation user/mailbox/profile/consumption/audit/outbox effects")
	}
	must(t, writerHold.Rollback(ctx))
	r5AwaitOperation(t, ctx, writerDone)
	var txt string
	must(t, f.pool.QueryRow(ctx, `SELECT txt_record FROM domain_zones WHERE id=$1`, zone.ID).Scan(&txt))
	if txt != zone.TXTRecord {
		t.Fatal("real domain writer did not commit after rejected activation released locks")
	}
}
