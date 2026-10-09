package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// ListCompanyAdminEvents reads the original durable outbox without claiming,
// acknowledging or changing dispatcher ownership. This is a bounded recent
// window, NOT a commit-ordered cursor: even created_at and UUID order cannot
// exclude late commits. Consumers must periodically reload authoritative data.
// The same current interactive administrator guard used by CompanyOverview
// applies on every read, including empty windows and reconnects.
func (s *PgStore) ListCompanyAdminEvents(ctx context.Context, actor authz.Actor, limit int) ([]models.OutboxEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	out := []models.OutboxEvent{}
	err := s.companyReadTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		// Project only invalidation metadata here and again at the transport.
		// No state filter: dispatcher ack/retry does not consume an SSE event.
		rows, err := tx.Query(ctx, `SELECT id,event_type,jsonb_build_object(
		 'type',event_type,'tenant_id',payload->>'tenant_id',
		 'occurred_at',payload->'occurred_at','metadata',jsonb_build_object(
		 'action',payload->'metadata'->'action',
		 'resource_type',payload->'metadata'->'resource_type',
		 'resource_id',payload->'metadata'->'resource_id'))
		 FROM outbox_events WHERE event_type='company.admin.changed'
		 AND payload->>'tenant_id'=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, current.TenantID.String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event models.OutboxEvent
			if err := rows.Scan(&event.ID, &event.EventType, &event.Payload); err != nil {
				return err
			}
			out = append(out, event)
		}
		return rows.Err()
	})
	return out, err
}

// WithCompanyAdminEventAccess fences one bounded frame release against current
// role/session changes. The caller MUST complete both write and flush inside
// emit; buffering a write and flushing after Commit would defeat this fence.
// Only the current user's FOR SHARE lock is held, never the tenant write lock.
// Emit errors/cancellation roll back and release the user fence.
func (s *PgStore) WithCompanyAdminEventAccess(ctx context.Context, actor authz.Actor, emit func() error) error {
	return s.companyReadTx(ctx, actor, true, func(_ pgx.Tx, _ authz.Actor) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return emit()
	})
}
