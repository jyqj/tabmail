package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Operations need a CURRENT super-admin, not a bearer role captured earlier.
func recoveryActor(ctx context.Context, tx pgx.Tx, a authz.Actor) (authz.Actor, error) {
	a, e := currentMemberActor(ctx, tx, a, a.TenantID)
	if e != nil {
		return a, e
	}
	if !a.IsSuperAdmin {
		return a, app.Forbidden("platform recovery administrator required")
	}
	return a, nil
}
func scanReceipt(row pgx.Row) (company.RecoveryReceipt, error) {
	v := company.RecoveryReceipt{Targets: []company.RecoveryTarget{}}
	e := row.Scan(&v.ID, &v.State, &v.Error, &v.UpdatedAt, &v.RawKey, &v.RawHash, &v.RawSize)
	if v.Error != "" {
		v.Error = "Receipt requires review; inspect the selected company destinations"
	}
	return v, e
}

const receiptSelect = `SELECT j.id,j.state,j.last_error,j.updated_at,j.raw_object_key,COALESCE(j.raw_sha256,''),COALESCE(j.raw_size,0) FROM ingest_jobs j`

func receiptTargets(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) ([]company.RecoveryTarget, error) {
	out := []company.RecoveryTarget{}
	rows, e := tx.Query(ctx, `SELECT mailbox_id,address,state,last_error FROM ingest_recipient_outcomes WHERE tenant_id=$1 AND job_id=$2 ORDER BY address`, tenant, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		v := company.RecoveryTarget{}
		if e = rows.Scan(&v.MailboxID, &v.Address, &v.State, &v.Error); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PgStore) ListRecoveryReceipts(ctx context.Context, a authz.Actor, page models.Page) ([]company.RecoveryReceipt, int, error) {
	page = page.Normalize()
	out := []company.RecoveryReceipt{}
	total := 0
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	a, e = recoveryActor(ctx, tx, a)
	if e != nil {
		return nil, 0, e
	}
	where := `j.recovery_managed AND j.state<>'done' AND EXISTS(SELECT 1 FROM ingest_recipient_outcomes t WHERE t.job_id=j.id AND t.tenant_id=$1)`
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs j WHERE `+where, a.TenantID).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, receiptSelect+` WHERE `+where+` ORDER BY j.created_at LIMIT $2 OFFSET $3`, a.TenantID, page.PerPage, page.Offset())
	if e != nil {
		return nil, 0, e
	}
	for rows.Next() {
		v, e := scanReceipt(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	for i := range out {
		out[i].Targets, e = receiptTargets(ctx, tx, a.TenantID, out[i].ID)
		if e != nil {
			return nil, 0, e
		}
	}
	return out, total, tx.Commit(ctx)
}
func (s *PgStore) InspectRecoveryReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, reason string) (*company.RecoveryReceipt, error) {
	if !meaningfulReason(reason) {
		return nil, app.BadRequest("inspection reason must be 8-1000 bytes")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	a, e = recoveryActor(ctx, tx, a)
	if e != nil {
		return nil, e
	}
	v, e := scanReceipt(tx.QueryRow(ctx, receiptSelect+` WHERE j.id=$1 AND j.recovery_managed AND EXISTS(SELECT 1 FROM ingest_recipient_outcomes t WHERE t.job_id=j.id AND t.tenant_id=$2)`, id, a.TenantID))
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, app.NotFound("recovery receipt not found")
	}
	if e != nil {
		return nil, e
	}
	v.Targets, e = receiptTargets(ctx, tx, a.TenantID, id)
	if e != nil {
		return nil, e
	}
	if e = companyAudit(ctx, tx, a, "ingress.inspect", "ingest_job", id, map[string]any{"reason": reason, "raw_size": v.RawSize, "inspection_version": v.UpdatedAt}); e != nil {
		return nil, e
	}
	return &v, tx.Commit(ctx)
}
func (s *PgStore) RetryRecoveryReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, version time.Time, targets []uuid.UUID, reason, verifiedHash string) error {
	if !meaningfulReason(reason) || len(targets) == 0 || len(targets) > 200 {
		return app.BadRequest("select destinations and provide a reason (8-1000 bytes)")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	a, e = recoveryActor(ctx, tx, a)
	if e != nil {
		return e
	}
	// Same job->target ordering as ingestion. NOWAIT refuses concurrent workers
	// instead of holding a company row while waiting on a worker's receipt lock.
	v, e := scanReceipt(tx.QueryRow(ctx, receiptSelect+` WHERE j.id=$1 AND j.recovery_managed FOR UPDATE NOWAIT`, id))
	if errors.Is(e, pgx.ErrNoRows) {
		return app.NotFound("receipt not found")
	}
	if e != nil {
		return app.Conflict("receipt is busy; inspect again")
	}
	if v.State == "processing" || v.State == "done" || !v.UpdatedAt.Equal(version) {
		return app.Conflict("receipt changed since inspection")
	}
	if verifiedHash == "" || verifiedHash != v.RawHash {
		return app.Conflict("original checksum verification required")
	}
	seen := map[uuid.UUID]bool{}
	for _, mb := range targets {
		if seen[mb] {
			return app.BadRequest("duplicate destination")
		}
		seen[mb] = true
		var valid bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest_recipient_outcomes t JOIN mailboxes m ON m.id=t.mailbox_id AND m.tenant_id=t.tenant_id AND m.zone_id=t.zone_id AND m.full_address=t.address JOIN domain_zones z ON z.id=t.zone_id AND z.tenant_id=t.tenant_id WHERE t.job_id=$1 AND t.mailbox_id=$2 AND t.tenant_id=$3 AND t.state<>'delivered' AND z.is_verified AND z.mx_verified AND (m.expires_at IS NULL OR m.expires_at>now()))`, id, mb, a.TenantID).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return app.Conflict("destination delivered, changed, expired or outside selected company")
		}
	}
	for _, mb := range targets {
		if _, e = tx.Exec(ctx, `UPDATE ingest_recipient_outcomes SET state='pending',attempts=0,last_error='',updated_at=clock_timestamp() WHERE job_id=$1 AND mailbox_id=$2 AND tenant_id=$3 AND state<>'delivered'`, id, mb, a.TenantID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE ingest_jobs SET state='pending',attempts=0,last_error='',next_attempt_at=now(),claimed_at=NULL,lease_until=NULL,claim_token=NULL,updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return e
	}
	if e = companyAudit(ctx, tx, a, "ingress.retry", "ingest_job", id, map[string]any{"reason": reason, "targets": targets, "inspection_version": version, "verified_hash": verifiedHash}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PgStore) ListOutboundRecipients(ctx context.Context, tenant, job uuid.UUID) ([]company.Recipient, error) {
	out := []company.Recipient{}
	rows, e := s.pool.Query(ctx, `SELECT address,state,smtp_code,diagnostic,attempts,updated_at FROM outbound_recipients WHERE tenant_id=$1 AND job_id=$2 ORDER BY address`, tenant, job)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		v := company.Recipient{}
		if e = rows.Scan(&v.Address, &v.State, &v.SMTPCode, &v.Diagnostic, &v.Attempts, &v.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PgStore) ReconcileOutbound(ctx context.Context, a authz.Actor, id uuid.UUID, version time.Time, results []company.Recipient, reason string) error {
	if !meaningfulReason(reason) || len(results) == 0 || len(results) > 50 {
		return app.BadRequest("explicit confirmed outcomes and reason required")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	a, e = recoveryActor(ctx, tx, a)
	if e != nil {
		return e
	}
	var state, marker string
	var updated time.Time
	var managed bool
	e = tx.QueryRow(ctx, `SELECT state,in_flight_domain,updated_at,recipient_ledger FROM outbound_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE NOWAIT`, a.TenantID, id).Scan(&state, &marker, &updated, &managed)
	if errors.Is(e, pgx.ErrNoRows) {
		return app.NotFound("send job not found")
	}
	if e != nil {
		return app.Conflict("send job busy")
	}
	if state == "processing" || !updated.Equal(version) || !managed {
		return app.Conflict("job changed or needs legacy manual investigation")
	}
	seen := map[string]bool{}
	for _, v := range results {
		if seen[v.Address] || (v.State != "accepted" && v.State != "permanent" && v.State != "temporary") {
			return app.BadRequest("each recipient must have one confirmed accepted/not-accepted outcome")
		}
		seen[v.Address] = true
		tag, e := tx.Exec(ctx, `UPDATE outbound_recipients SET state=$4,diagnostic='Operator confirmed outcome; see audit',updated_at=now() WHERE tenant_id=$1 AND job_id=$2 AND address=$3 AND state='uncertain'`, a.TenantID, id, v.Address, v.State)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("only uncertain recipients may be reconciled")
		}
	}
	var unresolved int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM outbound_recipients WHERE job_id=$1 AND state='uncertain'`, id).Scan(&unresolved); e != nil {
		return e
	}
	if unresolved == 0 {
		if _, e = tx.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='',
        state=CASE WHEN NOT EXISTS(SELECT 1 FROM outbound_recipients WHERE job_id=$1 AND state<>'accepted') THEN 'sent'::outbound_state ELSE 'failed'::outbound_state END,
        last_error=CASE WHEN NOT EXISTS(SELECT 1 FROM outbound_recipients WHERE job_id=$1 AND state<>'accepted') THEN '' ELSE 'Operator reconciliation complete; explicit retry required' END,
        smtp_response='Operator confirmed downstream outcome; see audit',updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return e
		}
	} else {
		if _, e = tx.Exec(ctx, `UPDATE outbound_jobs SET updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return e
		}
	}
	outcomes := make([]map[string]string, 0, len(results))
	for _, v := range results {
		outcomes = append(outcomes, map[string]string{"recipient_hash": company.Hash(v.Address), "state": v.State})
	}
	if e = companyAudit(ctx, tx, a, "outbound.reconcile", "outbound_job", id, map[string]any{"reason": reason, "inspection_version": version, "outcomes": outcomes, "previous_marker_hash": company.Hash(marker)}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PgStore) Readiness(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *PgStore) Heartbeat(ctx context.Context, id, role string) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO runtime_instances(id,role,last_seen) VALUES($1,$2,clock_timestamp()) ON CONFLICT(id) DO UPDATE SET role=EXCLUDED.role,last_seen=EXCLUDED.last_seen`, id, role)
	return e
}
func (s *PgStore) CheckWorkers(ctx context.Context, outbound bool) error {
	if !outbound {
		return nil
	}
	var ready bool
	e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_instances WHERE role IN ('all','worker') AND last_seen>now()-interval '90 seconds')`).Scan(&ready)
	if e != nil {
		return e
	}
	if !ready {
		return fmt.Errorf("no recent outbound worker heartbeat")
	}
	return nil
}

// Metadata housekeeping is bounded and does not erase undelivered originals.
// Attachments remain while referenced by either a draft or an outbound job.
func (s *PgStore) SweepCompanyMetadata(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM sent_mail_items WHERE asset_id IN (SELECT asset_id FROM sent_mail_items WHERE purge_after<now() OR expires_at<now() ORDER BY COALESCE(purge_after,expires_at) LIMIT 1000)`,
        `DELETE FROM sent_mail_assets a WHERE a.id IN (SELECT x.id FROM sent_mail_assets x WHERE NOT EXISTS(SELECT 1 FROM sent_mail_items i WHERE i.asset_id=x.id) AND NOT EXISTS(SELECT 1 FROM outbound_jobs j WHERE j.id=x.id) LIMIT 1000)`,
        `DELETE FROM runtime_instances WHERE last_seen<now()-interval '1 day'`,
		`DELETE FROM mailbox_event_log WHERE sequence IN (SELECT sequence FROM mailbox_event_log WHERE created_at<now()-interval '7 days' ORDER BY sequence LIMIT 5000)`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT tenant_id FROM mail_attachments WHERE expires_at<now() LIMIT 20`)
	if err != nil {
		return err
	}
	tenants := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		if err = s.sweepCompanyAttachments(ctx, tenant); err != nil {
			return err
		}
	}
	return nil
}

func (s *PgStore) sweepCompanyAttachments(ctx context.Context, tenant uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Same first lock as SaveDraft/FinishAttachment. A draft's JSON reference
	// must not race a GC snapshot; pinning outbound attachments also takes a
	// shared attachment-row lock and is protected by the foreign key.
	if err = lockMemberTenant(ctx, tx, tenant); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM mail_attachments WHERE tenant_id=$1 AND expires_at<now() ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`, tenant)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH gone AS (
       DELETE FROM mail_attachments a WHERE a.tenant_id=$1 AND a.id=ANY($2::uuid[]) AND a.expires_at<now()
       AND NOT EXISTS(SELECT 1 FROM outbound_attachments o WHERE o.attachment_id=a.id)
       AND NOT EXISTS(SELECT 1 FROM sent_asset_attachments o WHERE o.attachment_id=a.id)
       AND NOT EXISTS(SELECT 1 FROM mail_drafts d WHERE d.tenant_id=a.tenant_id AND d.payload->'attachment_ids' ? a.id::text)
       RETURNING object_key)
       INSERT INTO orphan_objects(object_key,first_failed_at,last_failed_at,attempts)
       SELECT object_key,now(),now(),1 FROM gone ON CONFLICT DO NOTHING`, tenant, ids)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ company.Repository = (*PgStore)(nil)
