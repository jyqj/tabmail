package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ permissions.ProfileCreationStore = (*PgStore)(nil)

// CreatePermissionProfileGuarded consumes current interactive authority, target
// scope, domain qualification, persisted revision and required audit atomically.
// The legacy pool writer remains available to trusted bootstrap/fixtures only;
// this is the shipping management service's sole creation port.
func (s *PgStore) CreatePermissionProfileGuarded(ctx context.Context, actor authz.Actor, selected *uuid.UUID, requested *models.PermissionProfile) (*models.PermissionProfile, error) {
	if actor.Type != authz.PrincipalUser || actor.ID == uuid.Nil {
		return nil, app.Forbidden("interactive administrator required")
	}
	if requested == nil || strings.TrimSpace(requested.Name) == "" || len([]rune(requested.Name)) > 64 {
		return nil, app.BadRequest("valid permission profile name required")
	}
	if requested.IsSystem {
		return nil, app.Forbidden("system permission profiles cannot be created through management")
	}
	for _, n := range []int{requested.DailySendQuota, requested.DailyReceiveQuota, requested.MaxMailboxes, requested.MaxDomains} {
		if n < 0 || int64(n) > 2147483647 {
			return nil, app.BadRequest("quota must be a nonnegative database integer")
		}
	}
	desired := *requested
	desired.AllowedZoneIDs = append([]uuid.UUID(nil), requested.AllowedZoneIDs...)
	if requested.AllowedZoneIDs != nil && len(requested.AllowedZoneIDs) == 0 {
		desired.AllowedZoneIDs = []uuid.UUID{}
	}
	if requested.TenantID != nil {
		id := *requested.TenantID
		desired.TenantID = &id
	}
	if desired.ID == uuid.Nil {
		desired.ID = uuid.New()
	}
	// Locate the home anchor without treating this preliminary read as authority.
	// companyTx then takes tenant-before-user locks and reloads the credential;
	// the locked persistent home must match again before any creation occurs.
	anchor := actor.TenantID
	if selected != nil {
		anchor = *selected
	} else {
		if err := s.pool.QueryRow(ctx, `SELECT tenant_id FROM users WHERE id=$1`, actor.ID).Scan(&anchor); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, app.Forbidden("administrator unavailable")
			}
			return nil, err
		}
	}
	if anchor == uuid.Nil {
		return nil, app.Forbidden("a persistent company audit anchor is required")
	}
	actor.TenantID = anchor
	var out *models.PermissionProfile
	err := s.companyTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		if selected == nil {
			if !current.IsSuperAdmin {
				return app.Forbidden("selected tenant is required for company administrators")
			}
			var home uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT tenant_id FROM users WHERE id=$1`, current.ID).Scan(&home); err != nil {
				return err
			}
			if home != anchor {
				return app.Conflict("administrator company changed; reload before retrying")
			}
		}
		// Hints never select scope: only the role reloaded after lock waits does.
		if !current.IsSuperAdmin {
			id := current.TenantID
			desired.TenantID = &id
		}
		if desired.TenantID != nil {
			if *desired.TenantID == uuid.Nil {
				return app.BadRequest("invalid target tenant")
			}
			var target uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE NOWAIT`, *desired.TenantID).Scan(&target)
			if errors.Is(err, pgx.ErrNoRows) {
				return app.NotFound("target company not found")
			}
			if err != nil {
				return err
			}
		} else if len(desired.AllowedZoneIDs) > 0 {
			return app.BadRequest("global permission profiles cannot carry tenant-local domains")
		}
		seen := map[uuid.UUID]bool{}
		for _, zone := range desired.AllowedZoneIDs {
			if zone == uuid.Nil || seen[zone] {
				return app.BadRequest("domain zone identities must be nonzero and distinct")
			}
			seen[zone] = true
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM domain_zones WHERE id=$1 AND tenant_id=$2 FOR SHARE NOWAIT`, zone, *desired.TenantID).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return app.BadRequest("domain zone is unavailable in the target company")
			}
			if err != nil {
				return err
			}
		}
		var err error
		out, err = scanPermProfile(tx.QueryRow(ctx, `INSERT INTO permission_profiles(id,tenant_id,name,description,can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,allowed_zone_ids,can_create_domains,can_create_routes,can_create_api_keys,is_system) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,false) RETURNING id,tenant_id,name,description,can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,allowed_zone_ids,can_create_domains,can_create_routes,can_create_api_keys,is_system,created_at,updated_at,permission_revision::text`, desired.ID, desired.TenantID, desired.Name, desired.Description, desired.CanSend, desired.DailySendQuota, desired.DailyReceiveQuota, desired.MaxMailboxes, desired.MaxDomains, uuidSliceParam(desired.AllowedZoneIDs), desired.CanCreateDomains, desired.CanCreateRoutes, desired.CanCreateAPIKeys))
		if err != nil {
			return err
		}
		if out == nil {
			return app.Internal(fmt.Errorf("permission profile insertion returned no persisted row"))
		}
		eventActor := current
		if out.TenantID != nil {
			eventActor.TenantID = *out.TenantID
		}
		// Global creation has no referenced companies yet: required audit, but no
		// invented anchor-only/all-tenant/platform invalidation audience.
		return permissionProfileAudit(ctx, tx, eventActor, out, []company.PermissionRevision{}, "permission.profile.create", map[string]any{"after_revision": out.Revision, "changed_fields": []string{"profile_presence"}})
	})
	if err != nil {
		if errors.Is(err, store.ErrMemberNotFound) {
			return nil, app.NotFound("persistent company audit anchor not found")
		}
		return nil, err
	}
	return out, nil
}
