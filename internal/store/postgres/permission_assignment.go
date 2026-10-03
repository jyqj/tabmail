package postgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"reflect"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// AssignPermissionEditor binds both the old editor observation and the selected
// new profile observation. Assignment and optional override intent commit with
// one required audit/outbox event; no two-request partial-success window is introduced.
func (s *PgStore) AssignPermissionEditor(ctx context.Context, actor authz.Actor, userID uuid.UUID, cmd company.PermissionAssignmentCommand) (*company.PermissionEditorSnapshot, error) {
	if err := cmd.ExpectedRevision.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	if err := cmd.Patch.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	if (cmd.ProfileID == nil) != (cmd.ProfileRevision == nil) {
		return nil, app.BadRequest("selected profile identity and revision must be supplied together")
	}
	if cmd.ProfileID != nil {
		if *cmd.ProfileID == uuid.Nil {
			return nil, app.BadRequest("invalid selected profile")
		}
		if err := permissionPositiveRevision(*cmd.ProfileRevision); err != nil {
			return nil, err
		}
	}
	var out *company.PermissionEditorSnapshot
	err := s.companyTx(ctx, actor, true, func(tx pgx.Tx, current authz.Actor) error {
		before, err := permissionEditorSnapshotTx(ctx, tx, current, userID, true)
		if err != nil {
			return err
		}
		if !cmd.ExpectedRevision.Equal(before.Revision) {
			return app.Conflict("member permissions changed; reload before retrying")
		}
		if cmd.ProfileID != nil {
			selected, err := scanPermProfile(tx.QueryRow(ctx, permProfileSelect+` WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2) FOR SHARE NOWAIT`, *cmd.ProfileID, before.TenantID))
			if err != nil {
				return err
			}
			if selected == nil {
				return app.NotFound("selected permission profile unavailable in this company")
			}
			if selected.Revision != *cmd.ProfileRevision {
				return app.Conflict("selected permission profile changed; reload before retrying")
			}
		}
		same := (before.Revision.ProfileID == nil && cmd.ProfileID == nil) || (before.Revision.ProfileID != nil && cmd.ProfileID != nil && *before.Revision.ProfileID == *cmd.ProfileID)
		if same && cmd.Patch.Empty() {
			out = before
			return nil
		}
		if !same {
			tag, err := tx.Exec(ctx, `UPDATE users SET permission_profile_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND tenant_id=$2 AND permission_revision=$4::bigint`, userID, before.TenantID, cmd.ProfileID, before.Revision.UserRevision)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return app.Conflict("member permissions changed; reload before retrying")
			}
		}
		if err = applyPermissionPatchTx(ctx, tx, userID, before.TenantID, cmd.Patch); err != nil {
			return err
		}
		out, err = permissionEditorSnapshotTx(ctx, tx, current, userID, true)
		if err != nil {
			return err
		}
		return companyAudit(ctx, tx, current, "permission.profile.assign", "user", before.UserID, map[string]any{"before_revision": before.Revision, "after_revision": out.Revision, "changed_fields": permissionAssignmentChangedFields(before, out)})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func permissionAssignmentChangedFields(before, after *company.PermissionEditorSnapshot) []string {
	out := []string{}
	if !reflect.DeepEqual(before.Revision.ProfileID, after.Revision.ProfileID) {
		out = append(out, "permission_profile_id")
	}
	b, a := before.Overrides, after.Overrides
	if (b == nil) != (a == nil) {
		out = append(out, "override_presence")
	}
	// Presence is separate from nullable field intent; an inserted row does not
	// turn all its unchanged inherited fields into claimed modifications.
	if b == nil {
		b = &company.RawPermissionOverrides{DomainAccess: company.DomainAccess{Mode: "inherit", ZoneIDs: []uuid.UUID{}}}
	}
	if a == nil {
		a = &company.RawPermissionOverrides{DomainAccess: company.DomainAccess{Mode: "inherit", ZoneIDs: []uuid.UUID{}}}
	}
	fields := []struct {
		name          string
		before, after any
	}{
		{"can_send", b.CanSend, a.CanSend}, {"daily_send_quota", b.DailySendQuota, a.DailySendQuota},
		{"daily_receive_quota", b.DailyReceiveQuota, a.DailyReceiveQuota}, {"max_mailboxes", b.MaxMailboxes, a.MaxMailboxes},
		{"max_domains", b.MaxDomains, a.MaxDomains}, {"can_create_domains", b.CanCreateDomains, a.CanCreateDomains},
		{"can_create_routes", b.CanCreateRoutes, a.CanCreateRoutes}, {"can_create_api_keys", b.CanCreateAPIKeys, a.CanCreateAPIKeys},
	}
	for _, f := range fields {
		if !reflect.DeepEqual(f.before, f.after) {
			out = append(out, f.name)
		}
	}
	if !reflect.DeepEqual(b.DomainAccess, a.DomainAccess) || !reflect.DeepEqual(b.AllowedZoneIDs, a.AllowedZoneIDs) {
		out = append(out, "domain_access")
	}
	return out
}
