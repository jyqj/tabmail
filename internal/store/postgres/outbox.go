package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *PgStore) CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	now := time.Now().UTC()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = now
	}
	if e.NextAttemptAt.IsZero() {
		e.NextAttemptAt = now
	}
	if e.State == "" {
		e.State = "pending"
	}
	e.CreatedAt = now
	e.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbox_events (id,event_type,payload,occurred_at,state,attempts,last_error,next_attempt_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ID, e.EventType, e.Payload, e.OccurredAt, e.State, e.Attempts, e.LastError, e.NextAttemptAt, e.CreatedAt, e.UpdatedAt)
	return err
}

func (s *PgStore) ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(claimLeaseDuration)
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM outbox_events
			WHERE (state IN ('pending','retry') AND next_attempt_at <= $1)
			   OR (state = 'processing' AND (lease_until IS NULL OR lease_until <= $1))
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events o
		SET state='processing', attempts=o.attempts + 1, claimed_at=$1, lease_until=$3, updated_at=$1
		FROM cte
		WHERE o.id = cte.id
		RETURNING o.id,o.event_type,o.payload,o.occurred_at,o.state,o.attempts,o.last_error,o.next_attempt_at,o.claimed_at,o.lease_until,o.created_at,o.updated_at`,
		now, limit, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.OutboxEvent
	for rows.Next() {
		e := &models.OutboxEvent{}
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.OccurredAt, &e.State, &e.Attempts, &e.LastError, &e.NextAttemptAt, &e.ClaimedAt, &e.LeaseUntil, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkOutboxEventDone(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox_events
		SET state='done', claimed_at=NULL, lease_until=NULL, updated_at=$2
		WHERE id=$1`, id, time.Now().UTC())
	return err
}

func (s *PgStore) MarkOutboxEventRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox_events
		SET state='retry', last_error=$2, next_attempt_at=$3, claimed_at=NULL, lease_until=NULL, updated_at=$4
		WHERE id=$1`, id, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}
