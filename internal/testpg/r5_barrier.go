package testpg

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// R5DatabaseNow observes the authoritative clock; never substitutes a fake clock.
func R5DatabaseNow(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	if pool == nil {
		return time.Time{}, errors.New("R5 clock requires a real PostgreSQL pool")
	}
	var now time.Time
	err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

// R5WaitBlockedBy returns only after PostgreSQL reports the exact wait edge.
// Poll pacing is not a readiness assumption; deadline/cancellation is an error.
func R5WaitBlockedBy(ctx context.Context, pool *pgxpool.Pool, waiter, blocker uint32) error {
	if pool == nil || waiter == 0 || blocker == 0 || waiter == blocker {
		return errors.New("R5 barrier requires distinct real backend PIDs")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("R5 barrier requires a hard context deadline")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT $2::integer=ANY(pg_blocking_pids($1::integer))`, int32(waiter), int32(blocker)).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
