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
		mb, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		u, e := scanUser(tx.QueryRow(ctx, userSelect+` WHERE tenant_id=$1 AND id=$2`, a.TenantID, user))
		if e != nil {
			return e
		}
		if u == nil {
			return app.NotFound("employee not found")
		}
		v.Active = u.IsActive
		v.SendPolicy = string(authz.MailboxSendPolicy(&mb.Mailbox))
		if mb.Mailbox.OwnerUserID != nil && *mb.Mailbox.OwnerUserID == user {
			v.Source = "owner"
		} else {
			var grant bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3)`, a.TenantID, mailbox, user).Scan(&grant); e != nil {
				return e
			}
			if grant {
				v.Source = "grant"
			}
		}
		v.Reasons = append(v.Reasons, v.Source)
		if !u.IsActive {
			v.Reasons = append(v.Reasons, "inactive")
			return nil
		}
		target := authz.Actor{Type: authz.PrincipalUser, ID: user, TenantID: a.TenantID, Role: u.Role, IsAdmin: u.Role == models.RoleAdmin, IsSuperAdmin: u.Role == models.RoleSuperAdmin}
		if !target.IsTenantAdmin() {
			target.Permission, e = effectivePermission(ctx, tx, user)
			if e != nil {
				return e
			}
		}
		// The SAME decision function and effective policy used for employee reads
		// and sends. The explanation does not implement a second permissions engine.
		rights, e := s.mailboxAccessTx(ctx, tx, target, mailbox)
		if e != nil {
			return e
		}
		v.CanRead, v.CanOrganize, v.CanSend, v.TemplateOnly = rights.CanRead, rights.CanOrganize, rights.CanSend, rights.TemplateOnly
		if mb.Mailbox.ExpiresAt != nil && !mb.Mailbox.ExpiresAt.After(time.Now()) {
			v.Reasons = append(v.Reasons, "expired")
		}
		if target.Permission != nil && !models.ZoneAllowed(target.Permission.AllowedZoneIDs, mb.Mailbox.ZoneID) {
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
	return v, e
}
