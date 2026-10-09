package outbound

import (
	"context"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"time"
)

// ValidateRetryRequester is independent of durable sender validation: being
// allowed to read a receipt never grants permission to cause another attempt.
// The transaction adapter additionally checks deadlines against database time.
func ValidateRetryRequester(ctx context.Context, st JobAuthorizationReader, a authz.Actor, j *models.OutboundJob) error {
	if j == nil || a.TenantID != j.TenantID || !a.Permission.AllowsZone(j.ZoneID) {
		return authz.ErrForbidden("retry sender scope unavailable")
	}
	switch a.Type {
	case authz.PrincipalUser:
		u, e := st.GetUser(ctx, a.ID)
		if e != nil {
			return e
		}
		var ok bool
		a, ok = authz.RefreshMemberActor(a, a.TenantID, u)
		if !ok {
			return authz.ErrForbidden("retry actor inactive")
		}
		if !a.IsTenantAdmin() {
			a.Permission, e = st.EffectivePermission(ctx, a.ID)
			if e != nil {
				return e
			}
		}
	case authz.PrincipalAPIKey:
		k, e := st.GetAPIKey(ctx, a.ID)
		if e != nil {
			return e
		}
		if !authz.OutboundKeyIdentityMatches(a, k) || !authz.OutboundKeyHasScope(k, "send:write") || !models.ZoneAllowed(k.AllowedZoneIDs, j.ZoneID) || k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
			return authz.ErrForbidden("retry key revoked or expired")
		}
		a = authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: k.TenantID, OwnerUserID: k.OwnerUserID, TenantWide: k.OwnerUserID == nil}
		if k.OwnerUserID != nil {
			u, e := st.GetUser(ctx, *k.OwnerUserID)
			if e != nil {
				return e
			}
			if u == nil || !u.IsActive || u.TenantID != a.TenantID {
				return authz.ErrForbidden("retry key owner inactive")
			}
			a.Permission, e = st.EffectivePermission(ctx, u.ID)
			if e != nil {
				return e
			}
		}
	default:
		return authz.ErrForbidden("current retry principal required")
	}
	if !a.Permission.AllowsZone(j.ZoneID) || !a.IsTenantAdmin() && a.EffectiveUserID() != nil && (a.Permission == nil || !a.Permission.CanSend) {
		return authz.ErrForbidden("retry sending capability revoked")
	}
	if j.SenderMailboxID != nil {
		if a.Type == authz.PrincipalAPIKey && a.TenantWide {
			// This is the same current ownerless integration key, not a grant
			// to operate another key's submission or an erased owned sender.
			if j.SenderKeyID == nil || *j.SenderKeyID != a.ID || j.SenderUserID != nil {
				return authz.ErrForbidden("retry integration sender identity changed")
			}
			address, e := authz.CanonicalSender(j.MailFrom)
			if e != nil || address != j.MailFrom {
				return authz.ErrForbidden("retry integration sender address invalid")
			}
			res, e := ResolveSendAuthorization(ctx, st, a, j.TenantID, address, j.TemplateVersionID != nil)
			if e != nil {
				return e
			}
			mb := res.Mailbox
			if mb == nil || mb.ID != *j.SenderMailboxID || mb.TenantID != j.TenantID || mb.ZoneID != j.ZoneID || mb.FullAddress != address || mb.Kind != "legacy" {
				return authz.ErrForbidden("retry integration sender mailbox changed")
			}
			if e := res.WorkerFailure(); e != nil {
				return e
			}
			// Ownerless template governance is not supported by the current
			// TemplateForSend port; a version ID never self-grants that authority.
			if authz.MailboxSendPolicy(mb) != authz.SendPolicyFree || j.TemplateVersionID != nil {
				return authz.ErrForbidden("retry integration template authority unavailable")
			}
			zone, e := st.GetZone(ctx, j.ZoneID)
			if e != nil {
				return e
			}
			if zone == nil || zone.TenantID != j.TenantID || zone.ID != mb.ZoneID || zone.Domain != extractDomain(address) || !zone.IsVerified || !zone.MXVerified {
				return authz.ErrForbidden("retry integration sender zone unavailable")
			}
			return nil
		}
		mb, e := st.ForTenant(j.TenantID).GetMailbox(ctx, *j.SenderMailboxID)
		if e != nil {
			return e
		}
		if mb == nil || mb.ZoneID != j.ZoneID {
			return authz.ErrForbidden("retry sender mailbox unavailable")
		}
		return authz.CheckMailboxSender(ctx, st, a, mb, j.TemplateVersionID != nil)
	}
	if uid := a.EffectiveUserID(); uid != nil && j.SenderUserID != nil && *uid == *j.SenderUserID {
		return nil
	}
	if a.Type == authz.PrincipalAPIKey && j.SenderKeyID != nil && a.ID == *j.SenderKeyID {
		return nil
	}
	return authz.ErrForbidden("send identity authority required to retry")
}
