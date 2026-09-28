package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
)

// lockMailboxAuthorization orders a mailbox-authorized write against the
// existing grant/policy/handover commands, which update its revision row.
// Call after the current actor's SHARE fence and before reading mailbox rights
// or locking dependent rows. It does not lock permission profiles or overrides.
func lockMailboxAuthorization(ctx context.Context, tx pgx.Tx, tenant, mailbox uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM mailboxes WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, mailbox).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.NotFound("mailbox not found")
	}
	return err
}

// Claim the version before changing grants or policy, in the same transaction.
// A tenant lock orders company writers; the row CAS additionally rejects stale
// UI snapshots and protects against other writers that advance the revision.
func claimMailboxRevision(ctx context.Context, tx pgx.Tx, tenant, mailbox uuid.UUID, revision int64) error {
	if revision < 1 {
		return app.BadRequest("a positive mailbox revision is required")
	}
	tag, err := tx.Exec(ctx, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE tenant_id=$1 AND id=$2 AND lifecycle_revision=$3`, tenant, mailbox, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return app.Conflict("mailbox permissions or policy changed; reload and review before saving")
	}
	return nil
}
