//go:build r5fixtures

package testpg_test

import (
	"context"
	"tabmail/internal/testpg"
	"testing"
	"time"
)

func TestR5FixtureExactBlockingBarrierAndRealClock(t *testing.T) {
	f := testpg.NewR5Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	before, err := testpg.R5DatabaseNow(ctx, f.Pool)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := f.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	_, err = holder.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.Companies[0].Tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := f.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	finished := make(chan struct{})
	defer func() {
		stopWorker()
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		select {
		case <-finished:
		case <-cleanup.Done():
			t.Error("owned PostgreSQL waiter did not terminate after cancellation")
		}
		conn.Release()
	}()
	done := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := conn.Exec(workerCtx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE`, f.Companies[0].Tenant.ID)
		done <- err
	}()
	if err = testpg.R5WaitBlockedBy(ctx, f.Pool, conn.Conn().PgConn().PID(), holder.Conn().PgConn().PID()); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	after, err := testpg.R5DatabaseNow(ctx, f.Pool)
	if err != nil || after.Before(before) {
		t.Fatal("authoritative DB clock observation failed")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err = testpg.R5WaitBlockedBy(canceled, f.Pool, conn.Conn().PgConn().PID(), holder.Conn().PgConn().PID()); err == nil {
		t.Fatal("canceled barrier falsely reported ready")
	}
}
