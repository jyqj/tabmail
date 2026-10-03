package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/app/credentials"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"time"
)

// Lock order: tenant -> departing user -> successor -> active jobs -> owned
// mailboxes (ORDER BY id). Draft and upload writes take a user SHARE lock;
// enqueue has the same database fence. A preview never reads or returns
// private draft/message content.
func offboardingSubjectsTx(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID, allowInactiveTarget bool) (*models.User, error) {
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
	// Access suspension is not disposition completion. A frozen employee can
	// be managed without reactivation; the durable completion epoch is checked
	// separately under this same target fence.
	if !allowInactiveTarget && !u.IsActive {
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
	return u, nil
}

func (s *PgStore) offboardingTarget(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID) (*models.User, error) {
	// The legacy one-step caller has no qualified plan/completion epoch. Keep
	// its original inactive guard; only the formal planner admits frozen targets.
	return s.offboardingQualifiedTarget(ctx, tx, a, target, successor, false)
}

func (s *PgStore) offboardingPlanTarget(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID) (*models.User, error) {
	return s.offboardingQualifiedTarget(ctx, tx, a, target, successor, true)
}

func (s *PgStore) offboardingQualifiedTarget(ctx context.Context, tx pgx.Tx, a authz.Actor, target, successor uuid.UUID, allowInactiveTarget bool) (*models.User, error) {
	u, e := offboardingSubjectsTx(ctx, tx, a, target, successor, allowInactiveTarget)
	if e != nil {
		return nil, e
	}
	targetEpoch, _, e := offboardingEpochsTx(ctx, tx, a.TenantID, target, successor)
	if e != nil {
		return nil, e
	}
	var completed bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee_offboarding_plans WHERE tenant_id=$1 AND target_id=$2 AND state='executed' AND fingerprint LIKE $3)`, a.TenantID, target, offboardingExecutedPrefix+targetEpoch+":%").Scan(&completed); e != nil {
		return nil, e
	}
	if completed {
		return nil, app.Conflict("employee disposition already completed for the current identity; do not repeat it")
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

const offboardingExecutedPrefix = "executed-v1:"

// These are actual persisted identity/authority epochs, not timestamps or
// cached user values. The caller already holds the tenant, target and successor
// fences. Bind migration16's durable user revision AND nullable assigned profile
// identity/revision: profile content ABA need not change the user's revision.
// Keep target and successor hashes separate: changed successor credentials must
// not make a completed target epoch eligible for a second disposition.
func offboardingEpochsTx(ctx context.Context, tx pgx.Tx, tenant, target, successor uuid.UUID) (string, string, error) {
	departing, e := offboardingAuthorityEpochTx(ctx, tx, tenant, target)
	if e != nil {
		return "", "", e
	}
	receiving, e := offboardingAuthorityEpochTx(ctx, tx, tenant, successor)
	if e != nil {
		return "", "", e
	}
	return departing, receiving, nil
}

// Subject rows are already fenced. Profiles follow user locks and use SHARE
// NOWAIT like the existing permission editor: a parent mutation must not form
// a user->profile/profile->user wait cycle. This reads only persistent version
// ownership; it does not invoke member editing policy or merge effective rights.
func offboardingAuthorityEpochTx(ctx context.Context, tx pgx.Tx, tenant, user uuid.UUID) (string, error) {
	var binding struct {
		Identity        json.RawMessage `json:"identity"`
		ProfileID       *uuid.UUID      `json:"profile_id"`
		ProfileRevision *string         `json:"profile_revision"`
	}
	if e := tx.QueryRow(ctx, `SELECT jsonb_build_array(tenant_id,id,role,is_active,session_version,permission_revision),permission_profile_id FROM users WHERE tenant_id=$1 AND id=$2`, tenant, user).Scan(&binding.Identity, &binding.ProfileID); e != nil {
		return "", e
	}
	if binding.ProfileID != nil {
		var revision string
		e := tx.QueryRow(ctx, `SELECT permission_revision::text FROM permission_profiles WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2) FOR SHARE NOWAIT`, *binding.ProfileID, tenant).Scan(&revision)
		if errors.Is(e, pgx.ErrNoRows) {
			return "", app.Conflict("assigned permission profile is unavailable; observe the current lifecycle again")
		}
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "55P03" {
			return "", app.Conflict("assigned permission profile is changing; observe the current lifecycle again")
		}
		if e != nil {
			return "", e
		}
		binding.ProfileRevision = &revision
	}
	raw, e := json.Marshal(binding)
	if e != nil {
		return "", e
	}
	return company.Hash(string(raw)), nil
}

func offboardingPreviewDeadlineTx(ctx context.Context, tx pgx.Tx, expiresAt time.Time) error {
	var expired bool
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, expiresAt).Scan(&expired); e != nil {
		return e
	}
	if expired {
		return app.Conflict("offboarding preview expired; preview again")
	}
	return nil
}

func offboardingSnapshot(ctx context.Context, tx pgx.Tx, tenant, target, successor uuid.UUID) (company.OffboardingImpact, string, error) {
	impact := company.OffboardingImpact{}
	targetEpoch, successorEpoch, e := offboardingEpochsTx(ctx, tx, tenant, target, successor)
	if e != nil {
		return impact, "", e
	}
	var raw []byte
	// IDs/versions, not just counts: same-count replacements invalidate a plan.
	// Incoming mail does not invalidate handover; ownership transfers the live
	// mailbox as a whole. Private draft revisions and queued outcomes do.
	e = tx.QueryRow(ctx, `SELECT jsonb_build_object(
 'user',(SELECT jsonb_build_array(tenant_id,id,is_active,session_version,role,permission_revision) FROM users WHERE tenant_id=$1 AND id=$2),
 'successor',(SELECT jsonb_build_array(tenant_id,id,is_active,session_version,role,permission_revision) FROM users WHERE tenant_id=$1 AND id=$3),
 'mailboxes',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,lifecycle_revision) ORDER BY id) FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2),'[]'),
 'drafts',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,revision,mailbox_id) ORDER BY id) FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL),'[]'),
 'uploads',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,state) ORDER BY id) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'keys',COALESCE((SELECT jsonb_agg(id ORDER BY id) FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2),'[]'),
 'grants',COALESCE((SELECT jsonb_agg(jsonb_build_array(mailbox_id,can_read,can_organize,can_send,template_only) ORDER BY mailbox_id) FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'template_grants',COALESCE((SELECT jsonb_agg(jsonb_build_array(template_id,mailbox_id) ORDER BY template_id,mailbox_id) FROM mail_template_grants WHERE tenant_id=$1 AND user_id=$2),'[]'),
 'jobs',COALESCE((SELECT jsonb_agg(jsonb_build_array(id,state,updated_at,in_flight_domain,(SELECT COALESCE(jsonb_agg(jsonb_build_array(r.address,r.state,r.attempts,r.updated_at) ORDER BY r.address),'[]') FROM outbound_recipients r WHERE r.tenant_id=$1 AND r.job_id=outbound_jobs.id)) ORDER BY id) FROM outbound_jobs WHERE tenant_id=$1 AND (sender_user_id=$2 OR user_id=$2) AND (state IN ('pending','retry','processing') OR in_flight_domain<>'' OR EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=outbound_jobs.id AND r.state='uncertain'))),'[]'))`, tenant, target, successor).Scan(&raw)
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
	// Profiles stay locked through the full asset read and commit. No effective
	// values, timestamps or cached profile observations stand in for revision.
	return impact, company.Hash(targetEpoch + ":" + successorEpoch + ":" + string(raw)), e
}
func (s *PgStore) PreviewOffboarding(ctx context.Context, a authz.Actor, target, successor uuid.UUID, options company.OffboardingOptions, reason string) (*company.OffboardingPlan, error) {
	reason, reasonErr := credentials.AuditReason(reason)
	if reasonErr != nil {
		return nil, app.BadRequest("documented offboarding reason required")
	}
	switch options.Drafts {
	case "seal", "transfer_owned", "discard":
	default:
		return nil, app.BadRequest("invalid draft disposition")
	}
	p := &company.OffboardingPlan{ID: uuid.New(), TargetID: target, SuccessorID: successor, Options: options, Reason: reason, State: "preview"}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if _, e := s.offboardingPlanTarget(ctx, tx, a, target, successor); e != nil {
			return e
		}
		impact, fingerprint, e := offboardingSnapshot(ctx, tx, a.TenantID, target, successor)
		if e != nil {
			return e
		}
		p.Impact = impact
		// Lease begins after authority/asset lock waits, using the DB clock.
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()+interval '15 minutes'`).Scan(&p.ExpiresAt); e != nil {
			return e
		}
		opts, _ := json.Marshal(options)
		counts, _ := json.Marshal(impact)
		if e = tx.QueryRow(ctx, `INSERT INTO employee_offboarding_plans(id,tenant_id,created_by,target_id,successor_id,options,impact,fingerprint,reason,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING expires_at`, p.ID, a.TenantID, a.ID, target, successor, opts, counts, fingerprint, reason, p.ExpiresAt).Scan(&p.ExpiresAt); e != nil {
			return e
		}
		if e = companyAudit(ctx, tx, a, "employee.offboard.preview", "user", target, map[string]any{"plan_id": p.ID, "successor": successor, "options": options, "impact": impact}); e != nil {
			return e
		}
		return offboardingPreviewDeadlineTx(ctx, tx, p.ExpiresAt)
	})
	if e != nil {
		return nil, e
	}
	return p, nil
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
			if _, e = offboardingSubjectsTx(ctx, tx, a, target, p.SuccessorID, true); e != nil {
				return e
			}
			targetEpoch, successorEpoch, e := offboardingEpochsTx(ctx, tx, a.TenantID, target, p.SuccessorID)
			if e != nil {
				return e
			}
			if !strings.HasPrefix(fingerprint, offboardingExecutedPrefix) || fingerprint != offboardingExecutedPrefix+targetEpoch+":"+successorEpoch {
				return app.Conflict("offboarding completion identity changed or is unverified; do not replay the old plan")
			}
			// A completed receipt is not a still-pending preview. Its old preview
			// deadline does not expire qualified same-epoch receipt replay.
			return nil
		}
		if e = offboardingPreviewDeadlineTx(ctx, tx, p.ExpiresAt); e != nil {
			return e
		}
		if _, e = s.offboardingPlanTarget(ctx, tx, a, target, p.SuccessorID); e != nil {
			return e
		}
		_, current, e := offboardingSnapshot(ctx, tx, a.TenantID, target, p.SuccessorID)
		if e != nil {
			return e
		}
		if current != fingerprint {
			return app.Conflict("employee assets changed; preview again")
		}
		if e = s.applyOffboarding(ctx, tx, a, target, p.SuccessorID, p.Options, p.Reason, p.ID); e != nil {
			return e
		}
		// Audit/outbox writes may wait; a pending preview cannot commit effects
		// based on the pre-wait time even though its exact plan row is fenced.
		if e = offboardingPreviewDeadlineTx(ctx, tx, p.ExpiresAt); e != nil {
			return e
		}
		targetEpoch, successorEpoch, e := offboardingEpochsTx(ctx, tx, a.TenantID, target, p.SuccessorID)
		if e != nil {
			return e
		}
		p.State = "executed"
		return tx.QueryRow(ctx, `UPDATE employee_offboarding_plans SET state='executed',executed_at=clock_timestamp(),fingerprint=$2 WHERE id=$1 RETURNING executed_at`, planID, offboardingExecutedPrefix+targetEpoch+":"+successorEpoch).Scan(&p.ExecutedAt)
	})
	if e != nil {
		return nil, e
	}
	return p, nil
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
	if e := transferOwnedMailboxes(ctx, tx, a.TenantID, target, successor); e != nil {
		return e
	}
	return companyAudit(ctx, tx, a, "employee.offboard", "user", target, map[string]any{"plan_id": plan, "successor": successor, "reason": reason, "drafts": options.Drafts, "queued_sends": "cancelled; active and uncertain delivery evidence preserved"})
}

var _ company.OffboardingPlanner = (*PgStore)(nil)
