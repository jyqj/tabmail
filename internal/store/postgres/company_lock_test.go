package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// Regression for the companyTx read/write lock split: mailbox reads and CAS
// writes run in companyReadTx without the tenants row lock, so ordinary
// company traffic never queues behind a management mutation (or behind the
// ingress quota lock). Administration itself keeps the company lock.
func TestCompanyReadPathDoesNotSerializeOnTenantLock(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()

	// An external company-lock holder: a concurrent administrative mutation
	// or a DeliverIngress quota section looks exactly like this to the store.
	conn, e := f.pool.Acquire(ctx)
	must(t, e)
	defer conn.Release()
	hold, e := conn.Begin(ctx)
	must(t, e)
	defer hold.Rollback(ctx)
	var id uuid.UUID
	must(t, hold.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID).Scan(&id))

	// A-class pure read must complete inside the timeout window: it does not
	// wait for the company lock.
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, _, e = f.st.ListWorkMessages(readCtx, f.u, f.personal.ID, "inbox", "", models.Page{}); e != nil {
		t.Fatalf("pure mailbox read blocked by company lock: %v", e)
	}

	// C-class management mutation must still serialize: it waits on the
	// company lock and only the context deadline unblocks it.
	writeCtx, wcancel := context.WithTimeout(ctx, 2*time.Second)
	defer wcancel()
	e = grantCurrent(f.st, writeCtx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true})
	if e == nil {
		t.Fatal("management mutation did not wait for the company lock")
	}
}
