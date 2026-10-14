package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

var _ permissions.ProfileVisibilityStore = (*PgStore)(nil)

// ListVisiblePermissionProfiles qualifies the formal management list before
// reading any profile fields. The company read boundary refreshes and fences
// interactive identity, role, active state and authenticated session version.
// A selected company is context, not a restriction on the existing platform
// administrator's all-profile public scope. References/event audiences never
// grant list visibility to another company's local profiles.
func (s *PgStore) ListVisiblePermissionProfiles(ctx context.Context, actor authz.Actor, selected *uuid.UUID) ([]*models.PermissionProfile, error) {
	if actor.Type != authz.PrincipalUser || actor.ID == uuid.Nil {
		return nil, app.Forbidden("interactive administrator required")
	}
	if selected != nil {
		actor.TenantID = *selected
	}
	if actor.TenantID == uuid.Nil {
		return nil, app.Forbidden("no tenant context")
	}
	var items []*models.PermissionProfile
	err := s.companyReadTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		if selected == nil && !current.IsSuperAdmin {
			return app.Forbidden("no tenant context")
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id=$1)`, current.TenantID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return app.NotFound("company not found")
		}
		query := permProfileSelect
		var args []any
		if !current.IsSuperAdmin {
			query += ` WHERE tenant_id=$1 OR tenant_id IS NULL`
			args = append(args, current.TenantID)
		}
		// The current wire API is an unpaginated array. Keep its scope, with an
		// ID tie-breaker so future pagination cannot order equal names loosely.
		rows, err := tx.Query(ctx, query+` ORDER BY name,id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []*models.PermissionProfile{}
		for rows.Next() {
			p, err := scanPermProfile(rows)
			if err != nil {
				return err
			}
			items = append(items, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}
