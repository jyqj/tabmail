package postgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The complete ledger is outcome evidence only. Ordinary receipts never carry
// addresses, even public To/CC, so classification/content authority cannot
// accidentally widen this operation projection.
type submissionReceiptRecipients struct{ ledgerStates []string }

func loadSubmissionReceiptRecipients(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]submissionReceiptRecipients, error) {
	out := map[uuid.UUID]submissionReceiptRecipients{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT job_id,state FROM outbound_recipients WHERE tenant_id=$1 AND job_id=ANY($2) ORDER BY job_id,address`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, err
		}
		r := out[id]
		r.ledgerStates = append(r.ledgerStates, state)
		out[id] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
