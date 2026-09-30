package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The registered ConvertShared route calls this admin/revision-gated command.
// Create a real ownerless legacy mailbox, not a company mailbox with its owner
// removed. Its active TTL source is eligible before conversion, not after it.
func TestR5ConvertSharedRetentionCompleteCommands(t *testing.T) {
	for _, order := range []string{"conversion-first", "retention-first"} {
		t.Run(order, func(t *testing.T) {
			f, doc, revision, cutoff := r5ConvertRetentionFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			before := r5MaintenanceState(t, f, doc.MessageID)
			metadata := r5ConvertMetadata(t, f)
			convertedMetadata, convertedAssets := r5ConvertExpected(t, f, doc.MessageID)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			conversionDone := make(chan error, 1)
			type receipt struct {
				n    int
				keys []string
				err  error
			}
			retentionDone := make(chan receipt, 1)
			convert := func() {
				conversionDone <- f.st.ConvertSharedMailbox(ctx, f.a, f.personal.ID, revision, "documented conversion for retention")
			}
			retain := func() {
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
				retentionDone <- receipt{n, keys, e}
			}
			if order == "conversion-first" {
				name := "r5_convert_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				key := "r5-convert-gate:" + f.personal.ID.String()
				_, err = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_id='`+f.personal.ID.String()+`'::uuid AND NEW.action='mailbox.convert_shared' THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+key+`',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
				must(t, err)
				_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
				must(t, err)
				go convert()
				conversionPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
				go retain()
				retentionPID := r5WaitBlockedBy(t, f, ctx, conversionPID, "DELETE FROM messages")
				r5MaintenanceTrace(t, f, ctx, hold.Conn().PgConn().PID(), conversionPID, retentionPID)
			} else {
				_, err = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, doc.MessageID)
				must(t, err)
				go retain()
				retentionPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
				go convert()
				r5MaintenanceWaitOrConflict(t, f, ctx, hold.Conn().PgConn().PID(), retentionPID, conversionDone)
			}
			must(t, hold.Rollback(ctx))
			conversionErr := r5ConcurrentResult(t, ctx, conversionDone)
			var result receipt
			select {
			case result = <-retentionDone:
			case <-ctx.Done():
				t.Fatal("retention terminal missing; timeout is not deadlock evidence")
			}
			conversionDead := r5SQLState(conversionErr) == "40P01"
			retentionDead := r5SQLState(result.err) == "40P01"
			if result.n < 0 || result.n > 1 || len(result.keys) != result.n || (result.n == 1 && result.keys[0] != doc.SourceKey) {
				t.Fatal("retention returned a lost or noncommitted exact-source receipt")
			}
			conversionEffects, sourceChanged := 0, 0
			if conversionErr == nil {
				conversionEffects = 1
				if r5ConvertMetadata(t, f) != convertedMetadata {
					t.Fatal("successful conversion has incorrect kind/access/retention/password/revision metadata")
				}
				if result.n == 0 || order == "conversion-first" {
					sourceChanged = 1
				}
			} else {
				if !conversionDead {
					e, ok := app.As(app.FromAuthz(conversionErr))
					if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindNotFound) {
						t.Fatalf("unexpected conversion terminal SQLSTATE=%s err=%v", r5SQLState(conversionErr), conversionErr)
					}
				}
				if r5ConvertMetadata(t, f) != metadata {
					t.Fatal("failed conversion retained mailbox metadata or revision changes")
				}
			}
			if result.err != nil && !retentionDead {
				t.Fatalf("unexpected retention terminal SQLSTATE=%s err=%v", r5SQLState(result.err), result.err)
			}
			if conversionErr != nil && result.err != nil {
				t.Fatal("neither complete command committed")
			}
			r5ConvertState(t, f, doc.MessageID, before, 1-result.n, conversionEffects, sourceChanged, result.n)
			r5RestoreReferences(t, f, ctx, doc.SourceKey, 1-result.n)
			if conversionErr == nil && result.n == 0 {
				if r5MaintenanceAssets(t, f, doc.MessageID) != convertedAssets {
					t.Fatal("conversion altered source/document/index bytes or lease beyond clearing active expiry")
				}
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
				must(t, e)
				if n != 0 || len(keys) != 0 {
					t.Fatal("fresh retention deleted newly permanent shared source")
				}
			}
			if order == "conversion-first" && conversionErr == nil && result.n == 1 {
				t.Error("old retention candidate deleted source after successful shared conversion cleared expiry")
			}
			// Retention winning first may legitimately leave an empty mailbox
			// that converts successfully. Do not invent a missing-source error.
			if conversionDead || retentionDead {
				t.Errorf("actual conversion/retention 40P01; metadata/source/doc/index/count/audit/outbox/event and receipts verified: conversion=%s retention=%s", r5SQLState(conversionErr), r5SQLState(result.err))
			}
		})
	}
}

func TestR5ConvertSharedRetentionNormalAndAuditRollback(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared-permanent", true: "audit-failure"}[failAudit], func(t *testing.T) {
			f, doc, revision, cutoff := r5ConvertRetentionFixture(t)
			ctx := context.Background()
			before := r5MaintenanceState(t, f, doc.MessageID)
			metadata, assets := r5ConvertMetadata(t, f), r5MaintenanceAssets(t, f, doc.MessageID)
			expectedMetadata, expectedAssets := r5ConvertExpected(t, f, doc.MessageID)
			if failAudit {
				_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_convert_audit_failure CHECK(action<>'mailbox.convert_shared') NOT VALID`)
				must(t, err)
				if err = f.st.ConvertSharedMailbox(ctx, f.a, f.personal.ID, revision, "documented audit failure conversion"); err == nil {
					t.Fatal("required conversion audit failure ignored")
				}
				if r5ConvertMetadata(t, f) != metadata || r5MaintenanceAssets(t, f, doc.MessageID) != assets {
					t.Fatal("failed audit retained conversion metadata/deadlines/document/index changes")
				}
				r5ConvertState(t, f, doc.MessageID, before, 1, 0, 0, 0)
				r5RestoreReferences(t, f, ctx, doc.SourceKey, 1)
				_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_convert_audit_failure`)
				must(t, err)
			}
			must(t, f.st.ConvertSharedMailbox(ctx, f.a, f.personal.ID, revision, "documented normal shared conversion"))
			if r5ConvertMetadata(t, f) != expectedMetadata || r5MaintenanceAssets(t, f, doc.MessageID) != expectedAssets {
				t.Fatal("normal conversion has incorrect metadata/source/document/index transition")
			}
			n, keys, err := f.st.DeleteExpiredMessagesReturningKeys(ctx, cutoff, 100)
			must(t, err)
			if n != 0 || len(keys) != 0 {
				t.Fatal("normal shared conversion lost permanent source or produced deletion receipt")
			}
			r5ConvertState(t, f, doc.MessageID, before, 1, 1, 1, 0)
			r5RestoreReferences(t, f, ctx, doc.SourceKey, 1)
		})
	}
}

func r5ConvertRetentionFixture(t *testing.T) (*companyFixture, company.ParsedMessage, int64, time.Time) {
	t.Helper()
	f := seedCompany(t)
	ctx := context.Background()
	hours, password, mailboxExpiry := 24, "test-only-legacy-password-hash", time.Now().Add(24*time.Hour)
	mb := &models.Mailbox{TenantID: f.tenant.ID, ZoneID: f.zone.ID, LocalPart: "convert-legacy", ResolvedDomain: f.zone.Domain, FullAddress: "convert-legacy@" + f.zone.Domain, Kind: "legacy", AccessMode: models.AccessToken, PasswordHash: &password, RetentionHoursOverride: &hours, ExpiresAt: &mailboxExpiry}
	must(t, f.st.CreateMailbox(ctx, mb))
	f.personal = mb
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true}))
	access, err := f.st.GetWorkMailbox(ctx, f.a, mb.ID)
	must(t, err)
	if !access.CanManage || access.Mailbox.Kind != "legacy" || access.Mailbox.OwnerUserID != nil {
		t.Fatal("fixture is not an authorized ownerless legacy conversion caller")
	}
	_, doc := r5IndexFixture(t, f, true)
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	_, err = f.pool.Exec(ctx, `UPDATE messages SET expires_at=$2::timestamptz-interval '1 hour',deleted_at=NULL,purge_after=NULL WHERE id=$1`, doc.MessageID, cutoff)
	must(t, err)
	return f, doc, access.Revision, cutoff
}

func r5ConvertMetadata(t *testing.T, f *companyFixture) string {
	t.Helper()
	var metadata string
	must(t, f.pool.QueryRow(context.Background(), `SELECT (to_jsonb(m)-'message_count')::text FROM mailboxes m WHERE id=$1`, f.personal.ID).Scan(&metadata))
	return metadata
}

func r5ConvertExpected(t *testing.T, f *companyFixture, source uuid.UUID) (string, string) {
	t.Helper()
	var metadata, assets string
	must(t, f.pool.QueryRow(context.Background(), `SELECT (to_jsonb(m)-'message_count'||jsonb_build_object('mailbox_kind','shared','access_mode','token','expires_at',NULL,'retention_hours_override',0,'password_hash',NULL,'lifecycle_revision',m.lifecycle_revision+1))::text FROM mailboxes m WHERE id=$1`, f.personal.ID).Scan(&metadata))
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('source',(SELECT to_jsonb(m)||jsonb_build_object('expires_at',NULL) FROM messages m WHERE id=$1),'document',(SELECT to_jsonb(d) FROM mail_documents d WHERE message_id=$1),'index',(SELECT to_jsonb(j) FROM mail_index_jobs j WHERE message_id=$1))::text`, source).Scan(&assets))
	return metadata, assets
}

func r5ConvertState(t *testing.T, f *companyFixture, id uuid.UUID, before [7]int, sources, conversions, changed, deleted int) {
	t.Helper()
	want := [7]int{sources, sources, sources, sources, before[4] + conversions, before[5] + conversions, before[6] + changed + deleted}
	if got := r5MaintenanceState(t, f, id); got != want {
		t.Fatalf("conversion source/doc/index/count/audit/outbox/event atomicity: got=%v want=%v", got, want)
	}
}
