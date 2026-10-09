package postgres_test

import (
	"context"
	"testing"
	"time"

	"tabmail/internal/models"
)

// Password changes write no outbox, but their required audit references tenant.
// Freeze is a complete tenant-first command, not synthetic SQL user locking.
func TestR5PasswordChangeOrdersMemberFreeze(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token := rbNewRefresh(f.employee.ID)
	must(t, f.st.CreateRefreshToken(ctx, token))
	var initialVersion int64
	var initialPasswordAudit, initialMemberAudit, initialOutbox int
	must(t, f.pool.QueryRow(ctx, `SELECT session_version,(SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='user.password_change'),(SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='user.update'),(SELECT count(*) FROM outbox_events) FROM users WHERE id=$1`, f.employee.ID).Scan(&initialVersion, &initialPasswordAudit, &initialMemberAudit, &initialOutbox))
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, err)
	passwordDone := make(chan error, 1)
	const nextHash = "r5-password-parent-next-test-only"
	go func() {
		passwordDone <- f.st.ChangePasswordAtomic(ctx, f.employee.ID, f.employee.PasswordHash, nextHash)
	}()
	passwordPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
	freezeDone := make(chan error, 1)
	inactive := false
	go func() {
		_, e := f.st.UpdateUserGuarded(ctx, f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &inactive})
		freezeDone <- e
	}()
	// Original waits on target U; corrected parent-first password makes freeze
	// wait on T instead. Either edge is from the actual complete command.
	r5WaitBlockedBy(t, f, ctx, passwordPID, "")
	must(t, hold.Rollback(ctx))
	passwordErr := r5ConcurrentResult(t, ctx, passwordDone)
	freezeErr := r5ConcurrentResult(t, ctx, freezeDone)
	var hash string
	var version int64
	var active, revoked bool
	var passwordAudit, memberAudit, outbox int
	must(t, f.pool.QueryRow(ctx, `SELECT password_hash,session_version,is_active,(SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE id=$2),(SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='user.password_change'),(SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='user.update'),(SELECT count(*) FROM outbox_events) FROM users WHERE id=$1`, f.employee.ID, token.ID).Scan(&hash, &version, &active, &revoked, &passwordAudit, &memberAudit, &outbox))
	passwordAudit -= initialPasswordAudit
	memberAudit -= initialMemberAudit
	if !revoked || outbox != initialOutbox {
		t.Fatal("successful command lost refresh revocation or changed unrelated outbox")
	}
	passwordDead := r5SQLState(passwordErr) == "40P01"
	freezeDead := r5SQLState(freezeErr) == "40P01"
	switch {
	case passwordDead && freezeErr == nil:
		if hash != f.employee.PasswordHash || version != initialVersion+1 || active || passwordAudit != 0 || memberAudit != 1 {
			t.Fatal("password victim did not roll back hash/session/audit atomically while freeze committed")
		}
	case freezeDead && passwordErr == nil:
		if hash != nextHash || version != initialVersion+1 || !active || passwordAudit != 1 || memberAudit != 0 {
			t.Fatal("freeze victim did not roll back active/session/audit atomically while password committed")
		}
		_, err = f.st.UpdateUserGuarded(ctx, f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &inactive})
		must(t, err)
	case passwordErr == nil && freezeErr == nil:
		if hash != nextHash || version != initialVersion+2 || active || passwordAudit != 1 || memberAudit != 1 {
			t.Fatal("ordered password and freeze lost hash/session/active/audit state")
		}
	default:
		t.Fatalf("unexpected complete-command errors: password SQLSTATE=%s err=%v; freeze SQLSTATE=%s err=%v", r5SQLState(passwordErr), passwordErr, r5SQLState(freezeErr), freezeErr)
	}
	if passwordDead || freezeDead {
		t.Errorf("actual password/freeze 40P01; victim rollback verified: password SQLSTATE=%s; freeze SQLSTATE=%s", r5SQLState(passwordErr), r5SQLState(freezeErr))
	}
}

// Required audit-parent protection must precede even the initial user UPDATE;
// taking tenant protection after changing the password preserves the lock ring.
func TestR5PasswordAuditParentPrecedesUserUpdate(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() {
		done <- f.st.ChangePasswordAtomic(ctx, f.employee.ID, f.employee.PasswordHash, "r5-parent-first-test-only")
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")
	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, f.tenant.ID)
	// Record the baseline as a direct absent-parent proof, not a timeout.
	missingParent := err == nil
	if !missingParent {
		r5RequireLockConflict(t, err)
	}
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	if missingParent {
		t.Error("password command waited on user before protecting the required audit tenant parent")
	}
}

func r5PasswordState(t *testing.T, f *companyFixture) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('user',(SELECT to_jsonb(u) FROM users u WHERE id=$1),'refresh',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM refresh_tokens r WHERE user_id=$1),'audit',(SELECT count(*) FROM audit_log),'outbox',(SELECT count(*) FROM outbox_events))::text`, f.employee.ID).Scan(&state))
	return state
}

func TestR5PasswordAuditFailureRollsBackSessionAndRefresh(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	must(t, f.st.CreateRefreshToken(ctx, rbNewRefresh(f.employee.ID)))
	before := r5PasswordState(t, f)
	_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_password_audit_reject CHECK(action<>'user.password_change') NOT VALID`)
	must(t, err)
	err = f.st.ChangePasswordAtomic(ctx, f.employee.ID, f.employee.PasswordHash, "r5-rejected-password-test-only")
	if r5SQLState(err) != "23514" {
		t.Fatalf("expected exact injected audit constraint failure, not deadlock: %v", err)
	}
	if r5PasswordState(t, f) != before {
		t.Fatal("failed mandatory password audit left password/session/refresh/audit/outbox changes")
	}
	_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_password_audit_reject`)
	must(t, err)
	must(t, f.st.ChangePasswordAtomic(ctx, f.employee.ID, f.employee.PasswordHash, "r5-accepted-password-test-only"))
}
