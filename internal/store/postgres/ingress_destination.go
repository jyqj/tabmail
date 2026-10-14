package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fenceIngressDestination binds the fixed receipt tuple to its current receive
// eligibility. Read/send grants are deliberately irrelevant to operator retry
// and durable receiving. NOWAIT avoids reverse-order waits against domain or
// mailbox management; a natural expiry still needs the final DB-time fence.
func fenceIngressDestination(ctx context.Context, tx pgx.Tx, tenant, zone, mailbox uuid.UUID, address string) (*time.Time, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM domain_zones WHERE id=$1 AND tenant_id=$2 AND is_verified AND mx_verified FOR SHARE NOWAIT`, zone, tenant).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var deadline *time.Time
	err = tx.QueryRow(ctx, `SELECT id,expires_at FROM mailboxes WHERE id=$1 AND tenant_id=$2 AND zone_id=$3 AND full_address=$4 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE NOWAIT`, mailbox, tenant, zone, address).Scan(&id, &deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return deadline, err == nil, err
}

func ingressDestinationsAlive(ctx context.Context, tx pgx.Tx, deadlines []*time.Time) (bool, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return false, err
	}
	for _, deadline := range deadlines {
		if deadline != nil && !deadline.After(now) {
			return false, nil
		}
	}
	return true, nil
}
