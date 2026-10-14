package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// PatchPermissionEditor owns authorization, CAS, field intent and required audit
// in one transaction. Trigger-owned revisions are never manually consumed here.
func (s *PgStore) PatchPermissionEditor(ctx context.Context, actor authz.Actor, userID uuid.UUID, cmd company.PermissionEditorCommand) (*company.PermissionEditorSnapshot, error) {
	if err := cmd.ExpectedRevision.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	if err := cmd.Patch.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	var out *company.PermissionEditorSnapshot
	err := s.companyTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		before, err := permissionEditorSnapshotTx(ctx, tx, current, userID, true)
		if err != nil {
			return err
		}
		if !cmd.ExpectedRevision.Equal(before.Revision) {
			return app.Conflict("permissions changed; reload before retrying")
		}
		if cmd.Patch.Empty() {
			out = before
			return nil
		}
		p := cmd.Patch
		if err = applyPermissionPatchTx(ctx, tx, userID, before.TenantID, p); err != nil {
			return err
		}
		out, err = permissionEditorSnapshotTx(ctx, tx, current, userID, true)
		if err != nil {
			return err
		}
		return companyAudit(ctx, tx, current, "permission.override.patch", "user", before.UserID, map[string]any{
			"before_revision": before.Revision, "after_revision": out.Revision,
			"changed_fields": permissionPatchFields(p),
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// applyPermissionPatchTx is shared by override and assignment commands. The
// caller must own a refreshed actor, target lock and successful compound CAS.
// It never starts a transaction or writes an audit: the outer command owns
// those boundaries, and only persistence triggers consume permission revisions.
func applyPermissionPatchTx(ctx context.Context, tx pgx.Tx, userID, tenantID uuid.UUID, p company.PermissionPatch) error {
	if err := p.Validate(); err != nil {
		return app.BadRequest(err.Error())
	}
	if p.Empty() {
		return nil
	}
	if p.DomainAccess.Present && !p.DomainAccess.Inherit && p.DomainAccess.Value.Mode == "list" {
		for _, zone := range p.DomainAccess.Value.ZoneIDs {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM domain_zones WHERE id=$1 AND tenant_id=$2 FOR KEY SHARE NOWAIT`, zone, tenantID).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return app.BadRequest("domain zone is unavailable in this company")
			}
			if err != nil {
				var pg *pgconn.PgError
				if errors.As(err, &pg) && pg.Code == "55P03" {
					return app.Conflict("domain zone is changing; reload before retrying")
				}
				return err
			}
		}
	}
	domainMode, domainZones := permissionDomainPatchParams(p.DomainAccess)
	// One upsert per command: omitted fields retain their raw existing columns.
	_, err := tx.Exec(ctx, `INSERT INTO user_permission_overrides
   (id,user_id,can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,can_create_domains,can_create_routes,can_create_api_keys,domain_access_mode,allowed_zone_ids,updated_at)
   VALUES ($1,$2,$4,$6,$8,$10,$12,$14,$16,$18,$20,$21,clock_timestamp())
   ON CONFLICT(user_id) DO UPDATE SET
    can_send=CASE WHEN $3 THEN EXCLUDED.can_send ELSE user_permission_overrides.can_send END,
    daily_send_quota=CASE WHEN $5 THEN EXCLUDED.daily_send_quota ELSE user_permission_overrides.daily_send_quota END,
    daily_receive_quota=CASE WHEN $7 THEN EXCLUDED.daily_receive_quota ELSE user_permission_overrides.daily_receive_quota END,
    max_mailboxes=CASE WHEN $9 THEN EXCLUDED.max_mailboxes ELSE user_permission_overrides.max_mailboxes END,
    max_domains=CASE WHEN $11 THEN EXCLUDED.max_domains ELSE user_permission_overrides.max_domains END,
    can_create_domains=CASE WHEN $13 THEN EXCLUDED.can_create_domains ELSE user_permission_overrides.can_create_domains END,
    can_create_routes=CASE WHEN $15 THEN EXCLUDED.can_create_routes ELSE user_permission_overrides.can_create_routes END,
    can_create_api_keys=CASE WHEN $17 THEN EXCLUDED.can_create_api_keys ELSE user_permission_overrides.can_create_api_keys END,
    domain_access_mode=CASE WHEN $19 THEN EXCLUDED.domain_access_mode ELSE user_permission_overrides.domain_access_mode END,
    allowed_zone_ids=CASE WHEN $19 THEN EXCLUDED.allowed_zone_ids ELSE user_permission_overrides.allowed_zone_ids END,
    updated_at=clock_timestamp()`,
		uuid.New(), userID, p.CanSend.Present, permissionFieldParam(p.CanSend),
		p.DailySendQuota.Present, permissionFieldParam(p.DailySendQuota),
		p.DailyReceiveQuota.Present, permissionFieldParam(p.DailyReceiveQuota),
		p.MaxMailboxes.Present, permissionFieldParam(p.MaxMailboxes),
		p.MaxDomains.Present, permissionFieldParam(p.MaxDomains),
		p.CanCreateDomains.Present, permissionFieldParam(p.CanCreateDomains),
		p.CanCreateRoutes.Present, permissionFieldParam(p.CanCreateRoutes),
		p.CanCreateAPIKeys.Present, permissionFieldParam(p.CanCreateAPIKeys),
		p.DomainAccess.Present, domainMode, domainZones)
	return err
}

func permissionFieldParam[T any](f company.PermissionField[T]) any {
	if !f.Present || f.Inherit {
		return nil
	}
	return f.Value
}
func permissionDomainPatchParams(f company.PermissionField[company.DomainAccess]) (any, any) {
	if !f.Present {
		return nil, nil
	}
	if f.Inherit || f.Value.Mode == "inherit" {
		return "inherit", nil
	}
	if f.Value.Mode == "list" {
		return "list", uuidSliceParam(f.Value.ZoneIDs)
	}
	return f.Value.Mode, uuidSliceParam([]uuid.UUID{})
}
func permissionPatchFields(p company.PermissionPatch) []string {
	out := []string{}
	for _, field := range []struct {
		name    string
		present bool
	}{
		{"can_send", p.CanSend.Present}, {"daily_send_quota", p.DailySendQuota.Present},
		{"daily_receive_quota", p.DailyReceiveQuota.Present}, {"max_mailboxes", p.MaxMailboxes.Present},
		{"max_domains", p.MaxDomains.Present}, {"can_create_domains", p.CanCreateDomains.Present},
		{"can_create_routes", p.CanCreateRoutes.Present}, {"can_create_api_keys", p.CanCreateAPIKeys.Present},
		{"domain_access", p.DomainAccess.Present},
	} {
		if field.present {
			out = append(out, field.name)
		}
	}
	return out
}
