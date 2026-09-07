package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ store.IngressLedger = (*PgStore)(nil)

func (s *PgStore) CreateIngress(ctx context.Context, j *models.IngestJob, targets []store.IngressTarget, rawHash string, rawSize int64) error {
	if rawSize < 0 || len(rawHash) != 64 || j == nil || j.ID == uuid.Nil || len(targets) == 0 || j.RawObjectKey != "ingress-"+j.ID.String()+".eml" {
		return app.BadRequest("invalid durable receipt")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO ingest_jobs(id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,recovery_managed,last_error,raw_sha256,raw_size)
 VALUES($1,$2,$3,$4,$5,$6,$7,true,'',$8,$9)`, j.ID, j.Source, j.RemoteIP, j.MailFrom, j.Recipients, j.RawObjectKey, j.Metadata, rawHash, rawSize)
	if err != nil {
		return err
	}
	for _, t := range targets {
		// Recheck the exact snapshot at the acceptance transaction; address reuse
		// later never changes these destination IDs.
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailboxes m JOIN domain_zones z ON z.id=m.zone_id
   WHERE m.id=$1 AND m.tenant_id=$2 AND m.zone_id=$3 AND m.full_address=$4
   AND z.tenant_id=m.tenant_id AND z.is_verified AND z.mx_verified
   AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp()))`, t.MailboxID, t.TenantID, t.ZoneID, t.Address).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return app.BadRequest("destination changed before acceptance")
		}
		_, err = tx.Exec(ctx, `INSERT INTO ingress_targets(job_id,mailbox_id,tenant_id,zone_id,address) VALUES($1,$2,$3,$4,$5)`, j.ID, t.MailboxID, t.TenantID, t.ZoneID, t.Address)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Claim just one receipt, using database time. Later receipts are not leased
// while waiting behind slow object storage or another mailbox's transaction.
func (s *PgStore) ClaimIngress(ctx context.Context) (*store.IngressClaim, error) {
	c := &store.IngressClaim{Job: &models.IngestJob{}}
	j := c.Job
	err := s.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM ingest_jobs WHERE recovery_managed AND
 ((state IN ('pending','retry') AND next_attempt_at<=clock_timestamp()) OR
  (state='processing' AND (lease_until IS NULL OR lease_until<=clock_timestamp())))
 ORDER BY next_attempt_at,created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE ingest_jobs j SET state='processing',attempts=j.attempts+1,
 claim_token=gen_random_uuid(),claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '5 minutes',updated_at=clock_timestamp()
 FROM candidate WHERE j.id=candidate.id
 RETURNING j.id,j.source,j.remote_ip,j.mail_from,j.recipients,j.raw_object_key,j.metadata,j.state,j.attempts,j.last_error,
 j.next_attempt_at,j.claimed_at,j.lease_until,j.created_at,j.updated_at,j.claim_token,j.raw_sha256,j.raw_size`).Scan(
		&j.ID, &j.Source, &j.RemoteIP, &j.MailFrom, &j.Recipients, &j.RawObjectKey, &j.Metadata, &j.State, &j.Attempts, &j.LastError,
		&j.NextAttemptAt, &j.ClaimedAt, &j.LeaseUntil, &j.CreatedAt, &j.UpdatedAt, &c.Token, &c.RawHash, &c.RawSize)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}
func (s *PgStore) ListIngressTargets(ctx context.Context, id uuid.UUID) ([]store.IngressTarget, error) {
	rows, err := s.pool.Query(ctx, `SELECT job_id,mailbox_id,tenant_id,zone_id,address,state,message_id,attempts,last_error
 FROM ingress_targets WHERE job_id=$1 ORDER BY address`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.IngressTarget{}
	for rows.Next() {
		var t store.IngressTarget
		if err = rows.Scan(&t.JobID, &t.MailboxID, &t.TenantID, &t.ZoneID, &t.Address, &t.State, &t.MessageID, &t.Attempts, &t.LastError); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func lockIngress(ctx context.Context, tx pgx.Tx, c *store.IngressClaim) error {
	if c == nil || c.Job == nil || c.Token == uuid.Nil {
		return store.ErrIngressClaim
	}
	var key string
	err := tx.QueryRow(ctx, `SELECT raw_object_key FROM ingest_jobs WHERE id=$1 AND recovery_managed
 AND state='processing' AND claim_token=$2 AND lease_until>clock_timestamp() FOR UPDATE`, c.Job.ID, c.Token).Scan(&key)
	if err == pgx.ErrNoRows {
		return store.ErrIngressClaim
	}
	if err != nil {
		return err
	}
	if key != c.Job.RawObjectKey {
		return store.ErrIngressClaim
	}
	return nil
}

// DeliverIngress commits message, quota, progress tombstone, audit and outbox as
// one unit. A lost commit acknowledgement is safely resolved by reading progress.
func (s *PgStore) DeliverIngress(ctx context.Context, c *store.IngressClaim, m *models.Message, maxMailbox, daily int) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = lockIngress(ctx, tx, c); err != nil {
		return false, err
	}
	if m == nil || m.RawObjectKey != c.Job.RawObjectKey {
		return false, app.BadRequest("receipt content mismatch")
	}
	var state, address string
	var tenant, zone uuid.UUID
	err = tx.QueryRow(ctx, `SELECT state,address,tenant_id,zone_id FROM ingress_targets WHERE job_id=$1 AND mailbox_id=$2 FOR UPDATE`, c.Job.ID, m.MailboxID).Scan(&state, &address, &tenant, &zone)
	if err != nil {
		return false, err
	}
	if tenant != m.TenantID || zone != m.ZoneID {
		return false, app.Forbidden("receipt destination mismatch")
	}
	if state == "delivered" {
		return false, nil
	}
	if state != "pending" {
		return false, app.Conflict("receipt requires operator review")
	}
	// Serialize all durable ingress for a tenant and seed the daily counter from
	// pre-ledger messages once. Quota consumption is not refunded by user deletion.
	var id uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&id); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingress_daily_usage(tenant_id,day,used)
 SELECT $1,(clock_timestamp() AT TIME ZONE 'UTC')::date,count(*) FROM messages
 WHERE tenant_id=$1 AND received_at >= date_trunc('day',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
 ON CONFLICT DO NOTHING`, tenant)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE ingress_daily_usage SET used=used+1
 WHERE tenant_id=$1 AND day=(clock_timestamp() AT TIME ZONE 'UTC')::date AND ($2<=0 OR used<$2)`, tenant, daily)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, store.ErrIngressQuota
	}
	tag, err = tx.Exec(ctx, `UPDATE mailboxes m SET message_count=message_count+1 WHERE m.id=$1 AND m.tenant_id=$2
 AND m.zone_id=$3 AND m.full_address=$4 AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())
 AND ($5<=0 OR m.message_count<$5) AND EXISTS(SELECT 1 FROM domain_zones z WHERE z.id=m.zone_id
 AND z.tenant_id=m.tenant_id AND z.is_verified AND z.mx_verified)`, m.MailboxID, tenant, zone, address, maxMailbox)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, store.ErrIngressQuota
	}
	m.ID = uuid.New()
	m.ReceivedAt = c.Job.CreatedAt
	_, err = tx.Exec(ctx, `INSERT INTO messages(id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,raw_object_key,headers_json,received_at,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, m.ID, tenant, m.MailboxID, zone, m.Sender, m.Recipients, m.Subject, m.Size, m.RawObjectKey, m.HeadersJSON, m.ReceivedAt, m.ExpiresAt)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE ingress_targets SET state='delivered',message_id=$3,attempts=attempts+1,last_error='',updated_at=clock_timestamp()
 WHERE job_id=$1 AND mailbox_id=$2`, c.Job.ID, m.MailboxID, m.ID)
	if err != nil {
		return false, err
	}
	details, _ := json.Marshal(map[string]any{"receipt_id": c.Job.ID, "mailbox_id": m.MailboxID})
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details)
 VALUES($1,$2,'message.received','message',$3,$4)`, tenant, "ingest:"+c.Job.ID.String(), m.ID, details)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(hooks.Event{Type: "message.received", Mailbox: address, MessageID: m.ID.String(), TenantID: tenant.String(), Sender: m.Sender, Recipients: m.Recipients, Subject: m.Subject, OccurredAt: time.Now().UTC()})
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload) VALUES($1,'message.received',$2)`, uuid.New(), payload)
	if err != nil {
		return false, err
	}
	// Recheck elapsed time after any blocked transaction before committing effects.
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT lease_until>clock_timestamp() FROM ingest_jobs WHERE id=$1 AND claim_token=$2`, c.Job.ID, c.Token).Scan(&valid); err != nil {
		return false, err
	}
	if !valid {
		return false, store.ErrIngressClaim
	}
	return true, tx.Commit(ctx)
}

func (s *PgStore) FailIngressTarget(ctx context.Context, c *store.IngressClaim, mailbox uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockIngress(ctx, tx, c); err != nil {
		return err
	}
	// Never undo a delivered tombstone when a successful commit's response was lost.
	_, err = tx.Exec(ctx, `UPDATE ingress_targets SET attempts=attempts+1,last_error=$3,updated_at=clock_timestamp()
 WHERE job_id=$1 AND mailbox_id=$2 AND state='pending'`, c.Job.ID, mailbox, boundedIngressError(reason))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func boundedIngressError(s string) string {
	r := []rune(strings.ToValidUTF8(s, "?"))
	if len(r) > 2000 {
		r = r[:2000]
	}
	return string(r)
}

func (s *PgStore) FinishIngress(ctx context.Context, c *store.IngressClaim, maxAttempts int, next time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockIngress(ctx, tx, c); err != nil {
		return err
	}
	var total, remaining, attempts int
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE state<>'delivered') FROM ingress_targets WHERE job_id=$1`, c.Job.ID).Scan(&total, &remaining)
	if err != nil {
		return err
	}
	if total == 0 {
		return fmt.Errorf("receipt has no destination ledger")
	}
	if err = tx.QueryRow(ctx, `SELECT attempts FROM ingest_jobs WHERE id=$1`, c.Job.ID).Scan(&attempts); err != nil {
		return err
	}
	state, reason := "done", ""
	if remaining > 0 {
		state = "retry"
		reason = "Some destinations remain undelivered; original bytes retained"
		if attempts >= max(1, maxAttempts) {
			state = "dead"
			reason = "Ingress held for review; original bytes retained"
			_, err = tx.Exec(ctx, `UPDATE ingress_targets SET state='held',updated_at=clock_timestamp() WHERE job_id=$1 AND state='pending'`, c.Job.ID)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE ingest_jobs SET state=$3,last_error=$4,next_attempt_at=$5,
 claim_token=NULL,claimed_at=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND claim_token=$2`, c.Job.ID, c.Token, state, reason, next)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Only the existing platform-admin surface may call this. It never re-resolves
// addresses or replays delivered records. Legacy receipts need separate review.
func (s *PgStore) RetryIngress(ctx context.Context, id uuid.UUID, actor, reason string) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return app.BadRequest("actor and reason required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var managed bool
	var state string
	err = tx.QueryRow(ctx, `SELECT recovery_managed,state FROM ingest_jobs WHERE id=$1 FOR UPDATE`, id).Scan(&managed, &state)
	if err == pgx.ErrNoRows {
		return app.NotFound("receipt not found")
	}
	if err != nil {
		return err
	}
	if !managed || state != "dead" {
		return app.Conflict("only held ledger receipts can be retried")
	}
	tag, err := tx.Exec(ctx, `UPDATE ingress_targets SET state='pending',last_error='',updated_at=clock_timestamp() WHERE job_id=$1 AND state='held'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return app.Conflict("no held destinations")
	}
	_, err = tx.Exec(ctx, `UPDATE ingest_jobs SET state='pending',attempts=0,next_attempt_at=clock_timestamp(),
 claim_token=NULL,claimed_at=NULL,lease_until=NULL,last_error='',updated_at=clock_timestamp() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"reason": boundedIngressError(reason), "destinations": tag.RowsAffected()})
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor,action,resource_type,resource_id,details) VALUES($1,'ingest.retry','ingest_job',$2,$3)`, actor, id, details)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
