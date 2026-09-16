package outbound

import (
	"context"
	"strings"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// ValidateJobAuthorization re-reads current identities and exact mailbox
// permissions at submit, manual retry and every delivery attempt. Immutable
// sender IDs prevent deletion of an owned key/user from widening old jobs.
func (s *Service) ValidateJobAuthorization(ctx context.Context, j *models.OutboundJob) error {
	if s == nil || s.store == nil || j == nil {
		return authz.ErrForbidden("sender validation unavailable")
	}
	if j.InFlightDomain != "" {
		return store.ErrOutboundUncertain
	}
	a := authz.Actor{TenantID: j.TenantID}
	if j.SenderKeyID != nil {
		key, err := s.store.GetAPIKey(ctx, *j.SenderKeyID)
		if err != nil {
			return err
		}
		if key == nil || key.TenantID != j.TenantID || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
			return authz.ErrForbidden("sending key revoked or expired")
		}
		canSend := false
		for _, scope := range key.Scopes {
			if strings.EqualFold(strings.TrimSpace(scope), "send:write") {
				canSend = true
			}
		}
		if !canSend || !models.ZoneAllowed(key.AllowedZoneIDs, j.ZoneID) {
			return authz.ErrForbidden("sending key scope revoked")
		}
		if (key.OwnerUserID == nil) != (j.SenderUserID == nil) || (key.OwnerUserID != nil && *key.OwnerUserID != *j.SenderUserID) {
			return authz.ErrForbidden("sending key ownership changed")
		}
		a.Type = authz.PrincipalAPIKey
		a.ID = key.ID
		a.OwnerUserID = key.OwnerUserID
		a.TenantWide = key.OwnerUserID == nil
	} else if j.SenderUserID != nil {
		a.Type = authz.PrincipalUser
		a.ID = *j.SenderUserID
	} else {
		return authz.ErrForbidden("submission has no durable sender identity")
	}
	if j.SenderUserID != nil {
		u, err := s.store.GetUser(ctx, *j.SenderUserID)
		if err != nil {
			return err
		}
		if u == nil || !u.IsActive {
			return authz.ErrForbidden("sender is inactive or deleted")
		}
		a.IsSuperAdmin = a.Type == authz.PrincipalUser && u.Role == models.RoleSuperAdmin
		if u.TenantID != j.TenantID && !a.IsSuperAdmin {
			return authz.ErrForbidden("sender company changed")
		}
		if !a.IsSuperAdmin && (u.Role != models.RoleAdmin || a.Type == authz.PrincipalAPIKey) {
			perm, err := s.store.EffectivePermission(ctx, u.ID)
			if err != nil {
				return err
			}
			if perm == nil || !perm.CanSend || !perm.AllowsZone(j.ZoneID) {
				return authz.ErrForbidden("sending capability revoked")
			}
			a.Permission = perm
		}
	}
	zone, err := s.store.GetZone(ctx, j.ZoneID)
	if err != nil {
		return err
	}
	if zone == nil || zone.TenantID != j.TenantID || !zone.IsVerified || !zone.MXVerified {
		return authz.ErrForbidden("sender zone no longer verified")
	}
	address, err := authz.CanonicalSender(j.MailFrom)
	if err != nil || address != j.MailFrom || extractDomain(address) != zone.Domain {
		return authz.ErrForbidden("sender address changed or invalid")
	}
	mb, err := s.store.ForTenant(j.TenantID).GetMailboxByAddress(ctx, address)
	if err != nil {
		return err
	}
	if j.SenderMailboxID != nil && (mb == nil || mb.ID != *j.SenderMailboxID) {
		return authz.ErrForbidden("sender mailbox deleted or replaced")
	}
	if mb != nil && mb.ExpiresAt != nil && !mb.ExpiresAt.After(time.Now()) {
		return authz.ErrForbidden("sender mailbox expired")
	}
	if a.TenantWide && mb != nil && (mb.OwnerUserID != nil || mb.Kind == "shared") {
		return authz.ErrForbidden("company mailbox requires employee sender")
	}
	if !a.TenantWide {
		if err := authz.CheckMailboxSender(ctx, s.store, a, mb, j.TemplateVersionID != nil); err != nil {
			return err
		}
	}
	if mb == nil {
		identity, err := s.store.FindSendIdentityForAddress(ctx, j.TenantID, address)
		if err != nil {
			return err
		}
		if identity == nil || !identity.Verified {
			return authz.ErrForbidden("sender identity revoked")
		}
	}
	if j.TemplateVersionID != nil {
		if j.SenderMailboxID == nil {
			return authz.ErrForbidden("template sender mailbox missing")
		}
		repo, ok := s.store.(company.Repository)
		if !ok {
			return authz.ErrForbidden("published template governance unavailable")
		}
		if _, _, _, err := repo.TemplateForSend(ctx, j.TenantID, j.SenderUserID, j.SenderKeyID, *j.SenderMailboxID, *j.TemplateVersionID); err != nil {
			return err
		}
	}
	if j.ContentDigest != "" && j.ContentDigest != contentDigest(j) {
		return authz.ErrForbidden("queued content integrity mismatch")
	}
	return nil
}
