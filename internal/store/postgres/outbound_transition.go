package postgres

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"time"
)

// finalizeOutbound is the single worker finalization write. State policy is
// shared with FakeStore, while concurrency authority stays in this one atomic
// database UPDATE. No read/compute/write window can bypass the live lease.
func (s *PgStore) finalizeOutbound(ctx context.Context, id uuid.UUID, token *uuid.UUID, event delivery.Finalization, reason string, next *time.Time, code *int, response, messageID *string) error {
	if token == nil {
		return ErrDeliveryTokenMismatch
	}
	normal, err := delivery.FinalState(event, false)
	if err != nil {
		return err
	}
	inFlight, flightErr := delivery.FinalState(event, true)
	if flightErr != nil {
		inFlight = normal
	}
	tag, err := s.pool.Exec(ctx, `UPDATE outbound_jobs SET
 state=(CASE WHEN in_flight_domain='' THEN $3::text ELSE $4::text END)::outbound_state,
 last_error=CASE WHEN $5::boolean AND in_flight_domain<>'' THEN $6::text||$7::text ELSE $7::text END,
 next_attempt_at=COALESCE($8::timestamptz,next_attempt_at),smtp_code=COALESCE($9::integer,smtp_code),
 smtp_response=COALESCE($10::text,smtp_response),message_id_header=COALESCE($11::text,message_id_header),
 claimed_at=NULL,lease_until=NULL,delivery_token=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND delivery_token=$2 AND state=$12::outbound_state AND lease_until>clock_timestamp()
 AND (NOT $13::boolean OR in_flight_domain='')`, id, *token, string(normal), string(inFlight), event == delivery.FinishRetry,
		delivery.UncertainRetryPrefix, boundedIngressError(reason), next, code, response, messageID, models.OutboundProcessing, flightErr != nil)
	return requireDeliveryUpdate(tag, err)
}
