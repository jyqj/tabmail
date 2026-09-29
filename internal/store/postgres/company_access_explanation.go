package postgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"time"
)

func (s *PgStore) ExplainMailboxAccess(ctx context.Context, a authz.Actor, mailbox, user uuid.UUID) (*company.AccessExplanation, error) {
	v := &company.AccessExplanation{MailboxID: mailbox, UserID: user, Source: "none", Reasons: []string{}}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		// The current administrator and the target are different principals.
		// Protect the target before reading its role/profile/optional override,
		// using the same stable row fence as ordinary company transactions.
		u, e := scanUser(tx.QueryRow(ctx, userSelect+` WHERE tenant_id=$1 AND id=$2 FOR SHARE`, a.TenantID, user))
		if e != nil {
			return e
		}
		if u == nil {
			return app.NotFound("employee not found")
		}
		target := authz.Actor{Type: authz.PrincipalUser, ID: user, TenantID: a.TenantID, Role: u.Role, IsAdmin: u.Role == models.RoleAdmin, IsSuperAdmin: u.Role == models.RoleSuperAdmin}
		if u.IsActive && !target.IsTenantAdmin() {
			target.Permission, e = effectivePermissionSnapshot(ctx, tx, user)
			if e != nil {
				return e
			}
		}
		if e = lockMailboxAuthorization(ctx, tx, a.TenantID, mailbox); e != nil {
			return e
		}
		// One mailbox decision supplies both capabilities and owner/grant source.
		// Management authorization above never becomes target content authority.
		rights, source, e := s.mailboxAccessWithSourceTx(ctx, tx, target, mailbox)
		if e != nil {
			return e
		}
		v.Active, v.Source = u.IsActive, source
		v.SendPolicy = string(authz.MailboxSendPolicy(&rights.Mailbox))
		v.Reasons = append(v.Reasons, source)
		if !u.IsActive {
			v.Reasons = append(v.Reasons, "inactive")
			return nil
		}
		v.CanRead, v.CanOrganize, v.CanSend, v.TemplateOnly = rights.CanRead, rights.CanOrganize, rights.CanSend, rights.TemplateOnly
		if rights.Mailbox.ExpiresAt != nil && !rights.Mailbox.ExpiresAt.After(time.Now()) {
			v.Reasons = append(v.Reasons, "expired")
		}
		if target.Permission != nil && !models.ZoneAllowed(target.Permission.AllowedZoneIDs, rights.Mailbox.ZoneID) {
			v.Reasons = append(v.Reasons, "zone_restricted")
		}
		if !target.IsTenantAdmin() && (target.Permission == nil || !target.Permission.CanSend) {
			v.Reasons = append(v.Reasons, "profile_send_disabled")
		}
		if v.SendPolicy == "disabled" {
			v.Reasons = append(v.Reasons, "sending_disabled")
		}
		if v.TemplateOnly {
			v.Reasons = append(v.Reasons, "template_required")
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return v, nil
}
