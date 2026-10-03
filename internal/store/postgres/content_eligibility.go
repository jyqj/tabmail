package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"tabmail/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
)

// receivedContentEligible is the single received-entry lifecycle predicate.
// Alias m is internal, never request input. A recorded deadline is a snapshot,
// not recalculated from a current policy. NULL creates no deadline and proves
// no perpetual grant. Personal historical hard-expiry is the existing owner
// exemption; purge is independent even for permanent personal mailboxes.
// Physical GC's additional shared COALESCE(override,0)=0 protection is NOT a
// content grant. It remains untouched in the retention/GC implementation.
const receivedContentEligible = `(m.purge_after IS NULL OR m.purge_after>clock_timestamp())
 AND EXISTS(SELECT 1 FROM mailboxes content_mb
  WHERE content_mb.tenant_id=m.tenant_id AND content_mb.id=m.mailbox_id
   AND (content_mb.expires_at IS NULL OR content_mb.expires_at>clock_timestamp())
   AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp() OR content_mb.owner_user_id IS NOT NULL))`

// No owner/retention exemption can revive a mailbox after its own deadline.
func requireReceivedMailboxLiveTx(ctx context.Context, tx pgx.Tx, tenant, mailbox uuid.UUID) error {
	var live bool
	err := tx.QueryRow(ctx, `SELECT expires_at IS NULL OR expires_at>clock_timestamp() FROM mailboxes WHERE tenant_id=$1 AND id=$2`, tenant, mailbox).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.NotFound("mailbox not found")
	}
	if err != nil {
		return err
	}
	if !live {
		return app.NotFound("mailbox not found")
	}
	return nil
}

// Call only inside companyReadTx, after its current-member SHARE fence. The
// mailbox fence serializes rights with grant/handover writers. Every deadline
// statement runs after this potentially blocking fence and uses actual DB time,
// not transaction now() or a predicate evaluated before the lock wait.
func (s *PgStore) authorizeReceivedMailboxTx(ctx context.Context, tx pgx.Tx, a authz.Actor, mailbox uuid.UUID) error {
	if err := lockMailboxAuthorization(ctx, tx, a.TenantID, mailbox); err != nil {
		return err
	}
	if err := requireReceivedMailboxLiveTx(ctx, tx, a.TenantID, mailbox); err != nil {
		return err
	}
	access, err := s.mailboxAccessTx(ctx, tx, a, mailbox)
	if err != nil {
		return err
	}
	if !access.CanRead {
		return app.Forbidden("mailbox read permission required")
	}
	return nil
}

// A valid entry can legitimately lack a parsed cache, but an unavailable entry
// must never be conflated with cache miss and trigger parser/object fallback.
// This statement is also used AFTER SaveParsedMessage acquires its source lock.
func requireReceivedContentTx(ctx context.Context, tx pgx.Tx, tenant, mailbox, id uuid.UUID) error {
	var found uuid.UUID
	err := tx.QueryRow(ctx, `SELECT m.id FROM messages m WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.id=$3 AND `+receivedContentEligible, tenant, mailbox, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.NotFound("message not found")
	}
	return err
}

// Count and page consume one materialized eligible set, AFTER all statement
// relation-lock waits (including message_user_states). An earlier count must
// not survive a later page wait past a deadline. The projected DTO is explicit:
// no raw key or incidental database column is released by this helper.
// All SQL fragments and placeholders are caller-owned, never request input.
func readReceivedPageTx(ctx context.Context, tx pgx.Tx, filter, viewer, limit, offset, order string, args ...any) ([]*models.Message, int, error) {
	const projection = `jsonb_build_object(
  'id',m.id,'tenant_id',m.tenant_id,'mailbox_id',m.mailbox_id,'zone_id',m.zone_id,
  'sender',m.sender,'recipients',m.recipients,'subject',m.subject,'size',m.size,
  'seen',COALESCE(mus.seen,m.seen),'starred',COALESCE(mus.starred,false),
  'headers',m.headers_json,'received_at',m.received_at,'expires_at',m.expires_at,
  'otp_code',m.otp_code,'otp_confidence',m.otp_confidence,'deleted_at',m.deleted_at,
  'purge_after',m.purge_after,'archived_at',m.archived_at)`
	query := `WITH eligible AS MATERIALIZED(SELECT m.id,m.received_at FROM messages m WHERE ` + filter + `),
 paged AS(SELECT ` + projection + ` AS content,e.received_at,e.id FROM eligible e
  JOIN messages m ON m.id=e.id LEFT JOIN message_user_states mus
   ON mus.tenant_id=m.tenant_id AND mus.mailbox_id=m.mailbox_id AND mus.message_id=m.id AND mus.user_id=` + viewer + `
  ORDER BY e.received_at ` + order + `,e.id ` + order + ` LIMIT ` + limit + ` OFFSET ` + offset + `)
 SELECT (SELECT count(*) FROM eligible),COALESCE(jsonb_agg(content ORDER BY received_at ` + order + `,id ` + order + `),'[]'::jsonb) FROM paged`
	var total int
	var data []byte
	if err := tx.QueryRow(ctx, query, args...).Scan(&total, &data); err != nil {
		return nil, 0, err
	}
	out := []*models.Message{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, 0, err
	}
	for _, m := range out {
		if string(m.HeadersJSON) == "null" {
			m.HeadersJSON = nil
		}
	}
	return out, total, nil
}
