package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

// Parent KEY SHARE must not be replaced by a tenant-exclusive enqueue lock.
// Both real commands reach a test-only late INSERT barrier concurrently.
func TestR5ConcurrencyIndependentSubmissionsShareParentLock(t *testing.T) {
	f := seedCompany(t)
	_, _, _, first := r5TransferFixture(t, f)
	second := *first
	second.ID = uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate := r5PauseCommand(t, f, ctx, "outbound_jobs", "INSERT", "ROW")
	done := make(chan error, 2)
	go func() { done <- f.st.CreateOutboundJob(ctx, first) }()
	go func() { done <- f.st.CreateOutboundJob(ctx, &second) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiters int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND query LIKE '%INSERT INTO outbound_jobs%' AND $1::int=ANY(pg_blocking_pids(pid))`, int32(gate.Conn().PgConn().PID())).Scan(&waiters))
		if waiters == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("two independent submissions did not share the parent key lock")
		case <-ticker.C:
		}
	}
	must(t, gate.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	must(t, r5ConcurrentResult(t, ctx, done))
	var jobs, assets int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=ANY($1::uuid[])),(SELECT count(*) FROM sent_mail_assets WHERE id=ANY($1::uuid[]))`, []uuid.UUID{first.ID, second.ID}).Scan(&jobs, &assets))
	if jobs != 2 || assets != 2 {
		t.Fatal("parallel accepted submissions did not retain both archives")
	}
	t.Log("two enqueues shared tenant KEY SHARE and sender/attachment SHARE, then both committed with complete archives")
}
