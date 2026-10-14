package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
)

func permissionPositiveRevision(s string) error {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return app.BadRequest("positive decimal permission revision required")
	}
	return nil
}

// Profile-first mutation locks are NOWAIT: member operations use the opposite
// stable-user-first order and must never form a profile/user wait cycle.
func permissionProfileMutationTx(ctx context.Context, tx pgx.Tx, a authz.Actor, id uuid.UUID) (*models.PermissionProfile, error) {
	lock := " FOR UPDATE NOWAIT"
	p, err := scanPermProfile(tx.QueryRow(ctx, permProfileSelect+` WHERE id=$1 AND (tenant_id=$2 OR ($3 AND tenant_id IS NULL))`+lock, id, a.TenantID, a.IsSuperAdmin))
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, app.NotFound("permission profile not found")
	}
	if p.IsSystem {
		return nil, app.Forbidden("system permission profiles are immutable")
	}
	return p, nil
}

// Freeze the exact affected-member set. The parent profile lock blocks new
// assignments; stable user locks fence raw overrides, role and existing FK
// removal. Cross-company global effects require a freshly reloaded super admin.
func permissionProfileMembersTx(ctx context.Context, tx pgx.Tx, a authz.Actor, p *models.PermissionProfile) ([]company.PermissionRevision, error) {
	rows, err := tx.Query(ctx, `SELECT id,tenant_id,role,permission_revision::text FROM users WHERE permission_profile_id=$1 ORDER BY id FOR NO KEY UPDATE NOWAIT`, p.ID)
	if err != nil {
		return nil, err
	}
	type member struct {
		id, tenant uuid.UUID
		role       models.UserRole
		revision   string
	}
	members := []member{}
	for rows.Next() {
		var m member
		if err = rows.Scan(&m.id, &m.tenant, &m.role, &m.revision); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []company.PermissionRevision{}
	for _, m := range members {
		scope := a
		if p.TenantID == nil && a.IsSuperAdmin {
			scope.TenantID = m.tenant
		}
		if !authz.CanManageTenantMember(scope, m.tenant, m.role) {
			return nil, authz.ErrForbidden("profile affects a member whose role cannot be managed")
		}
		var tenant uuid.UUID
		if err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE NOWAIT`, m.tenant).Scan(&tenant); err != nil {
			return nil, err
		}
		profileID, revision := p.ID, p.Revision
		out = append(out, company.PermissionRevision{UserID: m.id, TenantID: m.tenant, UserRevision: m.revision, ProfileID: &profileID, ProfileRevision: &revision})
	}
	return out, nil
}

func (s *PgStore) UpdatePermissionProfileCAS(ctx context.Context, actor authz.Actor, desired *models.PermissionProfile, expectedRevision string) (*models.PermissionProfile, error) {
	if err := permissionPositiveRevision(expectedRevision); err != nil {
		return nil, err
	}
	if desired == nil || desired.ID == uuid.Nil || strings.TrimSpace(desired.Name) == "" || len([]rune(desired.Name)) > 64 {
		return nil, app.BadRequest("valid permission profile identity and name required")
	}
	for _, n := range []int{desired.DailySendQuota, desired.DailyReceiveQuota, desired.MaxMailboxes, desired.MaxDomains} {
		if n < 0 || int64(n) > 2147483647 {
			return nil, app.BadRequest("quota must be a nonnegative database integer")
		}
	}
	var out *models.PermissionProfile
	err := s.companyTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		before, err := permissionProfileMutationTx(ctx, tx, current, desired.ID)
		if err != nil {
			return err
		}
		if (before.TenantID == nil) != (desired.TenantID == nil) || (before.TenantID != nil && *before.TenantID != *desired.TenantID) {
			return app.NotFound("permission profile not found in requested scope")
		}
		if before.Revision != expectedRevision {
			return app.Conflict("permission profile changed; reload before retrying")
		}
		affected, err := permissionProfileMembersTx(ctx, tx, current, before)
		if err != nil {
			return err
		}
		if len(desired.AllowedZoneIDs) > 0 && before.TenantID == nil {
			return app.BadRequest("global permission profiles cannot carry tenant-local domains")
		}
		seen := map[uuid.UUID]bool{}
		for _, zone := range desired.AllowedZoneIDs {
			if zone == uuid.Nil || seen[zone] {
				return app.BadRequest("domain zone identities must be nonzero and distinct")
			}
			seen[zone] = true
			var id uuid.UUID
			err = tx.QueryRow(ctx, `SELECT id FROM domain_zones WHERE id=$1 AND tenant_id=$2 FOR KEY SHARE NOWAIT`, zone, *before.TenantID).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return app.BadRequest("domain zone is unavailable in this company")
			}
			if err != nil {
				return err
			}
		}
		changedFields := permissionProfileChangedFields(before, desired)
		if len(changedFields) == 0 {
			out = before
			return nil
		}
		out, err = scanPermProfile(tx.QueryRow(ctx, `UPDATE permission_profiles SET name=$2,description=$3,can_send=$4,daily_send_quota=$5,daily_receive_quota=$6,max_mailboxes=$7,max_domains=$8,allowed_zone_ids=$9,can_create_domains=$10,can_create_routes=$11,can_create_api_keys=$12,updated_at=clock_timestamp() WHERE id=$1 AND permission_revision=$13::bigint RETURNING id,tenant_id,name,description,can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,allowed_zone_ids,can_create_domains,can_create_routes,can_create_api_keys,is_system,created_at,updated_at,permission_revision::text`, before.ID, desired.Name, desired.Description, desired.CanSend, desired.DailySendQuota, desired.DailyReceiveQuota, desired.MaxMailboxes, desired.MaxDomains, uuidSliceParam(desired.AllowedZoneIDs), desired.CanCreateDomains, desired.CanCreateRoutes, desired.CanCreateAPIKeys, expectedRevision))
		if err != nil {
			return err
		}
		if out == nil {
			return app.Conflict("permission profile changed; reload before retrying")
		}
		return permissionProfileAudit(ctx, tx, current, before, affected, "permission.profile.update", map[string]any{"before_revision": before.Revision, "after_revision": out.Revision, "affected_members": affected, "changed_fields": changedFields})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) DeletePermissionProfileCAS(ctx context.Context, actor authz.Actor, id uuid.UUID, expectedRevision string, confirmedMembers []company.PermissionRevision) error {
	if err := permissionPositiveRevision(expectedRevision); err != nil {
		return err
	}
	confirmations := map[uuid.UUID]company.PermissionRevision{}
	for _, r := range confirmedMembers {
		if err := r.Validate(); err != nil {
			return app.BadRequest(err.Error())
		}
		if _, ok := confirmations[r.UserID]; ok {
			return app.BadRequest("duplicate member confirmation")
		}
		confirmations[r.UserID] = r
	}
	return s.companyTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		p, err := permissionProfileMutationTx(ctx, tx, current, id)
		if err != nil {
			return err
		}
		if p.Revision != expectedRevision {
			return app.Conflict("permission profile changed; reload deletion preview")
		}
		members, err := permissionProfileMembersTx(ctx, tx, current, p)
		if err != nil {
			return err
		}
		if len(members) != len(confirmations) {
			return app.Conflict("affected members changed; reload deletion preview")
		}
		for _, r := range members {
			if !r.Equal(confirmations[r.UserID]) {
				return app.Conflict("member permissions changed; reload deletion preview")
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM permission_profiles WHERE id=$1 AND permission_revision=$2::bigint`, id, expectedRevision)
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23503" {
				return app.Conflict("permission profile is still referenced by an administrative workflow")
			}
			return err
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("permission profile changed; reload deletion preview")
		}
		changedFields := []string{"profile_presence"}
		if len(members) > 0 {
			changedFields = append(changedFields, "permission_profile_id")
		}
		return permissionProfileAudit(ctx, tx, current, p, members, "permission.profile.delete", map[string]any{"profile_revision": p.Revision, "confirmed_members": members, "changed_fields": changedFields})
	})
}

// permissionProfileAudit preserves the one selected-company mutation audit.
// A tenant-local profile keeps companyAudit's original event. A global profile
// invalidates only companies in the already-fenced affected-member snapshot,
// once each, with companyAudit's existing public event envelope/allowlist. This
// is notification routing, not authority to manage another company's members.
// There is no platform/global audience protocol: an unreferenced global profile
// has a required audit but no company invalidation event.
func permissionProfileAudit(ctx context.Context, tx pgx.Tx, a authz.Actor, p *models.PermissionProfile, members []company.PermissionRevision, action string, details any) error {
	if p.TenantID != nil {
		return companyAudit(ctx, tx, a, action, "permission_profile", p.ID, details)
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,$3,$4,$5,$6)`, a.TenantID, a.AuditLabel(), action, "permission_profile", p.ID, b); err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	tenants := []uuid.UUID{}
	for _, member := range members {
		if !seen[member.TenantID] {
			seen[member.TenantID] = true
			tenants = append(tenants, member.TenantID)
		}
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].String() < tenants[j].String() })
	for _, tenant := range tenants {
		raw, err := json.Marshal(hooks.Event{Type: "company.admin.changed", TenantID: tenant.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"action": action, "resource_type": "permission_profile", "resource_id": p.ID.String()}})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload) VALUES($1,'company.admin.changed',$2)`, uuid.New(), raw); err != nil {
			return err
		}
	}
	return nil
}

// GetPermissionProfileDeletionPreview fences the displayed profile and member
// observations, and computes actual inherited values with the same canonical
// SQL merge as production reads. Deletion requires this entire revision set.
func (s *PgStore) GetPermissionProfileDeletionPreview(ctx context.Context, actor authz.Actor, id uuid.UUID) (*company.PermissionProfileDeletionPreview, error) {
	var out *company.PermissionProfileDeletionPreview
	err := s.companyReadTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		p, err := scanPermProfile(tx.QueryRow(ctx, permProfileSelect+` WHERE id=$1 AND (tenant_id=$2 OR ($3 AND tenant_id IS NULL)) FOR UPDATE NOWAIT`, id, current.TenantID, current.IsSuperAdmin))
		if err != nil {
			return err
		}
		if p == nil {
			return app.NotFound("permission profile not found")
		}
		if p.IsSystem {
			return app.Forbidden("system permission profiles are immutable")
		}
		members, err := permissionProfileMembersTx(ctx, tx, current, p)
		if err != nil {
			return err
		}
		out = &company.PermissionProfileDeletionPreview{ProfileID: p.ID, ProfileRevision: p.Revision, Members: members, Changes: []company.PermissionProfileDeletionChange{}}
		for _, r := range members {
			before, err := effectivePermission(ctx, tx, r.UserID)
			if err != nil {
				return err
			}
			after, err := effectivePermissionAfterProfileRemoval(ctx, tx, r.UserID)
			if err != nil {
				return err
			}
			out.Changes = append(out.Changes, company.PermissionProfileDeletionChange{Revision: r, Before: before, After: after})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Audit names only persisted profile fields whose raw values actually changed.
func permissionProfileChangedFields(before, after *models.PermissionProfile) []string {
	fields := []struct {
		name          string
		before, after any
	}{
		{"name", before.Name, after.Name}, {"description", before.Description, after.Description},
		{"can_send", before.CanSend, after.CanSend}, {"daily_send_quota", before.DailySendQuota, after.DailySendQuota},
		{"daily_receive_quota", before.DailyReceiveQuota, after.DailyReceiveQuota}, {"max_mailboxes", before.MaxMailboxes, after.MaxMailboxes},
		{"max_domains", before.MaxDomains, after.MaxDomains}, {"allowed_zone_ids", before.AllowedZoneIDs, after.AllowedZoneIDs},
		{"can_create_domains", before.CanCreateDomains, after.CanCreateDomains}, {"can_create_routes", before.CanCreateRoutes, after.CanCreateRoutes},
		{"can_create_api_keys", before.CanCreateAPIKeys, after.CanCreateAPIKeys},
	}
	out := []string{}
	for _, f := range fields {
		if !reflect.DeepEqual(f.before, f.after) {
			out = append(out, f.name)
		}
	}
	return out
}
