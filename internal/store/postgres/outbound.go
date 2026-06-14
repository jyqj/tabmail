package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// ================================================================
// Outbound jobs
// ================================================================

func (s *PgStore) CreateOutboundJob(ctx context.Context, job *models.OutboundJob) error {
	prepareOutboundJob(job)
	return insertOutboundJob(ctx, s.pool, job)
}

func (s *PgStore) CreateOutboundJobWithQuota(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation) error {
	if !quota.HasLimits() {
		return s.CreateOutboundJob(ctx, job)
	}
	prepareOutboundJob(job)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockOutboundQuotaKeys(ctx, tx, job, quota); err != nil {
		return err
	}

	if q := quota.UserDaily; q != nil && q.Limit > 0 {
		count, err := countOutboundSinceQuery(ctx, tx, job.TenantID, q.UserID, q.Since)
		if err != nil {
			return err
		}
		if count >= q.Limit {
			return store.ErrOutboundDailyQuotaExceeded
		}
	}

	if q := quota.SendAsDaily; q != nil && q.Limit > 0 {
		count, err := countOutboundByIdentitySinceQuery(ctx, tx, job.TenantID, q.PrincipalType, q.PrincipalID, q.IdentityID, q.Since)
		if err != nil {
			return err
		}
		if count >= q.Limit {
			return store.ErrSendAsDailyQuotaExceeded
		}
	}

	if err := insertOutboundJob(ctx, tx, job); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func prepareOutboundJob(job *models.OutboundJob) {
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
}

type outboundJobExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertOutboundJob(ctx context.Context, execer outboundJobExecer, job *models.OutboundJob) error {
	_, err := execer.Exec(ctx, `
		INSERT INTO outbound_jobs (id, tenant_id, user_id, api_key_id, mail_from, rcpt_to, subject,
			text_body, html_body, headers_json, raw_mime, zone_id, state, attempts, max_attempts,
			last_error, next_attempt_at, smtp_code, smtp_response, message_id_header, created_at, updated_at,
			to_addrs, cc_addrs, bcc_addrs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`,
		job.ID, job.TenantID, job.UserID, job.APIKeyID, job.MailFrom, job.RcptTo, job.Subject,
		job.TextBody, job.HTMLBody, job.HeadersJSON, job.RawMIME, job.ZoneID, job.State,
		job.Attempts, job.MaxAttempts, job.LastError, job.NextAttemptAt, job.SMTPCode,
		job.SMTPResponse, job.MessageIDHeader, job.CreatedAt, job.UpdatedAt,
		nonNil(job.To), nonNil(job.CC), nonNil(job.BCC))
	return err
}

type outboundJobQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func lockOutboundQuotaKeys(ctx context.Context, execer outboundJobExecer, job *models.OutboundJob, quota store.OutboundQuotaReservation) error {
	keys := make([]string, 0, 2)
	if q := quota.UserDaily; q != nil && q.Limit > 0 {
		principal := "tenant"
		if q.UserID != nil {
			principal = "user:" + q.UserID.String()
		}
		keys = append(keys, fmt.Sprintf("outbound:user-daily:%s:%s:%s", job.TenantID, principal, quotaDay(q.Since)))
	}
	if q := quota.SendAsDaily; q != nil && q.Limit > 0 {
		keys = append(keys, fmt.Sprintf("outbound:send-as-daily:%s:%s:%s:%s:%s", job.TenantID, q.PrincipalType, q.PrincipalID, q.IdentityID, quotaDay(q.Since)))
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := execer.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, key); err != nil {
			return err
		}
	}
	return nil
}

func quotaDay(since time.Time) string {
	return since.UTC().Format("2006-01-02")
}

const outboundJobSelect = `SELECT id, tenant_id, user_id, api_key_id, mail_from, rcpt_to, subject,
	text_body, html_body, headers_json, raw_mime, zone_id, state, attempts, max_attempts,
	last_error, next_attempt_at, claimed_at, lease_until, smtp_code, smtp_response,
	message_id_header, delivery_token, created_at, updated_at, to_addrs, cc_addrs, bcc_addrs
	FROM outbound_jobs`

func scanOutboundJob(row pgx.Row) (*models.OutboundJob, error) {
	job := &models.OutboundJob{}
	var userID pgtype.UUID
	var apiKeyID pgtype.UUID
	var deliveryToken pgtype.UUID
	var smtpCode *int
	err := row.Scan(&job.ID, &job.TenantID, &userID, &apiKeyID, &job.MailFrom, &job.RcptTo, &job.Subject,
		&job.TextBody, &job.HTMLBody, &job.HeadersJSON, &job.RawMIME, &job.ZoneID, &job.State,
		&job.Attempts, &job.MaxAttempts, &job.LastError, &job.NextAttemptAt, &job.ClaimedAt,
		&job.LeaseUntil, &smtpCode, &job.SMTPResponse, &job.MessageIDHeader, &deliveryToken, &job.CreatedAt, &job.UpdatedAt,
		&job.To, &job.CC, &job.BCC)
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
	if deliveryToken.Valid {
		id := uuid.UUID(deliveryToken.Bytes)
		job.DeliveryToken = &id
	}
	job.SMTPCode = smtpCode
	return job, nil
}

func (s *PgStore) GetOutboundJob(ctx context.Context, id uuid.UUID) (*models.OutboundJob, error) {
	return scanOutboundJob(s.pool.QueryRow(ctx, outboundJobSelect+` WHERE id=$1`, id))
}

// ListOutboundJobsScoped applies the OwnerListFilter in SQL: tenant isolation
// plus the mutually-exclusive owner dimension (AllInTenant for admins, user_id
// for users, api_key_id for API keys). The constructor guarantees mutual
// exclusivity; this method asserts it defensively.
func (s *PgStore) ListOutboundJobsScoped(ctx context.Context, scope authz.OwnerListFilter, pg models.Page) ([]*models.OutboundJob, int, error) {
	pg = pg.Normalize()
	args := []any{scope.TenantID}
	where := []string{"tenant_id=$1"}
	n := 1
	switch {
	case scope.AllInTenant:
		// No owner filter — admins see the whole tenant.
	case scope.UserID != nil:
		n++
		where = append(where, "user_id=$"+strconv.Itoa(n))
		args = append(args, *scope.UserID)
	case scope.APIKeyID != nil:
		n++
		where = append(where, "api_key_id=$"+strconv.Itoa(n))
		args = append(args, *scope.APIKeyID)
	default:
		// Unknown principal: return empty rather than the whole tenant.
		return []*models.OutboundJob{}, 0, nil
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx,
		"SELECT count(*) FROM outbound_jobs WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rowArgs := append(args, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx,
		outboundJobSelect+" WHERE "+whereSQL+
			" ORDER BY created_at DESC LIMIT $"+strconv.Itoa(n+1)+" OFFSET $"+strconv.Itoa(n+2),
		rowArgs...)
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
		SET state='processing', attempts=j.attempts + 1, claimed_at=$1, lease_until=$3,
		    delivery_token=gen_random_uuid(), updated_at=$1
		FROM cte
		WHERE j.id = cte.id
		RETURNING j.id, j.tenant_id, j.user_id, j.api_key_id, j.mail_from, j.rcpt_to, j.subject,
			j.text_body, j.html_body, j.headers_json, j.raw_mime, j.zone_id, j.state, j.attempts,
			j.max_attempts, j.last_error, j.next_attempt_at, j.claimed_at, j.lease_until,
			j.smtp_code, j.smtp_response, j.message_id_header, j.delivery_token, j.created_at, j.updated_at,
			j.to_addrs, j.cc_addrs, j.bcc_addrs`,
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

// ErrDeliveryTokenMismatch is returned when a mark operation fails because the
// delivery_token has changed (another worker re-claimed the job).
var ErrDeliveryTokenMismatch = errors.New("delivery token mismatch: job was re-claimed")

func (s *PgStore) MarkOutboundJobSent(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, smtpCode int, smtpResponse, messageID string) error {
	now := time.Now().UTC()
	var tag pgconn.CommandTag
	var err error
	if deliveryToken != nil {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state='sent', smtp_code=$2, smtp_response=$3, message_id_header=$4,
				claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$5
			WHERE id=$1 AND delivery_token=$6`, id, smtpCode, smtpResponse, messageID, now, *deliveryToken)
	} else {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state='sent', smtp_code=$2, smtp_response=$3, message_id_header=$4,
				claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$5
			WHERE id=$1`, id, smtpCode, smtpResponse, messageID, now)
	}
	if err != nil {
		return err
	}
	if deliveryToken != nil && tag.RowsAffected() == 0 {
		return ErrDeliveryTokenMismatch
	}
	return nil
}

func (s *PgStore) MarkOutboundJobRetry(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	now := time.Now().UTC()
	var tag pgconn.CommandTag
	var err error
	if deliveryToken != nil {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state='retry', last_error=$2, next_attempt_at=$3,
				claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$4
			WHERE id=$1 AND delivery_token=$5`, id, lastError, nextAttemptAt.UTC(), now, *deliveryToken)
	} else {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state='retry', last_error=$2, next_attempt_at=$3,
				claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$4
			WHERE id=$1`, id, lastError, nextAttemptAt.UTC(), now)
	}
	if err != nil {
		return err
	}
	if deliveryToken != nil && tag.RowsAffected() == 0 {
		return ErrDeliveryTokenMismatch
	}
	return nil
}

func (s *PgStore) MarkOutboundJobFailed(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, dead bool) error {
	state := models.OutboundFailed
	if dead {
		state = models.OutboundDead
	}
	now := time.Now().UTC()
	var tag pgconn.CommandTag
	var err error
	if deliveryToken != nil {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state=$2, last_error=$3, claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$4
			WHERE id=$1 AND delivery_token=$5`, id, state, lastError, now, *deliveryToken)
	} else {
		tag, err = s.pool.Exec(ctx, `
			UPDATE outbound_jobs
			SET state=$2, last_error=$3, claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$4
			WHERE id=$1`, id, state, lastError, now)
	}
	if err != nil {
		return err
	}
	if deliveryToken != nil && tag.RowsAffected() == 0 {
		return ErrDeliveryTokenMismatch
	}
	return nil
}

func (s *PgStore) CountOutboundSince(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, since time.Time) (int, error) {
	return countOutboundSinceQuery(ctx, s.pool, tenantID, userID, since)
}

func countOutboundSinceQuery(ctx context.Context, querier outboundJobQuerier, tenantID uuid.UUID, userID *uuid.UUID, since time.Time) (int, error) {
	var n int
	if userID != nil {
		err := querier.QueryRow(ctx,
			`SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND user_id=$2 AND created_at >= $3`,
			tenantID, *userID, since).Scan(&n)
		return n, err
	}
	err := querier.QueryRow(ctx,
		`SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND created_at >= $2`,
		tenantID, since).Scan(&n)
	return n, err
}

// CountOutboundByIdentitySince counts outbound jobs created by a specific principal
// for a specific send identity since the given time.
// It joins outbound_jobs with send_identities to match the mail_from address.
func (s *PgStore) CountOutboundByIdentitySince(ctx context.Context, tenantID uuid.UUID, principalType string, principalID uuid.UUID, identityID uuid.UUID, since time.Time) (int, error) {
	return countOutboundByIdentitySinceQuery(ctx, s.pool, tenantID, principalType, principalID, identityID, since)
}

func countOutboundByIdentitySinceQuery(ctx context.Context, querier outboundJobQuerier, tenantID uuid.UUID, principalType string, principalID uuid.UUID, identityID uuid.UUID, since time.Time) (int, error) {
	var n int
	var err error
	if principalType == "user" {
		err = querier.QueryRow(ctx, `
			SELECT count(*) FROM outbound_jobs o
			JOIN send_identities si ON si.tenant_id = o.tenant_id
				AND (
					(si.identity_type = 'exact' AND o.mail_from = si.address)
					OR (si.identity_type = 'domain_wildcard' AND o.mail_from LIKE '%@' || SUBSTRING(si.address FROM 3))
				)
			WHERE o.tenant_id = $1 AND o.user_id = $2 AND si.id = $3 AND o.created_at >= $4`,
			tenantID, principalID, identityID, since).Scan(&n)
	} else {
		err = querier.QueryRow(ctx, `
			SELECT count(*) FROM outbound_jobs o
			JOIN send_identities si ON si.tenant_id = o.tenant_id
				AND (
					(si.identity_type = 'exact' AND o.mail_from = si.address)
					OR (si.identity_type = 'domain_wildcard' AND o.mail_from LIKE '%@' || SUBSTRING(si.address FROM 3))
				)
			WHERE o.tenant_id = $1 AND o.api_key_id = $2 AND si.id = $3 AND o.created_at >= $4`,
			tenantID, principalID, identityID, since).Scan(&n)
	}
	return n, err
}

func (s *PgStore) RequeueOutboundJob(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE outbound_jobs
		SET state='pending', last_error='', next_attempt_at=$2,
			claimed_at=NULL, lease_until=NULL, delivery_token=NULL, updated_at=$2
		WHERE id=$1 AND state IN ('dead','failed')`, id, now)
	return err
}

// ================================================================
// Outbound attempts
// ================================================================

func (s *PgStore) CreateOutboundAttempt(ctx context.Context, a *models.OutboundAttempt) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbound_attempts (id, job_id, tenant_id, adapter, attempt, smtp_code, smtp_response, remote_host, started_at, finished_at, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		a.ID, a.JobID, a.TenantID, a.Adapter, a.Attempt, a.SMTPCode, a.SMTPResponse, a.RemoteHost, a.StartedAt, a.FinishedAt, a.Error)
	return err
}

func (s *PgStore) ListOutboundAttempts(ctx context.Context, jobID uuid.UUID) ([]*models.OutboundAttempt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, job_id, tenant_id, adapter, attempt, smtp_code, smtp_response, remote_host, started_at, finished_at, error
		FROM outbound_attempts WHERE job_id=$1 ORDER BY attempt`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.OutboundAttempt
	for rows.Next() {
		a := &models.OutboundAttempt{}
		if err := rows.Scan(&a.ID, &a.JobID, &a.TenantID, &a.Adapter, &a.Attempt, &a.SMTPCode, &a.SMTPResponse, &a.RemoteHost, &a.StartedAt, &a.FinishedAt, &a.Error); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ================================================================
// Suppression list
// ================================================================

func (s *PgStore) AddSuppression(ctx context.Context, e *models.SuppressionEntry) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO suppression_list (id, tenant_id, address, reason, source_job_id, created_at)
		VALUES ($1,$2,LOWER($3),$4,$5,$6)
		ON CONFLICT (tenant_id, address) DO NOTHING`,
		e.ID, e.TenantID, e.Address, e.Reason, e.SourceJobID, e.CreatedAt)
	return err
}

func (s *PgStore) IsSuppressed(ctx context.Context, tenantID uuid.UUID, address string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM suppression_list WHERE tenant_id=$1 AND address=LOWER($2))`,
		tenantID, address).Scan(&exists)
	return exists, err
}

func (s *PgStore) ListSuppressions(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.SuppressionEntry, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM suppression_list WHERE tenant_id=$1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, address, reason, source_job_id, created_at
		FROM suppression_list WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.SuppressionEntry
	for rows.Next() {
		e := &models.SuppressionEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Address, &e.Reason, &e.SourceJobID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *PgStore) DeleteSuppression(ctx context.Context, tenantID uuid.UUID, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM suppression_list WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	return err
}

// ================================================================
// Outbound templates
// ================================================================

func (s *PgStore) CreateOutboundTemplate(ctx context.Context, t *models.OutboundTemplate) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbound_templates (id, tenant_id, name, subject_tmpl, text_tmpl, html_tmpl, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.TenantID, t.Name, t.SubjectTmpl, t.TextTmpl, t.HTMLTmpl, t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *PgStore) GetOutboundTemplate(ctx context.Context, tenantID uuid.UUID, name string) (*models.OutboundTemplate, error) {
	t := &models.OutboundTemplate{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, subject_tmpl, text_tmpl, html_tmpl, created_at, updated_at
		FROM outbound_templates WHERE tenant_id=$1 AND name=$2`, tenantID, name).
		Scan(&t.ID, &t.TenantID, &t.Name, &t.SubjectTmpl, &t.TextTmpl, &t.HTMLTmpl, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *PgStore) ListOutboundTemplates(ctx context.Context, tenantID uuid.UUID) ([]*models.OutboundTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, subject_tmpl, text_tmpl, html_tmpl, created_at, updated_at
		FROM outbound_templates WHERE tenant_id=$1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.OutboundTemplate
	for rows.Next() {
		t := &models.OutboundTemplate{}
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.SubjectTmpl, &t.TextTmpl, &t.HTMLTmpl, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PgStore) UpdateOutboundTemplate(ctx context.Context, t *models.OutboundTemplate) error {
	t.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE outbound_templates
		SET subject_tmpl=$3, text_tmpl=$4, html_tmpl=$5, updated_at=$6
		WHERE tenant_id=$1 AND name=$2`,
		t.TenantID, t.Name, t.SubjectTmpl, t.TextTmpl, t.HTMLTmpl, t.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *PgStore) DeleteOutboundTemplate(ctx context.Context, tenantID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM outbound_templates WHERE tenant_id=$1 AND name=$2`, tenantID, name)
	return err
}
