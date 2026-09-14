package postgres

import (
	"context"
	"github.com/google/uuid"
	"strings"
)

func (s *PgStore) BeginOutboundDomain(ctx context.Context, id uuid.UUID, token *uuid.UUID, domain string) error {
	if token == nil || strings.TrimSpace(domain) == "" {
		return ErrDeliveryTokenMismatch
	}
	tag, err := s.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain=$3,updated_at=clock_timestamp()
 WHERE id=$1 AND delivery_token=$2 AND state='processing' AND lease_until>clock_timestamp()
 AND in_flight_domain='' AND NOT ($3=ANY(delivered_domains))`, id, *token, domain)
	return requireDeliveryUpdate(tag, err)
}
func (s *PgStore) CompleteOutboundDomain(ctx context.Context, id uuid.UUID, token *uuid.UUID, domain string, accepted bool) error {
	if token == nil {
		return ErrDeliveryTokenMismatch
	}
	tag, err := s.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='',updated_at=clock_timestamp(),
 delivered_domains=CASE WHEN $4 AND NOT($3=ANY(delivered_domains)) THEN array_append(delivered_domains,$3) ELSE delivered_domains END
 WHERE id=$1 AND delivery_token=$2 AND state='processing' AND lease_until>clock_timestamp() AND in_flight_domain=$3`, id, *token, domain, accepted)
	return requireDeliveryUpdate(tag, err)
}
