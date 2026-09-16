package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ store.MemberGuardStore = (*PgStore)(nil)

// Lock the company before the target: locking only the two target rows allows
// two concurrent removals to both observe another active administrator.
func lockMemberTenant(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrMemberNotFound
	}
	return err
}

func currentMemberActor(ctx context.Context, tx pgx.Tx, actor authz.Actor, tenant uuid.UUID) (authz.Actor, error) {
	if actor.Type != authz.PrincipalUser || actor.TenantID != tenant {
		return actor, authz.ErrForbidden("interactive company administrator required")
	}
	u, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1`, actor.ID))
	if err != nil {
		return actor, err
	}
	if u == nil || !u.IsActive || (u.TenantID != tenant && u.Role != models.RoleSuperAdmin) {
		return actor, authz.ErrForbidden("administrator no longer active in this company")
	}
	actor.IsAdmin = u.Role == models.RoleAdmin
	actor.IsSuperAdmin = u.Role == models.RoleSuperAdmin
	actor.Role = u.Role
	return actor, nil
}

func guardMemberRemoval(ctx context.Context, tx pgx.Tx, old, next *models.User) error {
	if !models.IsActiveAdministrator(old) || models.IsActiveAdministrator(next) {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id=$1 AND id<>$2 AND is_active AND role IN ('admin','super_admin')`, old.TenantID, old.ID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return store.ErrLastAdministrator
	}
	return nil
}

func insertMemberAudit(ctx context.Context, tx pgx.Tx, actor authz.Actor, target *models.User, action string, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,$3,'user',$4,$5)`, target.TenantID, actor.AuditLabel(), action, target.ID, data)
	return err
}

func (s *PgStore) UpdateUserGuarded(ctx context.Context, actor authz.Actor, tenant, target uuid.UUID, patch models.UserAdminPatch) (*models.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockMemberTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	actor, err = currentMemberActor(ctx, tx, actor, tenant)
	if err != nil {
		return nil, err
	}
	old, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, target, tenant))
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, store.ErrMemberNotFound
	}
	if !authz.CanManageTenantMember(actor, tenant, old.Role) {
		return nil, authz.ErrForbidden("cannot manage this member's role")
	}
	next := *old
	patch.Apply(&next)
	if next.Role != models.RoleUser && next.Role != models.RoleAdmin && next.Role != models.RoleSuperAdmin {
		return nil, authz.ErrForbidden("invalid member role")
	}
	if patch.Role != nil && next.Role == models.RoleSuperAdmin && !actor.IsSuperAdmin {
		return nil, authz.ErrForbidden("only super admin can assign super_admin role")
	}
	if err = guardMemberRemoval(ctx, tx, old, &next); err != nil {
		return nil, err
	}
	// Recheck profile tenant under the same transaction, not only in the handler.
	if patch.SetPermissionProfile && patch.PermissionProfileID != nil {
		var allowed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM permission_profiles WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2))`, *patch.PermissionProfileID, tenant).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, authz.ErrForbidden("permission profile unavailable in this company")
		}
	}
	if old.IsActive != next.IsActive || old.Role != next.Role {
		next.SessionVersion++
	}
	if old.IsActive && !next.IsActive {
		// User lock serializes against refresh rotation's FOR SHARE. Do not take
		// family locks here: family->user is rotation's fixed lock order.
		if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`, target); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2`, tenant, target); err != nil {
			return nil, err
		}
	}
	next.UpdatedAt = time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE users SET display_name=$3,role=$4,is_active=$5,permission_profile_id=$6,updated_at=$7,session_version=$8 WHERE id=$1 AND tenant_id=$2`, target, tenant, next.DisplayName, next.Role, next.IsActive, next.PermissionProfileID, next.UpdatedAt, next.SessionVersion)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, store.ErrMemberNotFound
	}
	if err = insertMemberAudit(ctx, tx, actor, old, "user.update", map[string]any{"old_role": old.Role, "new_role": next.Role, "old_active": old.IsActive, "new_active": next.IsActive}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *PgStore) DeleteUserGuarded(ctx context.Context, actor authz.Actor, tenant, target uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockMemberTenant(ctx, tx, tenant); err != nil {
		return err
	}
	actor, err = currentMemberActor(ctx, tx, actor, tenant)
	if err != nil {
		return err
	}
	old, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, target, tenant))
	if err != nil {
		return err
	}
	if old == nil {
		return store.ErrMemberNotFound
	}
	if !authz.CanManageTenantMember(actor, tenant, old.Role) {
		return authz.ErrForbidden("cannot manage this member's role")
	}
	if actor.ID == target {
		return authz.ErrForbidden("cannot delete yourself")
	}
	if err = guardMemberRemoval(ctx, tx, old, nil); err != nil {
		return err
	}
	var owns bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2)`, tenant, target).Scan(&owns); err != nil {
		return err
	}
	if owns {
		return store.ErrMemberOwnsMailbox
	}
	if _, err = tx.Exec(ctx, `DELETE FROM tenant_api_keys WHERE owner_user_id=$1 AND tenant_id=$2`, target, tenant); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1 AND tenant_id=$2`, target, tenant)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return store.ErrMemberNotFound
	}
	if err = insertMemberAudit(ctx, tx, actor, old, "user.delete", map[string]any{"role": old.Role}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
