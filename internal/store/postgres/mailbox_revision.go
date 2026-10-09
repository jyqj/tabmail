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

// transferOwnedMailboxes moves every mailbox owned by the departing member to
// the successor during offboarding. It takes the same companyTx tenant lock as
// the single-mailbox commands, then locks the owned rows in a stable
// `ORDER BY id` scan so no reverse order is introduced against handover or
// grant writers, and advances each revision through claimMailboxRevision —
// offboarding never increments lifecycle_revision with its own statement.
func transferOwnedMailboxes(ctx context.Context, tx pgx.Tx, tenant, from, to uuid.UUID) error {
	type ownedMailbox struct {
		id       uuid.UUID
		revision int64
	}
	rows, err := tx.Query(ctx, `SELECT id,lifecycle_revision FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2 ORDER BY id FOR UPDATE`, tenant, from)
	if err != nil {
		return err
	}
	var batch []ownedMailbox
	for rows.Next() {
		var m ownedMailbox
		if err := rows.Scan(&m.id, &m.revision); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, m := range batch {
		// The rows are FOR UPDATE-locked, so the CAS below always matches;
		// routing through it keeps the revision increment in one place.
		if err := claimMailboxRevision(ctx, tx, tenant, m.id, m.revision); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2 WHERE tenant_id=$1 AND id=$3`, tenant, to, m.id); err != nil {
			return err
		}
	}
	return nil
}
