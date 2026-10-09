package postgres

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// GetPermissionEditorSnapshot returns raw edit intent, never reverse-engineered
// from the merged permission. Actor refresh and all observations share one tx.
func (s *PgStore) GetPermissionEditorSnapshot(ctx context.Context, actor authz.Actor, userID uuid.UUID) (*company.PermissionEditorSnapshot, error) {
	var out *company.PermissionEditorSnapshot
	err := s.companyReadTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		var err error
		out, err = permissionEditorSnapshotTx(ctx, tx, current, userID, false)
		return err
	})
	return out, err
}

// permissionEditorSnapshotTx requires a freshly reloaded interactive actor.
// Its stable user lock fences assignment and absent override rows; profile SHARE
// NOWAIT avoids the SET NULL parent->user deletion deadlock. Writers compare
// the complete Revision before any mutation and retain these locks until commit.
func permissionEditorSnapshotTx(ctx context.Context, tx pgx.Tx, current authz.Actor, userID uuid.UUID, write bool) (*company.PermissionEditorSnapshot, error) {
	lock := " FOR SHARE"
	if write {
		lock = " FOR NO KEY UPDATE"
	}
	u, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2`+lock, userID, current.TenantID))
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, app.NotFound("employee not found")
	}
	if !authz.CanManageTenantMember(current, u.TenantID, u.Role) {
		return nil, authz.ErrForbidden("cannot manage this member's permissions")
	}
	out := &company.PermissionEditorSnapshot{UserID: u.ID, TenantID: u.TenantID, FieldSources: map[string]string{}, Capabilities: company.PermissionEditorCapabilities{Patch: true, AssignProfile: true}}
	out.Revision.UserID = u.ID
	out.Revision.TenantID = u.TenantID
	var userRevision int64
	if err = tx.QueryRow(ctx, `SELECT permission_revision FROM users WHERE id=$1 AND tenant_id=$2`, u.ID, u.TenantID).Scan(&userRevision); err != nil {
		return nil, err
	}
	out.Revision.UserRevision = strconv.FormatInt(userRevision, 10)
	if u.PermissionProfileID != nil {
		out.Profile, err = scanPermProfile(tx.QueryRow(ctx, permProfileSelect+` WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2) FOR SHARE NOWAIT`, *u.PermissionProfileID, u.TenantID))
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "55P03" {
				return nil, app.Conflict("permission profile is changing; reload before retrying")
			}
			return nil, err
		}
		if out.Profile == nil {
			return nil, app.Conflict("assigned profile is unavailable; reload before retrying")
		}
		var profileRevision int64
		if err = tx.QueryRow(ctx, `SELECT permission_revision FROM permission_profiles WHERE id=$1`, out.Profile.ID).Scan(&profileRevision); err != nil {
			return nil, err
		}
		revision := strconv.FormatInt(profileRevision, 10)
		profileID := out.Profile.ID
		out.Revision.ProfileID = &profileID
		out.Revision.ProfileRevision = &revision
	}
	raw := &company.RawPermissionOverrides{}
	var mode *string
	err = tx.QueryRow(ctx, `SELECT can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,allowed_zone_ids,can_create_domains,can_create_routes,can_create_api_keys,domain_access_mode FROM user_permission_overrides WHERE user_id=$1`, u.ID).Scan(&raw.CanSend, &raw.DailySendQuota, &raw.DailyReceiveQuota, &raw.MaxMailboxes, &raw.MaxDomains, &raw.AllowedZoneIDs, &raw.CanCreateDomains, &raw.CanCreateRoutes, &raw.CanCreateAPIKeys, &mode)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		raw.DomainAccess = company.DomainAccess{Mode: "inherit"}
		if mode != nil {
			raw.DomainAccess.Mode = *mode
		} else if raw.AllowedZoneIDs != nil {
			raw.DomainAccess.Mode = "all"
			if len(raw.AllowedZoneIDs) > 0 {
				raw.DomainAccess.Mode = "list"
			}
		}
		raw.DomainAccess.ZoneIDs = raw.AllowedZoneIDs
		if raw.DomainAccess.ZoneIDs == nil {
			raw.DomainAccess.ZoneIDs = []uuid.UUID{}
		}
		out.Overrides = raw
	}
	source := func(overridden bool) string {
		if overridden {
			return "override"
		}
		if out.Profile != nil {
			return "profile"
		}
		return "default"
	}
	out.FieldSources["can_send"] = source(raw.CanSend != nil)
	out.FieldSources["daily_send_quota"] = source(raw.DailySendQuota != nil)
	out.FieldSources["daily_receive_quota"] = source(raw.DailyReceiveQuota != nil)
	out.FieldSources["max_mailboxes"] = source(raw.MaxMailboxes != nil)
	out.FieldSources["max_domains"] = source(raw.MaxDomains != nil)
	out.FieldSources["can_create_domains"] = source(raw.CanCreateDomains != nil)
	out.FieldSources["can_create_routes"] = source(raw.CanCreateRoutes != nil)
	out.FieldSources["can_create_api_keys"] = source(raw.CanCreateAPIKeys != nil)
	out.FieldSources["domain_access"] = source(out.Overrides != nil && raw.DomainAccess.Mode != "inherit")
	out.Effective, err = effectivePermission(ctx, tx, u.ID)
	if err != nil {
		return nil, err
	}
	return out, nil
}
