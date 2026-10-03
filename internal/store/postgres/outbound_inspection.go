package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/app/credentials"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

var _ company.OutboundRecoveryInspector = (*PgStore)(nil)

// Production submission admission is capped at 50 envelope recipients. Read
// one overflow sentinel and fail, never emit a truncated ledger as complete.
const outboundInspectionMaxRecipients = 50

func (s *PgStore) InspectOutboundRecovery(ctx context.Context, a authz.Actor, id uuid.UUID, reason string) (out *company.OutboundInspection, err error) {
	defer func() {
		if err != nil {
			out = nil
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "40001") {
			err = app.Conflict("send job or recipient ledger is changing; inspect again")
		}
	}()
	reason, e := credentials.AuditReason(reason)
	if e != nil {
		return nil, app.BadRequest("inspection reason must be 8-1000 bytes")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	// Keep the established recovery contract: selected tenant FK KEY SHARE,
	// current interactive actor SHARE. It re-derives role/active/tenant/session
	// after waits; neither a cached router super flag nor receipt visibility is
	// an exception grant. Tenant lifecycle scope remains recovery's own scope.
	a, e = recoveryReferencedActor(ctx, tx, a)
	if e != nil {
		return nil, e
	}
	j := &models.OutboundJob{}
	// Only fetch fields needed by the explicit DTO and completeness check. No
	// raw MIME, object keys, claim token, credential, lease or raw SMTP response.
	e = tx.QueryRow(ctx, `SELECT id,tenant_id,state,created_at,updated_at,mail_from,to_addrs,cc_addrs,bcc_addrs,subject,text_body,html_body,headers_json,rcpt_to,recipient_ledger,in_flight_domain FROM outbound_jobs WHERE tenant_id=$1 AND id=$2 FOR SHARE NOWAIT`, a.TenantID, id).Scan(&j.ID, &j.TenantID, &j.State, &j.CreatedAt, &j.UpdatedAt, &j.MailFrom, &j.To, &j.CC, &j.BCC, &j.Subject, &j.TextBody, &j.HTMLBody, &j.HeadersJSON, &j.RcptTo, &j.RecipientLedger, &j.InFlightDomain)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, app.NotFound("send job not found")
	}
	if e != nil {
		return nil, e
	}
	// Worker/reconcile writers fence job before ledger. Job SHARE fixes the job
	// version while this single statement reads and fences the complete ledger.
	// NOWAIT on both prevents actor->job or job->recipient wait cycles, including
	// direct ledger blockers. Rows stay locked through audit/outbox commit.
	rows, e := tx.Query(ctx, `SELECT address,state,smtp_code,diagnostic,attempts,updated_at FROM outbound_recipients WHERE tenant_id=$1 AND job_id=$2 ORDER BY address LIMIT $3 FOR SHARE NOWAIT`, a.TenantID, id, outboundInspectionMaxRecipients+1)
	if e != nil {
		return nil, e
	}
	ledger := make([]company.Recipient, 0, outboundInspectionMaxRecipients)
	for rows.Next() {
		var r company.Recipient
		e = rows.Scan(&r.Address, &r.State, &r.SMTPCode, &r.Diagnostic, &r.Attempts, &r.UpdatedAt)
		if e != nil {
			rows.Close()
			return nil, e
		}
		ledger = append(ledger, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(ledger) > outboundInspectionMaxRecipients || len(j.RcptTo) > outboundInspectionMaxRecipients {
		return nil, app.Conflict("recipient ledger exceeds bounded inspection; investigate separately")
	}
	v := company.ProjectOutboundInspection(j, ledger)
	if e = companyAudit(ctx, tx, a, "outbound.break_glass", "outbound_job", id, map[string]any{"reason": reason, "inspection_version": j.UpdatedAt, "recipient_count": len(ledger)}); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return v, nil
}
