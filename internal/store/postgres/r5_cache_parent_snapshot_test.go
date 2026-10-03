package postgres_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"

	"tabmail/internal/app"
	"tabmail/internal/app/companymail"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// A document references the message, not its tenant ancestor. Pin the actual
// catalog so a changed FK cannot silently invalidate the concurrency proof.
func r5CacheParents(t *testing.T, f *companyFixture, ctx context.Context) {
	t.Helper()
	var parents string
	must(t, f.pool.QueryRow(ctx, `SELECT string_agg(confrelid::regclass::text,',' ORDER BY confrelid::regclass::text) FROM pg_constraint WHERE conrelid='mail_documents'::regclass AND contype='f'`).Scan(&parents))
	if parents != "messages" {
		t.Fatalf("document FK parents changed: %q", parents)
	}
}

// Plain cache insertion intentionally does not need the audited-write parent
// fence. The audited recovery command does, and must acquire it before actor.
func TestR5CachePlainAndAuditedRecoveryParentClassification(t *testing.T) {
	for _, audited := range []bool{false, true} {
		name := "plain_cache"
		if audited {
			name = "audited_retry"
		}
		t.Run(name, func(t *testing.T) {
			f := seedCompany(t)
			_, d := r5IndexFixture(t, f, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			r5CacheParents(t, f, ctx)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			actor := f.u
			if audited {
				actor = f.a
			}
			_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, actor.ID)
			must(t, err)
			done := make(chan error, 1)
			go func() {
				if audited {
					_, e := f.st.RetryFailedMailIndex(ctx, actor, "classification fixture recovery")
					done <- e
					return
				}
				done <- f.st.SaveParsedMessage(ctx, actor, f.personal.ID, d)
			}()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")
			probe, err := f.pool.Begin(ctx)
			must(t, err)
			defer probe.Rollback(context.Background())
			_, err = probe.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, f.personal.ID)
			must(t, err)
			_, err = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, f.tenant.ID)
			if audited {
				r5RequireLockConflict(t, err)
			} else {
				must(t, err)
			}
			must(t, probe.Rollback(ctx))
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
		})
	}
}

// Both production calls execute completely. A table lock pauses the cache
// after actor/mailbox SHARE but before INSERT and its FK checks. The ingress
// worker then owns tenant UPDATE and is observed waiting on the cache's mailbox
// SHARE at its real counter UPDATE. Release the table, not a fabricated FK: a
// recursive tenant lock would form a real cycle here. Existing UPSERT is a
// separate control; admin and member use their real authorization branches.
func TestR5CacheWriteAndIngressDoNotAcquireAncestorFK(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, admin := range []bool{false, true} {
			name := "insert/member"
			if existing {
				name = "upsert/member"
			}
			if admin {
				name = name[:len(name)-len("member")] + "admin"
			}
			t.Run(name, func(t *testing.T) {
				f := seedCompany(t)
				_, d := r5IndexFixture(t, f, existing)
				d.TextBody = "cache independently committed"
				claim, message := r5ClaimIngress(t, f)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				r5CacheParents(t, f, ctx)
				actor := f.u
				if admin {
					actor = f.a
					access, err := f.st.GetWorkMailbox(ctx, f.a, f.personal.ID)
					must(t, err)
					// Tenant administration does not grant personal-mailbox
					// body access. Exercise the positive admin branch only
					// after the real grant command establishes read authority.
					must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.admin.ID, CanRead: true}, access.Revision))
				}
				hold, err := f.pool.Begin(ctx)
				must(t, err)
				defer hold.Rollback(context.Background())
				_, err = hold.Exec(ctx, `LOCK TABLE mail_documents IN SHARE MODE`)
				must(t, err)
				cacheDone := make(chan error, 1)
				go func() { cacheDone <- f.st.SaveParsedMessage(ctx, actor, f.personal.ID, d) }()
				cachePID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "INSERT INTO mail_documents")
				type result struct {
					delivered bool
					err       error
				}
				workerDone := make(chan result, 1)
				go func() { ok, e := f.st.DeliverIngress(ctx, claim, message, 100, 100); workerDone <- result{ok, e} }()
				r5WaitBlockedBy(t, f, ctx, cachePID, "UPDATE mailboxes m SET message_count")
				must(t, hold.Rollback(ctx))
				cacheErr := r5ConcurrentResult(t, ctx, cacheDone)
				var worker result
				select {
				case worker = <-workerDone:
				case <-ctx.Done():
					t.Fatal("ingress did not complete; timeout is not deadlock evidence")
				}
				if cacheErr != nil || worker.err != nil {
					t.Fatalf("full cache/ingress results: cache SQLSTATE=%s err=%v; ingress SQLSTATE=%s delivered=%v err=%v", r5SQLState(cacheErr), cacheErr, r5SQLState(worker.err), worker.delivered, worker.err)
				}
				if !worker.delivered {
					t.Fatal("ingress did not deliver its fixed target")
				}
				got, err := f.st.GetParsedMessage(ctx, actor, f.personal.ID, d.MessageID)
				must(t, err)
				if got == nil || got.TextBody != d.TextBody {
					t.Fatal("cache commit lost derived document")
				}
				var count, quota, mailbox, received, indexes int
				must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE tenant_id=$1),(SELECT sum(used) FROM ingress_daily_usage WHERE tenant_id=$1),(SELECT message_count FROM mailboxes WHERE id=$2),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='message.received'),(SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1)`, f.tenant.ID, f.personal.ID).Scan(&count, &quota, &mailbox, &received, &indexes))
				if count != 2 || quota != 2 || mailbox != 2 || received != 1 || indexes != 2 {
					t.Fatalf("unexpected committed effects: messages=%d quota=%d mailbox=%d received=%d indexes=%d", count, quota, mailbox, received, indexes)
				}
				t.Log("actual message-only FK allowed cache commit while ingress held tenant UPDATE; worker then committed once")
			})
		}
	}
}

// An SQL failure after cache authorization must not manufacture partial derived
// data or audit/outbox effects; do not label this injected constraint as 40P01.
func TestR5CacheConstraintFailureRollsBackDerivedWrite(t *testing.T) {
	f := seedCompany(t)
	_, d := r5IndexFixture(t, f, false)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `ALTER TABLE mail_documents ADD CONSTRAINT r5_cache_reject CHECK(text_body<>'rejected cache') NOT VALID`)
	must(t, err)
	var before string
	must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events))::text`).Scan(&before))
	d.TextBody = "rejected cache"
	err = f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d)
	if r5SQLState(err) != "23514" {
		t.Fatalf("expected constraint failure, not deadlock: %v", err)
	}
	got, e := f.st.GetParsedMessage(ctx, f.u, f.personal.ID, d.MessageID)
	must(t, e)
	if got != nil {
		t.Fatal("failed cache write left a document")
	}
	var after string
	must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events))::text`).Scan(&after))
	if before != after {
		t.Fatal("failed plain cache write changed audit/outbox")
	}
	d.TextBody = "accepted cache"
	must(t, f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d))
}

// Physical deletion owns the message parent before decrementing mailbox count.
// An insert-only cache barrier exposes that opposite order without replacing
// either production command or inventing a tenant FK. A detected victim must
// roll back all source/derived/count state; SQLSTATE is retained as evidence.
func TestR5CacheWriteVersusPhysicalDeleteLockOrder(t *testing.T) {
	for _, mode := range []string{"single", "expired", "purge"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			_, d := r5IndexFixture(t, f, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var purgeDeadline *time.Time
			if mode == "expired" {
				// Already elapsed content is rejected before INSERT, not a
				// candidate for the later cache/delete lock-order barrier.
				_, err := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '1 day',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, d.MessageID)
				must(t, err)
				r5CacheInitiallyExpiredHasNoEffects(t, f, ctx, d)
				// Independent new source: do not extend/restore the elapsed
				// fixture's deadline merely to make it enter the INSERT wait.
				f = seedCompany(t)
				_, d = r5IndexFixture(t, f, false)
				var deadline time.Time
				must(t, f.pool.QueryRow(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '1 day',purge_after=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING purge_after`, d.MessageID).Scan(&deadline))
				purgeDeadline = &deadline
			}
			r5CacheParents(t, f, ctx)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `LOCK TABLE mail_documents IN SHARE MODE`)
			must(t, err)
			cacheDone := make(chan error, 1)
			go func() { cacheDone <- f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d) }()
			cachePID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "INSERT INTO mail_documents")
			if purgeDeadline != nil {
				r5AwaitSentDeadline(t, f, ctx, cachePID, *purgeDeadline)
			}
			deleteDone := make(chan error, 1)
			go func() {
				if mode == "single" {
					deleteDone <- f.st.DeleteMessage(ctx, d.MessageID)
					return
				}
				if mode == "purge" {
					deleteDone <- f.st.PurgeMailbox(ctx, f.personal.ID)
					return
				}
				// Use the same actual DB clock as the crossed purge deadline;
				// host-clock skew must not make the GC candidate disappear.
				var cutoff time.Time
				if e := f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff); e != nil {
					deleteDone <- e
					return
				}
				_, _, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 1)
				deleteDone <- e
			}()
			// Baseline waits at the cascade table holding message DELETE. A
			// corrected cache source SHARE makes DELETE wait on that message
			// fence before owning DELETE, or mailbox-first deletion waits on
			// cache's mailbox fence. Observe the actual edge in either case.
			parentHeld := r5CacheDeleteWait(t, f, ctx, hold.Conn().PgConn().PID(), cachePID)
			probe, err := f.pool.Begin(ctx)
			must(t, err)
			defer probe.Rollback(context.Background())
			_, err = probe.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR KEY SHARE NOWAIT`, d.MessageID)
			if parentHeld {
				r5RequireLockConflict(t, err)
			} else {
				must(t, err)
			}
			must(t, probe.Rollback(ctx))
			must(t, hold.Rollback(ctx))
			cacheErr := r5ConcurrentResult(t, ctx, cacheDone)
			deleteErr := r5ConcurrentResult(t, ctx, deleteDone)
			if mode == "expired" {
				value, ok := app.As(cacheErr)
				if !ok || value.Kind != app.KindNotFound {
					t.Fatalf("cache crossing real purge deadline must roll back with 404: %v", cacheErr)
				}
				must(t, deleteErr)
			}
			cacheDead := r5SQLState(cacheErr) == "40P01"
			deleteDead := r5SQLState(deleteErr) == "40P01"
			var messages, documents, jobs, count int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE id=$1),(SELECT count(*) FROM mail_documents WHERE message_id=$1),(SELECT count(*) FROM mail_index_jobs WHERE message_id=$1),(SELECT message_count FROM mailboxes WHERE id=$2)`, d.MessageID, f.personal.ID).Scan(&messages, &documents, &jobs, &count))
			if cacheDead && deleteErr == nil {
				if messages != 0 || documents != 0 || jobs != 0 || count != 0 {
					t.Fatal("cache victim or completed deletion left partial effects")
				}
			} else if deleteDead && cacheErr == nil {
				if messages != 1 || documents != 1 || jobs != 1 || count != 1 {
					t.Fatal("delete victim did not roll back source/index/count atomically")
				}
				// A fresh command must be able to finish after the victim released locks.
				must(t, f.st.DeleteMessage(ctx, d.MessageID))
			} else {
				if deleteErr != nil {
					t.Fatalf("unexpected physical delete error SQLSTATE=%s: %v", r5SQLState(deleteErr), deleteErr)
				}
				if cacheErr != nil {
					ae, ok := app.As(cacheErr)
					if !ok || (ae.Kind != app.KindNotFound && ae.Kind != app.KindConflict) {
						t.Fatalf("unexpected cache error SQLSTATE=%s: %v", r5SQLState(cacheErr), cacheErr)
					}
				}
				if messages != 0 || documents != 0 || jobs != 0 || count != 0 {
					t.Fatal("serialized deletion left source/derived/count state")
				}
			}
			if cacheDead || deleteDead {
				t.Errorf("real 40P01: cache SQLSTATE=%s; delete SQLSTATE=%s; victim rollback verified; physical DELETE and cache mailbox/message order must align", r5SQLState(cacheErr), r5SQLState(deleteErr))
			}
		})
	}
}

// Return whether the delete is blocked at its child-table cascade (old order)
// rather than on cache's source-message SHARE or mailbox fence. Waits are observed
// through PostgreSQL, with timeout kept distinct from a 40P01 result.
func r5CacheDeleteWait(t *testing.T, f *companyFixture, ctx context.Context, tablePID, cachePID uint32) bool {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var parentHeld bool
		err := f.pool.QueryRow(ctx, `SELECT $1::int=ANY(pg_blocking_pids(pid)) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND pid<>$2::int AND ($1::int=ANY(pg_blocking_pids(pid)) OR $2::int=ANY(pg_blocking_pids(pid))) AND (query LIKE '%DELETE FROM messages%' OR query LIKE '%mailboxes%') LIMIT 1`, int32(tablePID), int32(cachePID)).Scan(&parentHeld)
		if err == nil {
			return parentHeld
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("observing physical delete wait: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("physical delete waiter not observed; not deadlock evidence")
		case <-ticker.C:
		}
	}
}

// Reverse schedule: the complete physical-delete command already owns message
// DELETE while its counter waits behind a controller's compatible mailbox SHARE.
// A cache call may read the old MVCC source, but must reject the busy source
// with Conflict before the mailbox controller is released, not wait on its FK.
func TestR5CacheBusySourceRejectsBeforePhysicalDeleteCounterRelease(t *testing.T) {
	for _, mode := range []string{"single", "expired", "purge"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			_, d := r5IndexFixture(t, f, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if mode == "expired" {
				_, err := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '1 day',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, d.MessageID)
				must(t, err)
			}
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR SHARE`, f.personal.ID)
			must(t, err)
			deleteDone := make(chan error, 1)
			go func() {
				switch mode {
				case "single":
					deleteDone <- f.st.DeleteMessage(ctx, d.MessageID)
				case "purge":
					deleteDone <- f.st.PurgeMailbox(ctx, f.personal.ID)
				default:
					_, _, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 1)
					deleteDone <- e
				}
			}()
			deletePID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "UPDATE mailboxes")
			// This probe must conflict: the actual DELETE, not a substitute controller,
			// already owns the source. It cannot be a fake source-busy fixture.
			probe, err := f.pool.Begin(ctx)
			must(t, err)
			defer probe.Rollback(context.Background())
			_, err = probe.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR SHARE NOWAIT`, d.MessageID)
			r5RequireLockConflict(t, err)
			must(t, probe.Rollback(ctx))
			cacheDone := make(chan error, 1)
			go func() { cacheDone <- f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d) }()
			var cacheErr error
			observedWait := false
			answered := false
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			// Distinguish the candidate's immediate response from the baseline's real
			// FK wait using pg_blocking_pids. A deadline alone is never product evidence.
			for !answered && !observedWait {
				select {
				case cacheErr = <-cacheDone:
					answered = true
				default:
				}
				if answered {
					break
				}
				var waiting bool
				err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND (query LIKE '%INSERT INTO mail_documents%' OR query LIKE '%FROM messages%'))`, int32(deletePID)).Scan(&waiting)
				must(t, err)
				if waiting {
					observedWait = true
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("neither cache response nor actual source waiter observed; not deadlock evidence")
				case <-ticker.C:
				}
			}
			if !observedWait {
				ae, ok := app.As(cacheErr)
				if !ok || ae.Kind != app.KindConflict {
					t.Fatalf("busy source must return Conflict before mailbox release: %v", cacheErr)
				}
				var docs int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE message_id=$1`, d.MessageID).Scan(&docs))
				if docs != 0 {
					t.Fatal("rejected busy-source cache left derived data")
				}
			}
			must(t, hold.Rollback(ctx))
			if observedWait {
				cacheErr = r5ConcurrentResult(t, ctx, cacheDone)
			}
			deleteErr := r5ConcurrentResult(t, ctx, deleteDone)
			if observedWait {
				// Keep exact baseline outcomes, rather than counting a controller timeout
				// as a defect. The actual source wait already violates fail-fast behavior.
				if cacheErr != nil && r5SQLState(cacheErr) != "40P01" {
					ae, ok := app.As(cacheErr)
					if !ok || (ae.Kind != app.KindNotFound && ae.Kind != app.KindConflict) {
						t.Fatalf("unexpected baseline cache result: %v", cacheErr)
					}
				}
				if deleteErr != nil && r5SQLState(deleteErr) != "40P01" {
					t.Fatalf("unexpected baseline delete result: %v", deleteErr)
				}
				if deleteErr != nil {
					must(t, f.st.DeleteMessage(ctx, d.MessageID))
				}
				t.Errorf("cache waited on actual physical-delete source instead of returning busy Conflict before release: cache SQLSTATE=%s err=%v; delete SQLSTATE=%s err=%v", r5SQLState(cacheErr), cacheErr, r5SQLState(deleteErr), deleteErr)
			} else {
				must(t, deleteErr)
			}
			var messages, docs, jobs, count int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE id=$1),(SELECT count(*) FROM mail_documents WHERE message_id=$1),(SELECT count(*) FROM mail_index_jobs WHERE message_id=$1),(SELECT message_count FROM mailboxes WHERE id=$2)`, d.MessageID, f.personal.ID).Scan(&messages, &docs, &jobs, &count))
			if messages != 0 || docs != 0 || jobs != 0 || count != 0 {
				t.Fatal("physical deletion did not atomically remove source/derived/index/count after controller release")
			}
		})
	}
}

// The elapsed negative control retains the source and index evidence, blocks
// would-be INSERT, and checks both real service zero-object I/O and SQL effects.
func r5CacheInitiallyExpiredHasNoEffects(t *testing.T, f *companyFixture, ctx context.Context, d company.ParsedMessage) {
	t.Helper()
	snapshot := func() string {
		var v string
		must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_object('message',to_jsonb(m),'document',(SELECT to_jsonb(doc) FROM mail_documents doc WHERE doc.message_id=m.id),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE j.message_id=m.id),'mailbox_count',(SELECT message_count FROM mailboxes WHERE id=m.mailbox_id),'audit_count',(SELECT count(*) FROM audit_log),'outbox_count',(SELECT count(*) FROM outbox_events))::text FROM messages m WHERE m.id=$1`, d.MessageID).Scan(&v))
		return v
	}
	before := snapshot()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `LOCK TABLE mail_documents IN SHARE MODE`)
	must(t, err)
	err = f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d)
	value, ok := app.As(err)
	if !ok || value.Kind != app.KindNotFound {
		t.Fatalf("already expired cache must reject before INSERT: %v", err)
	}
	objects := &r5ReadObjects{MemoryObjectStore: testutil.NewMemoryObjectStore()}
	svc := companymail.NewService(f.st, objects)
	detail, err := svc.Message(ctx, f.u, f.personal.ID, d.MessageID)
	value, ok = app.As(err)
	if detail != nil || !ok || value.Kind != app.KindNotFound {
		t.Fatalf("already expired detail returned payload: %v", err)
	}
	source, err := svc.Source(ctx, f.u, f.personal.ID, d.MessageID)
	if source != nil {
		source.Close()
	}
	value, ok = app.As(err)
	if source != nil || !ok || value.Kind != app.KindNotFound {
		t.Fatalf("already expired source returned reader: %v", err)
	}
	if objects.gets.Load() != 0 {
		t.Fatal("already expired content opened object store")
	}
	if after := snapshot(); after != before {
		t.Fatal("already expired rejection changed source/cache/index/count/audit/outbox")
	}
	must(t, hold.Rollback(ctx))
}
