package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// MessageAction calls MutateWorkMessage for an authorized non-owner. Both
// personal preferences below therefore use message_user_states, never the
// owner's messages.seen fast path. Retention handles the same real source.
// Service.MarkSeen has no production caller in the current checkout (only
// its definition and its PgStore call); no invented legacy dispatch is tested.
func TestR5PersonalMessageStateAndRetentionCompleteCommands(t *testing.T) {
	for _, action := range []string{"seen", "starred"} {
		for _, order := range []string{"retention-first", "mutation-first"} {
			t.Run(action+"/"+order, func(t *testing.T) {
				f := seedCompany(t)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true}))
				actor := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
				access, err := f.st.GetWorkMailbox(ctx, actor, f.personal.ID)
				must(t, err)
				if !access.CanRead || access.CanOrganize || *f.personal.OwnerUserID == actor.ID {
					t.Fatal("fixture must be a real read-only non-owner preference caller")
				}
				id := r5MaintenanceFixture(t, f)
				before := r5MaintenanceState(t, f, id)
				assets := r5MaintenanceAssets(t, f, id)
				r5PersonalState(t, f, id, actor.ID, false, action)
				hold, err := f.pool.Begin(ctx)
				must(t, err)
				defer hold.Rollback(context.Background())
				mutationDone := make(chan error, 1)
				type receipt struct {
					n    int
					keys []string
					err  error
				}
				retentionDone := make(chan receipt, 1)
				mutate := func() { mutationDone <- f.st.MutateWorkMessage(ctx, actor, f.personal.ID, id, action) }
				retain := func() {
					n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
					retentionDone <- receipt{n, keys, e}
				}
				if order == "mutation-first" {
					_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
					must(t, err)
					go mutate()
					pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
					go retain()
					r5WaitBlockedBy(t, f, ctx, pid, "DELETE FROM messages")
				} else {
					_, err = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, id)
					must(t, err)
					go retain()
					pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
					go mutate()
					r5MaintenanceWaitOrConflict(t, f, ctx, hold.Conn().PgConn().PID(), pid, mutationDone)
				}
				must(t, hold.Rollback(ctx))
				mutationErr := r5ConcurrentResult(t, ctx, mutationDone)
				var result receipt
				select {
				case result = <-retentionDone:
				case <-ctx.Done():
					t.Fatal("retention terminal not observed; timeout is not deadlock evidence")
				}
				mutationDead := r5SQLState(mutationErr) == "40P01"
				retentionDead := r5SQLState(result.err) == "40P01"
				switch {
				case mutationDead && result.err == nil:
					r5PersonalReceipt(t, result.n, result.keys, 1)
					r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 0, 1, 2)
					r5PersonalState(t, f, id, actor.ID, false, action)
				case retentionDead && mutationErr == nil:
					r5PersonalReceipt(t, result.n, result.keys, 0)
					r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 1, 1, 0, 2)
					r5PersonalState(t, f, id, actor.ID, true, action)
					if r5MaintenanceAssets(t, f, id) != assets {
						t.Fatal("retention victim altered source/document/index bytes or lease/token")
					}
					n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
					must(t, e)
					r5PersonalReceipt(t, n, keys, 1)
					r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 1, 1, 2)
					r5PersonalState(t, f, id, actor.ID, false, action)
				case result.err == nil:
					effects := 1
					if mutationErr != nil {
						e, ok := app.As(app.FromAuthz(mutationErr))
						if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindNotFound) {
							t.Fatalf("unexpected preference SQLSTATE=%s err=%v", r5SQLState(mutationErr), mutationErr)
						}
						effects = 0
					}
					r5PersonalReceipt(t, result.n, result.keys, 1)
					r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, effects, 1, 2)
					r5PersonalState(t, f, id, actor.ID, false, action)
				default:
					t.Fatalf("unexpected complete-command results preference SQLSTATE=%s err=%v retention SQLSTATE=%s err=%v", r5SQLState(mutationErr), mutationErr, r5SQLState(result.err), result.err)
				}
				if mutationDead || retentionDead {
					t.Errorf("actual non-owner %s/retention 40P01; source/state/doc/index/count/audit/outbox/event rollback verified: preference SQLSTATE=%s retention SQLSTATE=%s", action, r5SQLState(mutationErr), r5SQLState(result.err))
				}
			})
		}
	}
}

func r5PersonalReceipt(t *testing.T, n int, keys []string, want int) {
	t.Helper()
	if n != want || len(keys) != want {
		t.Fatalf("noncommitted or lost raw-key receipt: n=%d keys=%d want=%d", n, len(keys), want)
	}
}

func r5PersonalState(t *testing.T, f *companyFixture, id, user uuid.UUID, present bool, action string) {
	t.Helper()
	var count int
	var seen, starred bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT count(*),COALESCE(bool_or(seen),false),COALESCE(bool_or(starred),false) FROM message_user_states WHERE tenant_id=$1 AND mailbox_id=$2 AND message_id=$3 AND user_id=$4`, f.tenant.ID, f.personal.ID, id, user).Scan(&count, &seen, &starred))
	if !present {
		if count != 0 {
			t.Fatal("failed preference or cascade left a sparse state row")
		}
		return
	}
	if count != 1 || seen != (action == "seen") || starred != (action == "starred") {
		t.Fatalf("personal dimension/owner mismatch: count=%d seen=%v starred=%v action=%s", count, seen, starred, action)
	}
}
