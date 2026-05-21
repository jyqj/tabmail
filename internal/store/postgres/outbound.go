package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"tabmail/internal/models"
)

// ================================================================
// Outbound jobs
// ================================================================

func (s *PgStore) CreateOutboundJob(ctx context.Context, job *models.OutboundJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := time.Now().UTC()
	if job.NextAttemptAt.IsZero() {
		job.NextAttemptAt = now
	}
	if job.State == "" {
		job.State = models.OutboundPending
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbound_jobs (id, tenant_id, user_id, api_key_id, mail_from, rcpt_to, subject,
			text_body, html_body, headers_json, raw_mime, zone_id, state, attempts, max_attempts,
			last_error, next_attempt_at, smtp_code, smtp_response, message_id_header, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		job.ID, job.TenantID, job.UserID, job.APIKeyID, job.MailFrom, job.RcptTo, job.Subject,
		job.TextBody, job.HTMLBody, job.HeadersJSON, job.RawMIME, job.ZoneID, job.State,
		job.Attempts, job.MaxAttempts, job.LastError, job.NextAttemptAt, job.SMTPCode,
		job.SMTPResponse, job.MessageIDHeader, job.CreatedAt, job.UpdatedAt)
	return err
}

const outboundJobSelect = `SELECT id, tenant_id, user_id, api_key_id, mail_from, rcpt_to, subject,
	text_body, html_body, headers_json, raw_mime, zone_id, state, attempts, max_attempts,
	last_error, next_attempt_at, claimed_at, lease_until, smtp_code, smtp_response,
	message_id_header, created_at, updated_at
	FROM outbound_jobs`

func scanOutboundJob(row pgx.Row) (*models.OutboundJob, error) {
	job := &models.OutboundJob{}
	var userID pgtype.UUID
	var apiKeyID pgtype.UUID
	var smtpCode *int
	err := row.Scan(&job.ID, &job.TenantID, &userID, &apiKeyID, &job.MailFrom, &job.RcptTo, &job.Subject,
		&job.TextBody, &job.HTMLBody, &job.HeadersJSON, &job.RawMIME, &job.ZoneID, &job.State,
		&job.Attempts, &job.MaxAttempts, &job.LastError, &job.NextAttemptAt, &job.ClaimedAt,
		&job.LeaseUntil, &smtpCode, &job.SMTPResponse, &job.MessageIDHeader, &job.CreatedAt, &job.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		id := uuid.UUID(userID.Bytes)
		job.UserID = &id
	}
	if apiKeyID.Valid {
		id := uuid.UUID(apiKeyID.Bytes)
		job.APIKeyID = &id
	}
	job.SMTPCode = smtpCode
	return job, nil
}

func (s *PgStore) GetOutboundJob(ctx context.Context, id uuid.UUID) (*models.OutboundJob, error) {
	return scanOutboundJob(s.pool.QueryRow(ctx, outboundJobSelect+` WHERE id=$1`, id))
}

func (s *PgStore) ListOutboundJobs(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		outboundJobSelect+` WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.OutboundJob
	for rows.Next() {
		job, err := scanOutboundJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, job)
	}
	return out, total, rows.Err()
}

func (s *PgStore) ClaimOutboundJobs(ctx context.Context, now time.Time, limit int) ([]*models.OutboundJob, error) {
	if limit <= 0 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(claimLeaseDuration)
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM outbound_jobs
			WHERE (state IN ('pending','retry') AND next_attempt_at <= $1)
			   OR (state = 'processing' AND (lease_until IS NULL OR lease_until <= $1))
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbound_jobs j
		SET state='processing', attempts=j.attempts + 1, claimed_at=$1, lease_until=$3, updated_at=$1
		FROM cte
		WHERE j.id = cte.id
		RETURNING j.id, j.tenant_id, j.user_id, j.api_key_id, j.mail_from, j.rcpt_to, j.subject,
			j.text_body, j.html_body, j.headers_json, j.raw_mime, j.zone_id, j.state, j.attempts,
			j.max_attempts, j.last_error, j.next_attempt_at, j.claimed_at, j.lease_until,
			j.smtp_code, j.smtp_response, j.message_id_header, j.created_at, j.updated_at`,
		now, limit, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.OutboundJob
	for rows.Next() {
		job, err := scanOutboundJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkOutboundJobSent(ctx context.Context, id uuid.UUID, smtpCode int, smtpResponse, messageID string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE outbound_jobs
		SET state='sent', smtp_code=$2, smtp_response=$3, message_id_header=$4,
			claimed_at=NULL, lease_until=NULL, updated_at=$5
		WHERE id=$1`, id, smtpCode, smtpResponse, messageID, now)
	return err
}

func (s *PgStore) MarkOutboundJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE outbound_jobs
		SET state='retry', last_error=$2, next_attempt_at=$3,
			claimed_at=NULL, lease_until=NULL, updated_at=$4
		WHERE id=$1`, id, lastError, nextAttemptAt.UTC(), now)
	return err
}

func (s *PgStore) MarkOutboundJobFailed(ctx context.Context, id uuid.UUID, lastError string, dead bool) error {
	state := models.OutboundFailed
	if dead {
		state = models.OutboundDead
	}
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE outbound_jobs
		SET state=$2, last_error=$3, claimed_at=NULL, lease_until=NULL, updated_at=$4
		WHERE id=$1`, id, state, lastError, now)
	return err
}

func (s *PgStore) CountOutboundSince(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, since time.Time) (int, error) {
	var n int
	if userID != nil {
		err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND user_id=$2 AND created_at >= $3`,
			tenantID, *userID, since).Scan(&n)
		return n, err
	}
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND created_at >= $2`,
		tenantID, since).Scan(&n)
	return n, err
}

// ================================================================
// Uniqueness checks
// ================================================================

func (s *PgStore) ExistsMailboxByAddress(ctx context.Context, address string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM mailboxes WHERE full_address=$1)`, address).Scan(&exists)
	return exists, err
}

func (s *PgStore) ExistsZoneByDomain(ctx context.Context, domain string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM domain_zones WHERE domain=$1)`, domain).Scan(&exists)
	return exists, err
}
