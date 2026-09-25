package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"time"
)

// Lock order: tenant -> departing user -> successor -> active jobs. Draft and
// upload writes take a user SHARE lock; enqueue has the same database fence.
// A preview never reads or returns private draft/message content.
func (s *PgStore) offboardingTarget(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID) (*models.User, error) {
	if target == successor || target == a.ID || target == uuid.Nil || successor == uuid.Nil {
		return nil, app.BadRequest("distinct employee and successor required")
	}
	u, e := scanUser(tx.QueryRow(ctx, userSelect+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, target))
	if e != nil {
		return nil, e
	}
	if u == nil {
		return nil, app.NotFound("employee not found")
	}
	if !u.IsActive {
		return nil, app.Conflict("employee is already inactive")
	}
	if !authz.CanManageTenantMember(a, a.TenantID, u.Role) {
		return nil, app.Forbidden("cannot manage this employee")
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT is_active FROM users WHERE tenant_id=$1 AND id=$2 FOR SHARE`, a.TenantID, successor).Scan(&active); errors.Is(e, pgx.ErrNoRows) {
		return nil, app.BadRequest("successor must be an active company employee")
	}
	if e != nil {
		return nil, e
	}
	if !active {
		return nil, app.BadRequest("successor is inactive")
	}
	next := *u
	next.IsActive = false
	if e = guardMemberRemoval(ctx, tx, u, &next); e != nil {
		return nil, e
	}
	// Freeze current queue evidence while taking the fingerprint. Workers still
	// own processing/uncertain outcomes; executing a plan never rewrites them.
	rows, e := tx.Query(ctx, `SELECT id FROM outbound_jobs WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND (state IN ('pending','retry','processing') OR in_flight_domain<>'' OR EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=outbound_jobs.id AND r.state='uncertain')) ORDER BY id FOR UPDATE`, a.TenantID, target)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	return u, e
}
func offboardingSnapshot(ctx context.Context, tx pgx.Tx, tenant, target uuid.UUID) (company.OffboardingImpact, string, error) {
	impact := company.OffboardingImpact{}
	var raw []byte
	// IDs/versions, not just counts: same-count replacements invalidate a plan.
	// Incoming mail does not invalidate handover; ownership transfers the live
	// mailbox as a whole. Private draft revisions and queued outcomes do.
	e := tx.QueryRow(ctx, `SELECT jsonb_build_object(
 'user',(SELECT jsonb_build_array(id,is_active,session_version,role) FROM users WHERE tenant_id=$1 AND id=$2),
 'mailboxes',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,lifecycle_revision) ORDER BY id) FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2),'[]'),
 'drafts',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,revision,mailbox_id) ORDER BY id) FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL),'[]'),
 'uploads',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,state) ORDER BY id) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'keys',COALESCE((SELECT jsonb_agg(id ORDER BY id) FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2),'[]'),
 'grants',COALESCE((SELECT jsonb_agg(jsonb_build_array(mailbox_id,can_read,can_organize,can_send,template_only) ORDER BY mailbox_id) FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'template_grants',COALESCE((SELECT jsonb_agg(jsonb_build_array(template_id,mailbox_id) ORDER BY template_id,mailbox_id) FROM mail_template_grants WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'jobs',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,state,updated_at,in_flight_domain,(SELECT COALESCE(jsonb_agg(jsonb_build_array(r.address,r.state,r.attempts,r.updated_at) ORDER BY r.address),'[]') FROM outbound_recipients r WHERE r.tenant_id=$1 AND r.job_id=outbound_jobs.id)) ORDER BY id) FROM outbound_jobs WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND (state IN ('pending','retry','processing') OR in_flight_domain<>'' OR EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=outbound_jobs.id AND r.state='uncertain'))),'[]'))`, tenant, target).Scan(&raw)
	if e != nil {
		return impact, "", e
	}
	e = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2),
 (SELECT count(*) FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL),
 (SELECT count(*) FROM mail_drafts d JOIN mailboxes m ON m.tenant_id=d.tenant_id AND m.id=d.mailbox_id WHERE d.tenant_id=$1 AND d.user_id=$2 AND d.sealed_at IS NULL AND m.owner_user_id=$2),
 (SELECT count(*) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2),
 (SELECT count(*) FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2),
 (SELECT count(*) FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$2),
 (SELECT count(*) FROM outbound_jobs j WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND state IN ('pending','retry') AND in_flight_domain='' AND NOT EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=j.id AND r.state='uncertain')),
 (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND state='processing'),
 (SELECT count(*) FROM outbound_jobs j WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND (in_flight_domain<>'' OR EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=j.id AND r.state='uncertain')))`, tenant, target).Scan(&impact.Mailboxes, &impact.Drafts, &impact.TransferableDrafts, &impact.Attachments, &impact.APIKeys, &impact.Grants, &impact.Queued, &impact.InFlight, &impact.Uncertain)
	return impact, company.Hash(string(raw)), e
}
func (s *PgStore) PreviewOffboarding(ctx context.Context, a authz.Actor, target, successor uuid.UUID, options company.OffboardingOptions, reason string) (*company.OffboardingPlan, error) {
	reason = strings.TrimSpace(reason)
	if !meaningfulReason(reason) {
		return nil, app.BadRequest("documented offboarding reason required")
	}
	switch options.Drafts {
	case "seal", "transfer_owned", "discard":
	default:
		return nil, app.BadRequest("invalid draft disposition")
	}
	p := &company.OffboardingPlan{ID: uuid.New(), TargetID: target, SuccessorID: successor, Options: options, Reason: reason, State: "preview", ExpiresAt: time.Now().UTC().Add(15 * time.Minute)}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if _, e := s.offboardingTarget(ctx, tx, a, target, successor); e != nil {
			return e
		}
		impact, fingerprint, e := offboardingSnapshot(ctx, tx, a.TenantID, target)
		if e != nil {
			return e
		}
		p.Impact = impact
		opts, _ := json.Marshal(options)
		counts, _ := json.Marshal(impact)
		if _, e = tx.Exec(ctx, `INSERT INTO employee_offboarding_plans(id,tenant_id,created_by,target_id,successor_id,options,impact,fingerprint,reason,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.ID, a.TenantID, a.ID, target, successor, opts, counts, fingerprint, reason, p.ExpiresAt); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "employee.offboard.preview", "user", target, map[string]any{"plan_id": p.ID, "successor": successor, "options": options, "impact": impact})
	})
	return p, e
}
func (s *PgStore) ExecuteOffboarding(ctx context.Context, a authz.Actor, target, planID uuid.UUID) (*company.OffboardingPlan, error) {
	p := &company.OffboardingPlan{ID: planID}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		var options, counts []byte
		var fingerprint string
		e := tx.QueryRow(ctx, `SELECT target_id,successor_id,options,impact,fingerprint,reason,state,expires_at,executed_at FROM employee_offboarding_plans WHERE tenant_id=$1 AND created_by=$2 AND id=$3 AND target_id=$4 FOR UPDATE`, a.TenantID, a.ID, planID, target).Scan(&p.TargetID, &p.SuccessorID, &options, &counts, &fingerprint, &p.Reason, &p.State, &p.ExpiresAt, &p.ExecutedAt)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("offboarding plan not found")
		}
		if e != nil {
			return e
		}
		if e = json.Unmarshal(options, &p.Options); e != nil {
			return e
		}
		if e = json.Unmarshal(counts, &p.Impact); e != nil {
			return e
		}
		// Replays return the same disposition receipt; no second session increment,
		// mailbox transfer, queue cancellation or audit is performed.
		if p.State == "executed" {
			return nil
		}
		if time.Now().After(p.ExpiresAt) {
			return app.Conflict("offboarding preview expired; preview again")
		}
		if _, e = s.offboardingTarget(ctx, tx, a, target, p.SuccessorID); e != nil {
			return e
		}
		_, current, e := offboardingSnapshot(ctx, tx, a.TenantID, target)
		if e != nil {
			return e
		}
		if current != fingerprint {
			return app.Conflict("employee assets changed; preview again")
		}
		if e = s.applyOffboarding(ctx, tx, a, target, p.SuccessorID, p.Options, p.Reason, p.ID); e != nil {
			return e
		}
		p.State = "executed"
		return tx.QueryRow(ctx, `UPDATE employee_offboarding_plans SET state='executed',executed_at=now() WHERE id=$1 RETURNING executed_at`, planID).Scan(&p.ExecutedAt)
	})
	return p, e
}
func (s *PgStore) applyOffboarding(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID, options company.OffboardingOptions, reason string, plan uuid.UUID) error {
	if options.Drafts == "transfer_owned" {
		// The row is workspace custody, not a historical-author assertion. Its
		// content/object identity does not change; the transfer is audited below.
		_, e := tx.Exec(ctx, `UPDATE mail_attachments f SET user_id=$3 WHERE f.tenant_id=$1 AND f.user_id=$2 AND EXISTS(SELECT 1 FROM mail_drafts d JOIN mailboxes m ON m.id=d.mailbox_id AND m.tenant_id=d.tenant_id WHERE d.tenant_id=$1 AND d.user_id=$2 AND d.sealed_at IS NULL AND m.owner_user_id=$2 AND d.payload->'attachment_ids' ? f.id::text)`, a.TenantID, target, successor)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE mail_drafts SET user_id=$3,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL AND mailbox_id IN(SELECT id FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2)`, a.TenantID, target, successor)
		if e != nil {
			return e
		}
	}
	draftSQL := `UPDATE mail_drafts SET sealed_at=now(),revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL`
	if options.Drafts == "discard" {
		draftSQL = `DELETE FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL`
	}
	statements := []string{
		draftSQL,
		`UPDATE outbound_jobs j SET state='cancelled',last_error='Cancelled by employee offboarding',claimed_at=NULL,lease_until=NULL,delivery_token=NULL,updated_at=now() WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND state IN ('pending','retry') AND in_flight_domain='' AND NOT EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=j.id AND r.state='uncertain')`,
		`UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$2 AND revoked_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2)`,
		`DELETE FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2`,
		`DELETE FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$2`,
		`DELETE FROM mail_template_grants WHERE tenant_id=$1 AND user_id=$2`,
		`UPDATE users SET is_active=false,session_version=session_version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`,
	}
	for _, stmt := range statements {
		if _, e := tx.Exec(ctx, stmt, a.TenantID, target); e != nil {
			return e
		}
	}
	if _, e := tx.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$3,lifecycle_revision=lifecycle_revision+1 WHERE tenant_id=$1 AND owner_user_id=$2`, a.TenantID, target, successor); e != nil {
		return e
	}
	return companyAudit(ctx, tx, a, "employee.offboard", "user", target, map[string]any{"plan_id": plan, "successor": successor, "reason": reason, "drafts": options.Drafts, "queued_sends": "cancelled; active and uncertain delivery evidence preserved"})
}

var _ company.OffboardingPlanner = (*PgStore)(nil)
