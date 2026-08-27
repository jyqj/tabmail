package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *PgStore) CreateIngestJob(ctx context.Context, job *models.IngestJob) error {
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
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ingest_jobs (id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,state,attempts,last_error,next_attempt_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		job.ID, job.Source, job.RemoteIP, job.MailFrom, job.Recipients, job.RawObjectKey, job.Metadata, job.State, job.Attempts, job.LastError, job.NextAttemptAt, job.CreatedAt, job.UpdatedAt)
	return err
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
