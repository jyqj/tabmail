package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Finite shared fixtures are provisioned through the shipping retention input,
// never by relabelling the default shared-zero historical anomaly as finite.
func r5RestoreFiniteFixture(t *testing.T, f *companyFixture) (*models.Mailbox, *models.Message) {
	t.Helper()
	ctx := context.Background()
	hours := 24
	mb, e := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "finite-" + uuid.NewString()[:8], Kind: "shared", RetentionHours: &hours})
	must(t, e)
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true}))
	var now time.Time
	must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now))
	expiry, e := models.MessageExpiry(mb, hours, now)
	must(t, e)
	if mb.RetentionHoursOverride == nil || *mb.RetentionHoursOverride != hours || expiry == nil {
		t.Fatal("formal shared finite fixture lost retention policy")
	}
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: mb.ID, ZoneID: f.zone.ID, Sender: "sender@restore.test", Recipients: []string{mb.FullAddress}, Subject: "finite restoration", RawObjectKey: "restore-" + uuid.NewString(), ExpiresAt: expiry}
	must(t, f.st.CreateMessage(ctx, m))
	return mb, m
}
func r5RestoreRequirePG(t *testing.T) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("R5 restore lifecycle requires owned TABMAIL_TEST_DB_DSN, not skip")
	}
}
func r5RestoreState(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('message',(SELECT to_jsonb(m) FROM messages m WHERE id=$1),'document',(SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1),'audit',(SELECT count(*) FROM audit_log),'outbox',(SELECT count(*) FROM outbox_events),'events',(SELECT count(*) FROM mailbox_event_log),'mailbox_counts',(SELECT jsonb_agg(jsonb_build_array(id,message_count) ORDER BY id) FROM mailboxes))::text`, id).Scan(&state))
	return state
}
func r5RestoreCutoffs(t *testing.T, f *companyFixture, id uuid.UUID) (string, bool, bool) {
	t.Helper()
	var expiry string
	var deleted, purge bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT COALESCE(expires_at::text,'NULL'),deleted_at IS NOT NULL,purge_after IS NOT NULL FROM messages WHERE id=$1`, id).Scan(&expiry, &deleted, &purge))
	return expiry, deleted, purge
}
func r5RestoreConflict(t *testing.T, e error) {
	t.Helper()
	v, ok := app.As(e)
	if !ok || v.Kind != app.KindConflict {
		t.Fatalf("restore unavailable deadline want409, got %v", e)
	}
}

func TestR5RestoreLifecyclePreservesRecordedDeadline(t *testing.T) {
	r5RestoreRequirePG(t)
	for _, axis := range []string{"active-finite", "archive-finite", "personal-legacy-past", "shared-zero-nil", "shared-null-nil", "shared-zero-unknown-future", "shared-null-unknown-future"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			mb, m := r5RestoreFiniteFixture(t, f)
			ctx := context.Background()
			if axis == "personal-legacy-past" {
				mb = f.personal
				past := time.Now().Add(-time.Hour)
				m = &models.Message{TenantID: f.tenant.ID, MailboxID: mb.ID, ZoneID: f.zone.ID, Sender: "legacy@restore.test", Recipients: []string{mb.FullAddress}, Subject: "personal historical expiry", RawObjectKey: "restore-personal-" + uuid.NewString(), ExpiresAt: &past}
				must(t, f.st.CreateMessage(ctx, m))
			} else if strings.HasPrefix(axis, "shared-") {
				var override any = 0
				if strings.Contains(axis, "null") {
					override = nil
				}
				if strings.HasSuffix(axis, "nil") {
					// Provision zero-retention normally; never erase a finite
					// row's hard deadline to manufacture a nil positive.
					zero := 0
					var e error
					mb, e = f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "permanent-" + uuid.NewString()[:8], Kind: "shared", RetentionHours: &zero})
					must(t, e)
					must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true}))
					_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=$2 WHERE id=$1`, mb.ID, override)
					must(t, e)
					m = &models.Message{TenantID: f.tenant.ID, MailboxID: mb.ID, ZoneID: f.zone.ID, Sender: "permanent@restore.test", Recipients: []string{mb.FullAddress}, RawObjectKey: "restore-permanent-" + uuid.NewString()}
					must(t, f.st.CreateMessage(ctx, m))
				} else {
					// Named historical current-policy mismatch: retain the
					// recorded future snapshot unchanged; do not map/migrate.
					_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=$2 WHERE id=$1`, mb.ID, override)
					must(t, e)
				}
			}
			must(t, f.st.SaveParsedMessage(ctx, f.u, mb.ID, company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("restored cache"), ParserVersion: 1, TextBody: "restored derived content"}))
			derived := func() string {
				var state string
				must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1))::text`, m.ID).Scan(&state))
				return state
			}
			beforeDerived := derived()
			before, _, _ := r5RestoreCutoffs(t, f, m.ID)
			if axis == "archive-finite" {
				must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "archive"))
				after, _, _ := r5RestoreCutoffs(t, f, m.ID)
				if after != before {
					t.Fatal("archive paused/changed hard expiry")
				}
			}
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "trash"))
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore"))
			after, deleted, purge := r5RestoreCutoffs(t, f, m.ID)
			if after != before || deleted || purge {
				t.Fatalf("restore changed recorded expiry or retained trash: before=%s after=%s deleted=%v purge=%v", before, after, deleted, purge)
			}
			if derived() != beforeDerived {
				t.Fatal("restore changed derived cache or index metadata")
			}
			got, e := f.st.GetWorkMessage(ctx, f.u, mb.ID, m.ID)
			must(t, e)
			if got == nil {
				t.Fatal("valid restored message unavailable")
			}
		})
	}
}

func TestR5RestoreLifecycleRejectsElapsedDeadlineWithoutEffects(t *testing.T) {
	r5RestoreRequirePG(t)
	for _, axis := range []string{"hard-past", "hard-equal", "purge-past", "purge-equal", "archive-hard-past", "shared-zero-unknown-past", "shared-null-unknown-past", "personal-purge-past", "no-organize", "no-read", "missing", "not-trash"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			mb, m := r5RestoreFiniteFixture(t, f)
			ctx := context.Background()
			if axis == "personal-purge-past" {
				mb = f.personal
				m = &models.Message{TenantID: f.tenant.ID, MailboxID: mb.ID, ZoneID: f.zone.ID, Sender: "legacy@restore.test", Recipients: []string{mb.FullAddress}}
				must(t, f.st.CreateMessage(ctx, m))
			}
			if axis == "archive-hard-past" {
				must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "archive"))
			}
			if axis != "not-trash" {
				must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "trash"))
			}
			var e error
			switch axis {
			case "hard-past", "archive-hard-past":
				_, e = f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
			case "hard-equal":
				_, e = f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`, m.ID)
			case "purge-past", "personal-purge-past":
				_, e = f.pool.Exec(ctx, `UPDATE messages SET purge_after=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
			case "purge-equal":
				_, e = f.pool.Exec(ctx, `UPDATE messages SET purge_after=clock_timestamp() WHERE id=$1`, m.ID)
			case "shared-zero-unknown-past", "shared-null-unknown-past":
				var override any = 0
				if strings.Contains(axis, "null") {
					override = nil
				}
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=$2 WHERE id=$1`, mb.ID, override)
				must(t, e)
				_, e = f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
			case "no-organize":
				e = grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true})
			case "no-read":
				e = grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID})
			case "missing":
				_, e = f.pool.Exec(ctx, `DELETE FROM messages WHERE id=$1`, m.ID)
			}
			must(t, e)
			before := r5RestoreState(t, f, m.ID)
			e = f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore")
			if axis == "no-organize" || axis == "no-read" {
				v, ok := app.As(e)
				if !ok || v.Kind != app.KindForbidden {
					t.Fatalf("restore without organize want403: %v", e)
				}
			} else if axis == "missing" || axis == "not-trash" {
				v, ok := app.As(e)
				if !ok || v.Kind != app.KindNotFound {
					t.Fatalf("restore missing/nontrash want404: %v", e)
				}
			} else {
				r5RestoreConflict(t, e)
			}
			if after := r5RestoreState(t, f, m.ID); after != before {
				t.Fatal("rejected restore changed source/index/cache/audit/outbox/events/counts")
			}
		})
	}
}

// Identity/mailbox waits occur before source NOWAIT. Audit/outbox relation waits
// occur after UPDATE has cleared purge. The latter must retain the ORIGINAL
// purge deadline in the operation guard, not infer permanence from new NULL.
func TestR5RestoreLifecycleRechecksClockAfterWait(t *testing.T) {
	r5RestoreRequirePG(t)
	for _, axis := range []string{"identity-hard", "mailbox-hard", "audit-purge", "outbox-purge", "audit-hard", "outbox-hard"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			mb, m := r5RestoreFiniteFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "trash"))
			var deadline time.Time
			if strings.HasSuffix(axis, "purge") {
				must(t, f.pool.QueryRow(ctx, `UPDATE messages SET purge_after=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING purge_after`, m.ID).Scan(&deadline))
			} else {
				must(t, f.pool.QueryRow(ctx, `UPDATE messages SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, m.ID).Scan(&deadline))
			}
			before := r5RestoreState(t, f, m.ID)
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			query := "users"
			switch {
			case strings.HasPrefix(axis, "identity"):
				_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
			case strings.HasPrefix(axis, "mailbox"):
				query = "mailboxes"
				_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, mb.ID)
			case strings.HasPrefix(axis, "audit"):
				query = "audit_log"
				_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			default:
				query = "outbox_events"
				_, e = hold.Exec(ctx, `LOCK TABLE outbox_events IN SHARE MODE`)
			}
			must(t, e)
			done := make(chan error, 1)
			go func() { done <- f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore") }()
			pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), query)
			r5AwaitSentDeadline(t, f, ctx, pid, deadline)
			must(t, hold.Rollback(ctx))
			e = r5ConcurrentResult(t, ctx, done)
			r5RestoreConflict(t, e)
			if r5RestoreState(t, f, m.ID) != before {
				t.Fatal("restore crossing deadline committed partial effects")
			}
		})
	}
}

func TestR5RestoreLifecycleSourceBusyAndDeleted(t *testing.T) {
	r5RestoreRequirePG(t)
	for _, axis := range []string{"source-nowait", "deleted-before-restore", "restored-before-old-purge"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			mb, m := r5RestoreFiniteFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "trash"))
			if axis == "deleted-before-restore" {
				must(t, f.st.DeleteMessage(ctx, m.ID))
				before := r5RestoreState(t, f, m.ID)
				e := f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore")
				v, ok := app.As(e)
				if !ok || v.Kind != app.KindNotFound {
					t.Fatalf("deleted source restore want404: %v", e)
				}
				if r5RestoreState(t, f, m.ID) != before {
					t.Fatal("missing source restore created effects")
				}
				return
			}
			if axis == "source-nowait" {
				before := r5RestoreState(t, f, m.ID)
				hold, e := f.pool.Begin(ctx)
				must(t, e)
				defer hold.Rollback(context.Background())
				_, e = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, m.ID)
				must(t, e)
				e = f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore")
				r5RestoreConflict(t, e)
				if r5RestoreState(t, f, m.ID) != before {
					t.Fatal("busy source restore created effects")
				}
				must(t, hold.Rollback(ctx))
				return
			}
			var oldPurge time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE messages SET purge_after=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING purge_after`, m.ID).Scan(&oldPurge))
			expiry, _, _ := r5RestoreCutoffs(t, f, m.ID)
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore"))
			// Wait on authoritative time only; no fabricated future GC cutoff.
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				var elapsed bool
				must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, oldPurge).Scan(&elapsed))
				if elapsed {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-tick.C:
				}
			}
			var cutoff time.Time
			must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff))
			n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
			must(t, e)
			if n != 0 || len(keys) != 0 {
				t.Fatal("GC reused old trash eligibility after valid restore")
			}
			after, deleted, purge := r5RestoreCutoffs(t, f, m.ID)
			if after != expiry || deleted || purge {
				t.Fatal("restore/GC lost current lifecycle proof")
			}
		})
	}
}

func TestR5RestoreLifecycleCurrentProofVersusGC(t *testing.T) {
	r5RestoreRequirePG(t)
	for _, axis := range []string{"restore-waits-past-purge", "gc-owns-source"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			mb, m := r5RestoreFiniteFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			must(t, f.st.SaveParsedMessage(ctx, f.u, mb.ID, company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("restore source"), ParserVersion: 1, TextBody: "retained restore cache"}))
			must(t, f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "trash"))
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			type receipt struct {
				n    int
				keys []string
				err  error
			}
			deleted := make(chan receipt, 1)
			retain := func() {
				var cutoff time.Time
				e := f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff)
				if e != nil {
					deleted <- receipt{err: e}
					return
				}
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
				deleted <- receipt{n, keys, e}
			}
			var restoreErr error
			if axis == "restore-waits-past-purge" {
				var deadline time.Time
				must(t, f.pool.QueryRow(ctx, `UPDATE messages SET purge_after=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING purge_after`, m.ID).Scan(&deadline))
				_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
				must(t, e)
				restored := make(chan error, 1)
				go func() { restored <- f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore") }()
				restorePID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
				r5AwaitSentDeadline(t, f, ctx, restorePID, deadline)
				go retain()
				r5WaitBlockedBy(t, f, ctx, restorePID, "DELETE FROM messages")
				must(t, hold.Rollback(ctx))
				restoreErr = r5ConcurrentResult(t, ctx, restored)
				r5RestoreConflict(t, restoreErr)
			} else {
				_, e = f.pool.Exec(ctx, `UPDATE messages SET purge_after=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
				must(t, e)
				_, e = hold.Exec(ctx, `LOCK TABLE mail_documents IN SHARE MODE`)
				must(t, e)
				go retain()
				r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
				restoreErr = f.st.MutateWorkMessage(ctx, f.u, mb.ID, m.ID, "restore")
				r5RestoreConflict(t, restoreErr)
				must(t, hold.Rollback(ctx))
			}
			var result receipt
			select {
			case result = <-deleted:
			case <-ctx.Done():
				t.Fatal("GC terminal missing; timeout is not committed deletion")
			}
			must(t, result.err)
			if result.n != 1 || len(result.keys) != 1 || result.keys[0] != m.RawObjectKey {
				t.Fatal("GC receipt did not identify committed exact source")
			}
			var messages, documents, jobs, count, restoreAudits, restoreOutbox int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE id=$1),(SELECT count(*) FROM mail_documents WHERE message_id=$1),(SELECT count(*) FROM mail_index_jobs WHERE message_id=$1),(SELECT message_count FROM mailboxes WHERE id=$2),(SELECT count(*) FROM audit_log WHERE action='message.restore' AND resource_id=$1),(SELECT count(*) FROM outbox_events WHERE (event_type='message.restore' AND payload->>'message_id'=$1::text) OR (event_type='company.admin.changed' AND payload->'metadata'->>'action'='message.restore' AND payload->'metadata'->>'resource_id'=$1::text))`, m.ID, mb.ID).Scan(&messages, &documents, &jobs, &count, &restoreAudits, &restoreOutbox))
			if messages != 0 || documents != 0 || jobs != 0 || count != 0 || restoreAudits != 0 || restoreOutbox != 0 {
				t.Fatal("restore loser or GC left partial source/derived/count/audit/outbox effects")
			}
			refs, e := f.st.CountRawObjectReferences(ctx, m.RawObjectKey)
			must(t, e)
			if refs != 0 {
				t.Fatal("physically deleted source retained ghost raw reference")
			}
		})
	}
}
