package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
)

// AR09: every mailbox lifecycle_revision increment must flow through
// claimMailboxRevision. Handover, shared conversion and the offboarding batch
// previously carried their own inline rev+1 statements; these tests pin the
// routed behavior and the offboarding lock order.

func ar09Revision(t *testing.T, f *companyFixture, id uuid.UUID) int64 {
	t.Helper()
	var r int64
	must(t, f.pool.QueryRow(context.Background(), `SELECT lifecycle_revision FROM mailboxes WHERE id=$1`, id).Scan(&r))
	return r
}

func TestAR09HandoverAndConvertClaimRevisionThroughHelper(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()

	before := ar09Revision(t, f, f.personal.ID)
	must(t, f.st.TransferWorkMailbox(ctx, f.a, f.personal.ID, f.other.ID, before, "documented handover reason"))
	if got := ar09Revision(t, f, f.personal.ID); got != before+1 {
		t.Fatalf("handover revision=%d, want exactly %d", got, before+1)
	}
	var owner *uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
	if owner == nil || *owner != f.other.ID {
		t.Fatal("handover did not move ownership")
	}
	if f.st.TransferWorkMailbox(ctx, f.a, f.personal.ID, f.employee.ID, before, "stale revision handover") == nil {
		t.Fatal("stale revision accepted")
	}
	if got := ar09Revision(t, f, f.personal.ID); got != before+1 {
		t.Fatalf("rejected handover advanced revision to %d", got)
	}

	// Conversion claims the revision of an ownerless legacy mailbox through
	// the same helper while rewriting only business fields.
	_, err := f.pool.Exec(ctx, `UPDATE mailboxes SET mailbox_kind='legacy',access_mode='token',password_hash='legacy-hash' WHERE id=$1`, f.shared.ID)
	must(t, err)
	cbefore := ar09Revision(t, f, f.shared.ID)
	must(t, f.st.ConvertSharedMailbox(ctx, f.a, f.shared.ID, cbefore, "documented convert reason"))
	var kind, access string
	var passwordHash *string
	var retentionHours int
	must(t, f.pool.QueryRow(ctx, `SELECT mailbox_kind,access_mode,password_hash,retention_hours_override FROM mailboxes WHERE id=$1`, f.shared.ID).Scan(&kind, &access, &passwordHash, &retentionHours))
	if got := ar09Revision(t, f, f.shared.ID); got != cbefore+1 {
		t.Fatalf("convert revision=%d, want exactly %d", got, cbefore+1)
	}
	if kind != "shared" || access != "token" || passwordHash != nil || retentionHours != 0 {
		t.Fatalf("convert business fields wrong: kind=%s access=%s hash=%v retention=%d", kind, access, passwordHash, retentionHours)
	}
	if f.st.ConvertSharedMailbox(ctx, f.a, f.shared.ID, cbefore, "stale convert attempt") == nil {
		t.Fatal("stale convert accepted")
	}
	if got := ar09Revision(t, f, f.shared.ID); got != cbefore+1 {
		t.Fatalf("rejected convert advanced revision to %d", got)
	}
}

// The offboarding batch must lock the departing member's mailboxes in stable
// id order and route each revision increment through the shared claim CAS; it
// must not regress to one bulk UPDATE mixing owner and revision fields.
func TestAR09OffboardingLocksOwnedMailboxesInIdOrder(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	workbench, e := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "workbench", Kind: "personal", OwnerUserID: &f.employee.ID})
	must(t, e)
	owned := []uuid.UUID{f.personal.ID, workbench.ID}
	lower, higher := owned[0], owned[1]
	if higher.String() < lower.String() {
		lower, higher = higher, lower
	}
	before := map[uuid.UUID]int64{}
	for _, id := range owned {
		before[id] = ar09Revision(t, f, id)
	}

	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	// Block only the higher id. The worker must acquire the lower id before
	// waiting here; holding both ourselves could not prove its ordering.
	_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, higher)
	must(t, e)
	done := make(chan error, 1)
	go func() {
		done <- f.st.OffboardEmployee(ctx, f.a, f.employee.ID, f.other.ID, "Employee departure and mailbox handover")
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "ORDER BY id FOR UPDATE")

	assertLocked := func(query string, id uuid.UUID) {
		t.Helper()
		// NOWAIT failure aborts its transaction. Every probe therefore gets a
		// fresh transaction and unconditional release, including t.Fatal paths.
		probe, err := f.pool.Begin(ctx)
		must(t, err)
		defer probe.Rollback(context.Background())
		_, err = probe.Exec(ctx, query, id)
		r5RequireLockConflict(t, err)
	}
	assertLocked(`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.employee.ID)
	assertLocked(`SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, lower)
	t.Log("observed worker waiting on higher mailbox while holding user and lower mailbox; each NOWAIT probe released independently")
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)

	for _, id := range owned {
		if got := ar09Revision(t, f, id); got != before[id]+1 {
			t.Fatalf("mailbox %s revision=%d, want exactly %d", id, got, before[id]+1)
		}
		var moved *uuid.UUID
		must(t, f.pool.QueryRow(context.Background(), `SELECT owner_user_id FROM mailboxes WHERE id=$1`, id).Scan(&moved))
		if moved == nil || *moved != f.other.ID {
			t.Fatalf("mailbox %s was not moved to the successor", id)
		}
	}
	var active bool
	var audits int
	must(t, f.pool.QueryRow(context.Background(), `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
	must(t, f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='employee.offboard'`, f.tenant.ID).Scan(&audits))
	if active || audits != 1 {
		t.Fatalf("offboarding atomicity degraded: active=%v audits=%d", active, audits)
	}
}
