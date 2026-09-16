package postgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

func (s *PgStore) FindOutboundSubmission(ctx context.Context, tenant uuid.UUID, actor, key, hash string) (*models.OutboundJob, error) {
	if key == "" {
		return nil, nil
	}
	old, e := scanOutboundJob(s.pool.QueryRow(ctx, outboundJobSelect+` WHERE tenant_id=$1 AND submit_actor=$2 AND idempotency_key=$3`, tenant, actor, key))
	if e != nil {
		return nil, e
	}
	if old != nil && old.RequestHash != hash {
		return nil, app.Conflict("idempotency key was already used for a different request")
	}
	return old, nil
}

// Mark uncertainty BEFORE touching the network. A killed worker leaves explicit
// evidence, not a falsely retryable recipient. Legacy jobs keep domain fencing.
func (s *PgStore) BeginOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address string) (bool, error) {
	if token == nil || address == "" {
		return false, ErrDeliveryTokenMismatch
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var valid bool
	e = tx.QueryRow(ctx, `SELECT true FROM outbound_jobs WHERE id=$1 AND delivery_token=$2 AND state='processing' AND lease_until>clock_timestamp() AND in_flight_domain='' AND recipient_ledger FOR UPDATE`, id, *token).Scan(&valid)
	if e == pgx.ErrNoRows {
		return false, ErrDeliveryTokenMismatch
	}
	if e != nil {
		return false, e
	}
	tag, e := tx.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain',attempts=attempts+1,diagnostic='SMTP attempt started; outcome pending',updated_at=clock_timestamp() WHERE job_id=$1 AND address=$2 AND state IN ('pending','temporary')`, id, address)
	if e != nil {
		return false, e
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, e = tx.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain=$2,updated_at=clock_timestamp() WHERE id=$1`, id, "rcpt:"+address); e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
func (s *PgStore) CompleteOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address, state string, code int, diagnostic string) error {
	if token == nil || (state != "accepted" && state != "temporary" && state != "permanent") {
		return ErrDeliveryTokenMismatch
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='',updated_at=clock_timestamp() WHERE id=$1 AND delivery_token=$2 AND state='processing' AND lease_until>clock_timestamp() AND in_flight_domain=$3 AND recipient_ledger`, id, *token, "rcpt:"+address)
	if e = requireDeliveryUpdate(tag, e); e != nil {
		return e
	}
	tag, e = tx.Exec(ctx, `UPDATE outbound_recipients SET state=$3,smtp_code=$4,diagnostic=$5,updated_at=clock_timestamp() WHERE job_id=$1 AND address=$2 AND state='uncertain'`, id, address, state, code, boundedIngressError(strings.ToValidUTF8(diagnostic, "?")))
	if e = requireDeliveryUpdate(tag, e); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
