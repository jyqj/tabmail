package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

// DeleteZone is deliberately not a mailbox lifecycle operation. A domain may
// only be removed when it has no company assets, primary-domain reference,
// child domains or durable ingress provenance. The FK in migration 00009 is
// the final guard against concurrent mailbox creation and direct SQL deletes.
// The tenant bound and mandatory audit commit with the delete; callers cannot
// report success after losing the audit trail.
func (s *PgStore) DeleteZone(ctx context.Context, id uuid.UUID, entry models.AuditEntry) error {
	if entry.TenantID == nil || entry.Actor == "" {
		return app.BadRequest("domain deletion requires tenant and audit actor")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Match company administration's tenant -> resource lock order.
	if err = lockMemberTenant(ctx, tx, *entry.TenantID); err != nil {
		return err
	}
	var domain string
	err = tx.QueryRow(ctx, `SELECT domain FROM domain_zones WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, *entry.TenantID).Scan(&domain)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.NotFound("domain not found")
	}
	if err != nil {
		return err
	}
	var used bool
	err = tx.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM company_settings WHERE primary_zone_id=$1) OR
 EXISTS(SELECT 1 FROM mailboxes WHERE zone_id=$1) OR
 EXISTS(SELECT 1 FROM messages WHERE zone_id=$1) OR
 EXISTS(SELECT 1 FROM outbound_jobs WHERE zone_id=$1) OR
 EXISTS(SELECT 1 FROM ingest_recipient_outcomes WHERE zone_id=$1) OR
 EXISTS(SELECT 1 FROM domain_zones WHERE parent_zone_id=$1) OR
 EXISTS(SELECT 1 FROM employee_invitations WHERE tenant_id=$2 AND split_part(mailbox_address,'@',2)=$3 AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>now())`, id, *entry.TenantID, domain).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return app.Conflict("domain has company assets or references; preserve it and migrate its mailboxes before removal")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM domain_zones WHERE id=$1 AND tenant_id=$2`, id, *entry.TenantID); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23503" {
			return app.Conflict("domain acquired a reference; reload before removal")
		}
		return err
	}
	details, err := mergeAuditDetail(entry.Details, "domain", domain)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,'domain.delete','domain_zone',$3,$4)`, *entry.TenantID, entry.Actor, id, details); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
