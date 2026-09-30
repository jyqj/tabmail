package postgres_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
)

// Reachable on a company mailbox through app/messages.DeleteMessage (which
// dispatches to TrashCompanyMessages) and the retention scanner. PgStore's
// physical PurgeMailbox is a legacy-only fallback at that app seam: do not
// manufacture simultaneous company trash/legacy purge dispatch for one mailbox.
func r5MaintenanceFixture(t *testing.T, f *companyFixture) uuid.UUID {
	t.Helper()
	_, d := r5IndexFixture(t, f, true)
	_, err := f.pool.Exec(context.Background(), `UPDATE messages SET deleted_at=clock_timestamp()-interval '2 days',purge_after=clock_timestamp()-interval '1 day' WHERE id=$1`, d.MessageID)
	must(t, err)
	return d.MessageID
}
func r5MaintenanceState(t *testing.T, f *companyFixture, id uuid.UUID) [7]int {
	t.Helper()
	var state [7]int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM messages WHERE id=$1),(SELECT count(*) FROM mail_documents WHERE message_id=$1),(SELECT count(*) FROM mail_index_jobs WHERE message_id=$1),(SELECT message_count FROM mailboxes WHERE id=$2),(SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM mailbox_event_log WHERE mailbox_id=$2)`, id, f.personal.ID).Scan(&state[0], &state[1], &state[2], &state[3], &state[4], &state[5], &state[6]))
	return state
}
func r5MaintenanceExpect(t *testing.T, got, before [7]int, source, trash, deleted int, outboxPerMutation ...int) {
	t.Helper()
	outboxes := 1
	if len(outboxPerMutation) > 0 {
		outboxes = outboxPerMutation[0]
	}
	want := [7]int{source, source, source, source, before[4] + trash, before[5] + trash*outboxes, before[6] + trash + deleted}
	if got != want {
		t.Fatalf("message/doc/index/count/audit/outbox/event atomicity: got=%v want=%v", got, want)
	}
}

func TestR5MessageTrashAndRetentionCompleteCommandsLockOrder(t *testing.T) {
	r5RunMessageMaintenance(t, "legacy-trash")
}

// This is the CompanyMailHandler.Mutate port. It is reachable on the same
// company mailbox as retention, unlike legacy physical PurgeMailbox dispatch.
func TestR5MessageOrganizeAndRetentionCompleteCommandsLockOrder(t *testing.T) {
	r5RunMessageMaintenance(t, "organize-trash")
}

// The owner's seen path updates messages.seen, not a per-user sparse row. It
// shares messageMutation with organizing but keeps its distinct state owner.
func TestR5MessageOwnerSeenAndRetentionCompleteCommandsLockOrder(t *testing.T) {
	r5RunMessageMaintenance(t, "owner-seen")
}
func r5RunMessageMaintenance(t *testing.T, operation string) {
	outboxPerMutation := 1
	if operation != "legacy-trash" {
		outboxPerMutation = 2
	}
	for _, order := range []string{"retention-first", "trash-first"} {
		t.Run(order, func(t *testing.T) {
			f := seedCompany(t)
			id := r5MaintenanceFixture(t, f)
			before := r5MaintenanceState(t, f, id)
			beforeAssets := r5MaintenanceAssets(t, f, id)
			if operation == "owner-seen" {
				beforeAssets = r5MaintenanceSeenAssets(t, f, id)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			trashDone := make(chan error, 1)
			type retentionResult struct {
				count int
				keys  []string
				err   error
			}
			retentionDone := make(chan retentionResult, 1)
			trash := func() {
				if operation == "legacy-trash" {
					trashDone <- f.st.TrashCompanyMessages(ctx, f.tenant.ID, f.personal.ID, &id, "user:"+f.employee.ID.String())
					return
				}
				action := "trash"
				if operation == "owner-seen" {
					action = "seen"
				}
				trashDone <- f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, id, action)
			}
			retention := func() {
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
				retentionDone <- retentionResult{n, keys, e}
			}
			if order == "trash-first" {
				_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
				must(t, err)
				go trash()
				trashPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
				go retention()
				r5WaitBlockedBy(t, f, ctx, trashPID, "DELETE FROM messages")
			} else {
				_, err = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, id)
				must(t, err)
				go retention()
				retentionPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
				go trash()
				// Original trash owns T/M while queuing behind the physical delete.
				// If a corrected command fails fast here, retain that exact response
				// rather than mistaking an absent waiter for a controller timeout.
				r5MaintenanceWaitOrConflict(t, f, ctx, hold.Conn().PgConn().PID(), retentionPID, trashDone)
			}
			must(t, hold.Rollback(ctx))
			trashErr := r5ConcurrentResult(t, ctx, trashDone)
			var result retentionResult
			select {
			case result = <-retentionDone:
			case <-ctx.Done():
				t.Fatal("retention terminal not observed; timeout is not deadlock evidence")
			}
			trashDead := r5SQLState(trashErr) == "40P01"
			retentionDead := r5SQLState(result.err) == "40P01"
			switch {
			case trashDead && result.err == nil:
				if result.count != 1 || len(result.keys) != 1 {
					t.Fatal("successful retention lost deleted count/raw-key receipt")
				}
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 0, 1, outboxPerMutation)
			case retentionDead && trashErr == nil:
				if result.count != 0 || len(result.keys) != 0 {
					t.Fatal("retention victim returned noncommitted deletion receipts")
				}
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 1, 1, 0, outboxPerMutation)
				if r5MaintenanceAssets(t, f, id) != beforeAssets {
					t.Fatal("retention victim changed surviving source/document/job bytes or lease/token")
				}
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
				must(t, e)
				if n != 1 || len(keys) != 1 {
					t.Fatal("fresh retention did not finish after victim rollback")
				}
			case result.err == nil:
				trashEffects := 1
				if trashErr != nil {
					e, ok := app.As(app.FromAuthz(trashErr))
					if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindNotFound) {
						t.Fatalf("unexpected trash SQLSTATE=%s err=%v", r5SQLState(trashErr), trashErr)
					}
					trashEffects = 0
				}
				if result.count != 1 || len(result.keys) != 1 {
					t.Fatal("serialized retention lost deletion receipt")
				}
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, trashEffects, 1, outboxPerMutation)
			default:
				t.Fatalf("unexpected complete-command results trash SQLSTATE=%s err=%v; retention SQLSTATE=%s err=%v", r5SQLState(trashErr), trashErr, r5SQLState(result.err), result.err)
			}
			if trashDead || retentionDead {
				t.Errorf("actual %s/retention 40P01; victim source/doc/index/count/audit/outbox/event rollback verified: mutation SQLSTATE=%s retention SQLSTATE=%s", operation, r5SQLState(trashErr), r5SQLState(result.err))
			}
		})
	}
}

// Preserve source/document/job bytes, not only counts. On this already-trash
// fixture, retrash COALESCE changes no source fields; the retention victim must
// restore the exact parsed document and current claimed-index lease/token.
func r5MaintenanceAssets(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('source',(SELECT to_jsonb(m) FROM messages m WHERE id=$1),'document',(SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1))::text`, id).Scan(&state))
	return state
}

// Detect the actual lock edge, including FIFO tuple-lock queue blockers: the
// second waiter may point at retention, not directly at the controller. Also
// allow a corrected command to serialize on T/M or return bounded Conflict.
// Every result still runs the common data/receipt consistency assertions.
func r5MaintenanceWaitOrConflict(t *testing.T, f *companyFixture, ctx context.Context, blocker, retentionPID uint32, done chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			done <- err
			t.Logf("trash responded before controller release: SQLSTATE=%s err=%v", r5SQLState(err), err)
			return
		default:
		}
		var pid uint32
		var query string
		err := f.pool.QueryRow(ctx, `SELECT pid,query FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND pid<>$2::int AND ($1::int=ANY(pg_blocking_pids(pid)) OR $2::int=ANY(pg_blocking_pids(pid))) AND (query LIKE '%UPDATE messages%' OR query LIKE '%FROM messages%' OR query LIKE '%FROM mailboxes%' OR query LIKE '%FROM tenants%') LIMIT 1`, int32(blocker), int32(retentionPID)).Scan(&pid, &query)
		if err == nil {
			r5MaintenanceTrace(t, f, ctx, blocker, retentionPID, pid)
			if strings.Contains(query, "messages") {
				// Prove the original reverse edge really owns M while source waits;
				// this NOWAIT probe cannot turn a safe sequence into a new wait ring.
				probe, e := f.pool.Begin(ctx)
				must(t, e)
				defer probe.Rollback(context.Background())
				_, e = probe.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, f.personal.ID)
				r5RequireLockConflict(t, e)
				must(t, probe.Rollback(ctx))
			} else {
				t.Log("observed parent/mailbox serialization instead of assumed source wait")
			}
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			r5MaintenanceTrace(t, f, context.Background(), blocker, retentionPID, 0)
			t.Fatal("trash response/actual SQL waiter not observed; not deadlock evidence")
		case <-ticker.C:
		}
	}
}

func r5MaintenanceTrace(t *testing.T, f *companyFixture, ctx context.Context, pids ...uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ids := make([]int32, len(pids))
	for i, p := range pids {
		ids[i] = int32(p)
	}
	var trace string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object('activity',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'state',state,'query',query,'wait_type',wait_event_type,'wait',wait_event,'blockers',pg_blocking_pids(pid))) FROM pg_stat_activity WHERE datname=current_database() AND pid=ANY($1::int[])),'locks',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'type',locktype,'relation',relation::regclass::text,'mode',mode,'granted',granted,'transaction',transactionid::text,'page',page,'tuple',tuple)) FROM pg_locks WHERE pid=ANY($1::int[])))::text`, ids).Scan(&trace)
	if err != nil {
		t.Logf("lock trace unavailable: %v", err)
		return
	}
	t.Logf("fresh fixture actual wait graph: %s", trace)
}

func r5MaintenanceSeenAssets(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('source',(SELECT to_jsonb(m)||jsonb_build_object('seen',true) FROM messages m WHERE id=$1),'document',(SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1))::text`, id).Scan(&state))
	return state
}

// Company mailbox clearing selects only currently untrashed messages. Keep
// the existing tombstone, document and index of an older trash item unchanged;
// this is the real batch nil-ID port used by app/messages.PurgeMailbox.
func TestR5MessageCompanyClearPreservesExistingTombstones(t *testing.T) {
	f := seedCompany(t)
	oldID := r5MaintenanceFixture(t, f)
	oldAssets := r5MaintenanceAssets(t, f, oldID)
	_, fresh := r5IndexFixture(t, f, true)
	before := r5MaintenanceState(t, f, oldID)
	ctx := context.Background()
	must(t, f.st.TrashCompanyMessages(ctx, f.tenant.ID, f.personal.ID, nil, "user:"+f.employee.ID.String()))
	if r5MaintenanceAssets(t, f, oldID) != oldAssets {
		t.Fatal("company clear rewrote an existing trash/purge tombstone or its document/index lease")
	}
	var trashed bool
	must(t, f.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL AND purge_after>clock_timestamp() FROM messages WHERE id=$1`, fresh.MessageID).Scan(&trashed))
	if !trashed {
		t.Fatal("company clear did not give the active source its normal trash recovery deadline")
	}
	freshState := r5MaintenanceState(t, f, fresh.MessageID)
	if freshState[0] != 1 || freshState[1] != 1 || freshState[2] != 1 || freshState[3] != 2 {
		t.Fatal("company clear deleted source/derived/index data or decremented mailbox count")
	}

	after := r5MaintenanceState(t, f, oldID)
	want := before
	want[4]++
	want[5]++
	want[6]++
	if after != want {
		t.Fatalf("company clear must retain both source/doc/index/count and emit exactly one batch audit/outbox plus active-source event: got=%v want=%v", after, want)
	}
}
