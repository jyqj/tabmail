package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
)

// The row locks protect these original bounds from concurrent changes. Read a
// fresh database clock after acquisition and after all required writes. This is
// the transaction's decision point, not a promise about COMMIT acknowledgement.
func checkSentItemDeadline(ctx context.Context, tx pgx.Tx, expiresAt, purgeAfter, mailboxExpiresAt *time.Time) error {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !sentItemAvailableAt(now, expiresAt, purgeAfter, mailboxExpiresAt) {
		return app.Conflict("sent item changed or unavailable; reload")
	}
	return nil
}

func sentItemAvailableAt(now time.Time, expiresAt, purgeAfter, mailboxExpiresAt *time.Time) bool {
	for _, deadline := range []*time.Time{expiresAt, purgeAfter, mailboxExpiresAt} {
		if deadline != nil && !deadline.After(now) {
			return false
		}
	}
	return true
}
