package postgres

import (
	"context"
	"fmt"
	"time"

	"tabmail/internal/models"

	"github.com/google/uuid"
)

// ================================================================
// Queue: outbox events, webhook deliveries, ingest jobs
// ================================================================

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

func (s *PgStore) CreateWebhookDeliveries(ctx context.Context, event *models.OutboxEvent, urls []string) error {
	if event == nil || len(urls) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	for _, url := range urls {
		if _, err := tx.Exec(ctx, `
			INSERT INTO webhook_deliveries (id,event_id,url,event_type,payload,state,attempts,next_attempt_at,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,'pending',0,$6,$6,$6)
			ON CONFLICT (event_id, url) DO NOTHING`,
			uuid.New(), event.ID, url, event.EventType, event.Payload, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PgStore) ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(claimLeaseDuration)
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM webhook_deliveries
			WHERE (state IN ('pending','retry') AND next_attempt_at <= $1)
			   OR (state = 'processing' AND (lease_until IS NULL OR lease_until <= $1))
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_deliveries d
		SET state='processing', attempts=d.attempts + 1, claimed_at=$1, lease_until=$3, last_tried_at=$1, updated_at=$1
		FROM cte
		WHERE d.id = cte.id
		RETURNING d.id,d.event_id,d.url,d.event_type,d.payload,d.state,d.attempts,d.last_error,d.next_attempt_at,d.claimed_at,d.lease_until,d.last_tried_at,d.delivered_at,d.created_at,d.updated_at`,
		now, limit, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.WebhookDelivery
	for rows.Next() {
		d := &models.WebhookDelivery{}
		if err := rows.Scan(&d.ID, &d.EventID, &d.URL, &d.EventType, &d.Payload, &d.State, &d.Attempts, &d.LastError, &d.NextAttemptAt, &d.ClaimedAt, &d.LeaseUntil, &d.LastTriedAt, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkWebhookDeliveryDone(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET state='delivered', delivered_at=$2, claimed_at=NULL, lease_until=NULL, updated_at=$2
		WHERE id=$1`, id, now)
	return err
}

func (s *PgStore) MarkWebhookDeliveryRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET state=$2, last_error=$3, next_attempt_at=$4, claimed_at=NULL, lease_until=NULL, updated_at=$5
		WHERE id=$1`, id, state, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}

func (s *PgStore) ListDeadWebhookDeliveries(ctx context.Context, limit int) ([]models.DeadLetter, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,url,event_type,payload,attempts,last_error,created_at,last_tried_at
		FROM webhook_deliveries
		WHERE state='dead'
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.DeadLetter, 0, limit)
	for rows.Next() {
		var dl models.DeadLetter
		var id uuid.UUID
		var lastTriedAt *time.Time
		if err := rows.Scan(&id, &dl.URL, &dl.EventType, &dl.Payload, &dl.Attempts, &dl.LastError, &dl.CreatedAt, &lastTriedAt); err != nil {
			return nil, err
		}
		dl.ID = id.String()
		if lastTriedAt != nil {
			dl.LastTriedAt = *lastTriedAt
		}
		out = append(out, dl)
	}
	return out, rows.Err()
}

func (s *PgStore) CountDeadWebhookDeliveries(ctx context.Context) (int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE state='dead'`).Scan(&total)
	return total, err
}

func (s *PgStore) ListWebhookDeliveries(ctx context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if state != "" {
		add("state =", state)
	}
	if eventType != "" {
		add("event_type =", eventType)
	}
	if url != "" {
		add("url ILIKE", "%"+url+"%")
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`+where, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(filters, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx, `
		SELECT id,event_id,url,event_type,payload,state,attempts,last_error,next_attempt_at,claimed_at,lease_until,last_tried_at,delivered_at,created_at,updated_at
		FROM webhook_deliveries`+where+fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.WebhookDelivery
	for rows.Next() {
		item := &models.WebhookDelivery{}
		if err := rows.Scan(&item.ID, &item.EventID, &item.URL, &item.EventType, &item.Payload, &item.State, &item.Attempts, &item.LastError, &item.NextAttemptAt, &item.ClaimedAt, &item.LeaseUntil, &item.LastTriedAt, &item.DeliveredAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CountWebhookDeliveriesByState(ctx context.Context, states ...string) (int, error) {
	if len(states) == 0 {
		var total int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&total)
		return total, err
	}
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE state = ANY($1)`, states).Scan(&total)
	return total, err
}

func (s *PgStore) CreateIngestJob(ctx context.Context, job *models.IngestJob, ensureObject func(context.Context) error) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := time.Now().UTC()
	if job.NextAttemptAt.IsZero() {
		job.NextAttemptAt = now
	}
	if job.State == "" {
		job.State = "pending"
	}
	job.CreatedAt = now
	job.UpdatedAt = now

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Serialize against raw-object GC on the same content key and re-ensure the
	// object exists before committing the pending job that references it.
	if job.RawObjectKey != "" {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, job.RawObjectKey); err != nil {
			return err
		}
		if ensureObject != nil {
			if err := ensureObject(ctx); err != nil {
				return err
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ingest_jobs (id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,state,attempts,last_error,next_attempt_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		job.ID, job.Source, job.RemoteIP, job.MailFrom, job.Recipients, job.RawObjectKey, job.Metadata, job.State, job.Attempts, job.LastError, job.NextAttemptAt, job.CreatedAt, job.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error) {
	if limit <= 0 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(claimLeaseDuration)
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM ingest_jobs
			WHERE (state IN ('pending','retry') AND next_attempt_at <= $1)
			   OR (state = 'processing' AND (lease_until IS NULL OR lease_until <= $1))
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE ingest_jobs j
		SET state='processing', attempts=j.attempts + 1, claimed_at=$1, lease_until=$3, updated_at=$1
		FROM cte
		WHERE j.id = cte.id
		RETURNING j.id,j.source,j.remote_ip,j.mail_from,j.recipients,j.raw_object_key,j.metadata,j.state,j.attempts,j.last_error,j.next_attempt_at,j.claimed_at,j.lease_until,j.created_at,j.updated_at`,
		now, limit, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.IngestJob
	for rows.Next() {
		job := &models.IngestJob{}
		if err := rows.Scan(&job.ID, &job.Source, &job.RemoteIP, &job.MailFrom, &job.Recipients, &job.RawObjectKey, &job.Metadata, &job.State, &job.Attempts, &job.LastError, &job.NextAttemptAt, &job.ClaimedAt, &job.LeaseUntil, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkIngestJobDone(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ingest_jobs
		SET state='done', claimed_at=NULL, lease_until=NULL, updated_at=$2
		WHERE id=$1`, id, time.Now().UTC())
	return err
}

func (s *PgStore) MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE ingest_jobs
		SET state=$2, last_error=$3, next_attempt_at=$4, claimed_at=NULL, lease_until=NULL, updated_at=$5
		WHERE id=$1`, id, state, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}

func (s *PgStore) ListIngestJobs(ctx context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if state != "" {
		add("state =", state)
	}
	if source != "" {
		add("source =", source)
	}
	if recipient != "" {
		filters = append(filters, recipient)
		where += fmt.Sprintf(" AND $%d = ANY(recipients)", len(filters))
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs`+where, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(filters, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx, `
		SELECT id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,state,attempts,last_error,next_attempt_at,claimed_at,lease_until,created_at,updated_at
		FROM ingest_jobs`+where+fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.IngestJob
	for rows.Next() {
		item := &models.IngestJob{}
		if err := rows.Scan(&item.ID, &item.Source, &item.RemoteIP, &item.MailFrom, &item.Recipients, &item.RawObjectKey, &item.Metadata, &item.State, &item.Attempts, &item.LastError, &item.NextAttemptAt, &item.ClaimedAt, &item.LeaseUntil, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *PgStore) PurgeOldIngestJobs(ctx context.Context, before time.Time, limit int) (int, []string, error) {
	rows, err := s.pool.Query(ctx, `
		DELETE FROM ingest_jobs
		WHERE id IN (
			SELECT id FROM ingest_jobs
			WHERE state IN ('done','dead') AND updated_at < $1
			ORDER BY updated_at
			LIMIT $2
		)
		RETURNING COALESCE(raw_object_key, '')`, before, limit)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var keys []string
	n := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return 0, nil, err
		}
		n++
		if key != "" {
			keys = append(keys, key)
		}
	}
	return n, keys, rows.Err()
}

func (s *PgStore) CountIngestJobsByState(ctx context.Context, states ...string) (int, error) {
	if len(states) == 0 {
		var total int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs`).Scan(&total)
		return total, err
	}
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs WHERE state = ANY($1)`, states).Scan(&total)
	return total, err
}
