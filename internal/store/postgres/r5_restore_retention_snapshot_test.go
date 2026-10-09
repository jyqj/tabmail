package postgres_test

import (
	"context"
	"testing"
	"time"

	"tabmail/internal/app"
	"tabmail/internal/models"
)

// CompanyMailHandler.MessageAction dispatches restore to MutateWorkMessage.
// Retention must recheck the source that it actually deletes after a wait,
// rather than treat an old trash candidate as an irrevocable delete receipt.
func TestR5RestoreAndRetentionSnapshotEligibility(t *testing.T) {
	for _, order := range []string{"restore-first", "retention-first"} {
		t.Run(order, func(t *testing.T) {
			f := seedCompany(t)
			id := r5MaintenanceFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			access, err := f.st.GetWorkMailbox(ctx, f.u, f.personal.ID)
			must(t, err)
			if !access.CanRead || !access.CanOrganize {
				t.Fatal("restore caller lacks current owner organizing authority")
			}
			before := r5MaintenanceState(t, f, id)
			var restoredAssets, key string
			must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_object('source',(SELECT to_jsonb(m)||jsonb_build_object('deleted_at',NULL,'purge_after',NULL,'expires_at',NULL) FROM messages m WHERE id=$1),'document',(SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1))::text,(SELECT raw_object_key FROM messages WHERE id=$1)`, id).Scan(&restoredAssets, &key))
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			restored := make(chan error, 1)
			type receipt struct {
				n    int
				keys []string
				err  error
			}
			deleted := make(chan receipt, 1)
			restore := func() { restored <- f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, id, "restore") }
			retain := func() {
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
				deleted <- receipt{n, keys, e}
			}
			if order == "restore-first" {
				_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
				must(t, err)
				go restore()
				pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
				// This row is held by the complete restore command, not by the
				// controller. Retention's real DELETE must be observed waiting.
				go retain()
				r5WaitBlockedBy(t, f, ctx, pid, "DELETE FROM messages")
			} else {
				_, err = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, id)
				must(t, err)
				go retain()
				pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
				go restore()
				r5MaintenanceWaitOrConflict(t, f, ctx, hold.Conn().PgConn().PID(), pid, restored)
			}
			must(t, hold.Rollback(ctx))
			restoreErr := r5ConcurrentResult(t, ctx, restored)
			var result receipt
			select {
			case result = <-deleted:
			case <-ctx.Done():
				t.Fatal("retention terminal missing; timeout is not deletion evidence")
			}
			if result.err != nil {
				t.Fatalf("retention failed SQLSTATE=%s err=%v", r5SQLState(result.err), result.err)
			}
			if result.n < 0 || result.n > 1 || len(result.keys) != result.n || (result.n == 1 && result.keys[0] != key) {
				t.Fatalf("deletion receipt does not identify committed exact source: n=%d keys=%v", result.n, result.keys)
			}
			if order == "restore-first" {
				must(t, restoreErr)
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 1-result.n, 1, result.n, 2)
				r5RestoreReferences(t, f, ctx, key, 1-result.n)
				if result.n == 0 {
					if r5MaintenanceAssets(t, f, id) != restoredAssets {
						t.Fatal("restore did not preserve source/document/index bytes and lease while clearing deadlines")
					}
				} else {
					t.Errorf("retention deleted a source after successful restore cleared deleted_at/purge_after/expires_at; stale candidate became committed raw-key receipt")
				}
			} else {
				if restoreErr == nil {
					t.Fatal("restore succeeded after retention won exact source deletion")
				}
				e, ok := app.As(app.FromAuthz(restoreErr))
				if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindNotFound) {
					t.Fatalf("restore loser SQLSTATE=%s err=%v", r5SQLState(restoreErr), restoreErr)
				}
				r5PersonalReceipt(t, result.n, result.keys, 1)
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 0, 1, 2)
				r5RestoreReferences(t, f, ctx, key, 0)
			}
		})
	}
}

func TestR5RestoreRetentionExpiredSourceControl(t *testing.T) {
	f := seedCompany(t)
	id := r5MaintenanceFixture(t, f)
	ctx := context.Background()
	before := r5MaintenanceState(t, f, id)
	var key string
	must(t, f.pool.QueryRow(ctx, `SELECT raw_object_key FROM messages WHERE id=$1`, id).Scan(&key))
	n, keys, err := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
	must(t, err)
	if n != 1 || len(keys) != 1 || keys[0] != key {
		t.Fatal("normally expired source lost exact physical deletion receipt")
	}
	r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 0, 1, 2)
	r5RestoreReferences(t, f, ctx, key, 0)
}

// These controls create the mailbox kind through its normal store command,
// not by stripping an owner from a company mailbox. Only source deadlines
// are seeded directly, as in the existing retention fixtures. Preserve both
// active-expiry and trash-purge equality semantics (the predicate is <, not <=).
func TestR5RestoreRetentionEligibilityControls(t *testing.T) {
	for _, name := range []string{"legacy-active-expired", "owner-permanent", "shared-permanent", "legacy-active-deadline-equal", "trash-purge-deadline-equal"} {
		t.Run(name, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			if name == "shared-permanent" {
				f.personal = f.shared
			} else if name != "owner-permanent" && name != "trash-purge-deadline-equal" {
				mb := &models.Mailbox{TenantID: f.tenant.ID, ZoneID: f.zone.ID, LocalPart: "legacy-control", ResolvedDomain: f.zone.Domain, FullAddress: "legacy-control@" + f.zone.Domain, Kind: "legacy", AccessMode: models.AccessAPIKey}
				must(t, f.st.CreateMailbox(ctx, mb))
				f.personal = mb
			}
			if f.personal.OwnerUserID == nil {
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: f.employee.ID, CanRead: true}))
			}
			_, doc := r5IndexFixture(t, f, true)
			cutoff := time.Now().UTC().Truncate(time.Microsecond)
			expiry := cutoff.Add(-time.Hour)
			if name == "legacy-active-deadline-equal" {
				expiry = cutoff
			}
			if name == "trash-purge-deadline-equal" {
				_, err := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=$2::timestamptz-interval '1 day',purge_after=$2,expires_at=NULL WHERE id=$1`, doc.MessageID, cutoff)
				must(t, err)
			} else {
				_, err := f.pool.Exec(ctx, `UPDATE messages SET expires_at=$2,deleted_at=NULL,purge_after=NULL WHERE id=$1`, doc.MessageID, expiry)
				must(t, err)
			}
			var fixtureValid bool
			must(t, f.pool.QueryRow(ctx, `SELECT CASE WHEN $2='trash-purge-deadline-equal' THEN deleted_at IS NOT NULL AND purge_after=$3 ELSE deleted_at IS NULL AND expires_at=$4 END FROM messages WHERE id=$1`, doc.MessageID, name, cutoff, expiry).Scan(&fixtureValid))
			if !fixtureValid {
				t.Fatal("fixture did not preserve exact active/trash deadline semantics")
			}
			before := r5MaintenanceState(t, f, doc.MessageID)
			assets := r5MaintenanceAssets(t, f, doc.MessageID)
			n, keys, err := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
			must(t, err)
			wantDeleted := 0
			if name == "legacy-active-expired" {
				wantDeleted = 1
			}
			if n != wantDeleted || len(keys) != wantDeleted || (wantDeleted == 1 && keys[0] != doc.SourceKey) {
				t.Fatalf("eligibility/receipt mismatch: n=%d keys=%v wantDeleted=%d", n, keys, wantDeleted)
			}
			r5MaintenanceExpect(t, r5MaintenanceState(t, f, doc.MessageID), before, 1-wantDeleted, 0, wantDeleted, 2)
			r5RestoreReferences(t, f, ctx, doc.SourceKey, 1-wantDeleted)
			if wantDeleted == 0 && r5MaintenanceAssets(t, f, doc.MessageID) != assets {
				t.Fatal("ineligible source/document/index or current lease/token was changed")
			}
		})
	}
}

func r5RestoreReferences(t *testing.T, f *companyFixture, ctx context.Context, key string, want int) {
	t.Helper()
	n, err := f.st.CountRawObjectReferences(ctx, key)
	must(t, err)
	if n != want {
		t.Fatalf("raw-key durable references=%d want=%d", n, want)
	}
}
