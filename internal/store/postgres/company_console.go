package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func (s *PgStore) CompanyOverview(ctx context.Context, a authz.Actor) (*company.Overview, error) {
	v := &company.Overview{}
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		return tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM mailboxes WHERE tenant_id=$1),
 (SELECT count(*) FROM users WHERE tenant_id=$1 AND is_active),
 (SELECT count(*) FROM employee_invitations WHERE tenant_id=$1 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>now()),
 (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND state IN ('pending','retry','processing')),
 (SELECT count(*) FROM outbound_jobs j WHERE j.tenant_id=$1 AND (in_flight_domain<>'' OR EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.tenant_id=$1 AND r.job_id=j.id AND r.state='uncertain'))),
 (SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1 AND state='failed')`, a.TenantID).Scan(&v.Mailboxes, &v.ActiveEmployees, &v.PendingInvitations, &v.Queued, &v.Uncertain, &v.IndexFailed)
	})
	return v, e
}
func (s *PgStore) ListCompanyAudit(ctx context.Context, a authz.Actor, p models.Page) ([]company.AdminAudit, int, error) {
	p = p.Normalize()
	out := []company.AdminAudit{}
	total := 0
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		const filter = `tenant_id=$1 AND (action LIKE 'company.%' OR action LIKE 'employee.%' OR action LIKE 'mailbox.%' OR action LIKE 'template.%' OR action LIKE 'domain.%')`
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE `+filter, a.TenantID).Scan(&total); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,COALESCE(actor,''),action,COALESCE(resource_type,''),resource_id,COALESCE(details->>'reason',''),created_at FROM audit_log WHERE `+filter+` ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, a.TenantID, p.PerPage, p.Offset())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v company.AdminAudit
			if e = rows.Scan(&v.ID, &v.Actor, &v.Action, &v.ResourceType, &v.ResourceID, &v.Reason, &v.CreatedAt); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, total, e
}
