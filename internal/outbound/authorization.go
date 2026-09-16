package outbound

import (
	"context"
	"strings"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// TemplateGovernance is the published-template dependency the service needs.
// It is deliberately narrower than company.Repository so test assemblies can
// supply a focused stub. Injected at construction; a missing dependency fails
// at startup instead of degrading at request time.
type TemplateGovernance interface {
	TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error)
}

// SendAddressStore is the read surface ResolveSendAuthorization needs. Both
// the production PgStore and the test FakeStore satisfy it with their existing
// methods — no second query path is introduced.
type SendAddressStore interface {
	authz.MailboxGrantReader
	ForTenant(uuid.UUID) store.TenantScoped
	FindSendIdentityForAddress(context.Context, uuid.UUID, string) (*models.SendIdentity, error)
}

// SendAuthorization is the resolved From-address verdict for one actor. Every
// independent violation is reported together; each caller maps the fields to
// its own error precedence and wording (HTTP and worker have historically
// differed in both, and this refactor preserves that byte-for-byte).
type SendAuthorization struct {
	Mailbox  *models.Mailbox
	Identity *models.SendIdentity // set only when the address resolved via a send identity

	MailboxExpired     bool  // mailbox exists but its ExpiresAt has passed
	TenantWideBlocked  bool  // tenant-wide credential targeted an employee-owned/shared mailbox
	MailboxSenderErr   error // CheckMailboxSender verdict (authz or store error); nil when passed or not applicable
	IdentityUnverified bool  // no mailbox and no verified send identity for the address
}

// ResolveSendAuthorization is the single send-authorization decision tree:
// given an actor (user or API key) and a canonical From address, resolve the
// mailbox path, the verified SendIdentity fallback, the tenant-wide company
// mailbox ban and the exact-mailbox send right in one place. The HTTP send
// handler and ValidateJobAuthorization (submit, manual retry, every delivery
// attempt) both route through this function so the two paths cannot drift.
func ResolveSendAuthorization(ctx context.Context, st SendAddressStore, actor authz.Actor, tenantID uuid.UUID, address string, hasPublishedTemplate bool) (*SendAuthorization, error) {
	mb, err := st.ForTenant(tenantID).GetMailboxByAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	res := &SendAuthorization{Mailbox: mb}
	if mb != nil && mb.ExpiresAt != nil && !mb.ExpiresAt.After(time.Now()) {
		res.MailboxExpired = true
	}
	if actor.TenantWide && mb != nil && (mb.OwnerUserID != nil || mb.Kind == "shared") {
		res.TenantWideBlocked = true
	}
	if !actor.TenantWide {
		if err := authz.CheckMailboxSender(ctx, st, actor, mb, hasPublishedTemplate); err != nil {
			res.MailboxSenderErr = err
		}
	}
	if mb == nil {
		identity, err := st.FindSendIdentityForAddress(ctx, tenantID, address)
		if err != nil {
			return nil, err
		}
		res.Identity = identity
		res.IdentityUnverified = identity == nil || !identity.Verified
	}
	return res, nil
}

// WorkerFailure ranks violations in the precedence ValidateJobAuthorization
// has always reported (expiry first, then the tenant-wide ban, the exact
// mailbox send right, and finally the identity fallback).
func (r *SendAuthorization) WorkerFailure() error {
	switch {
	case r.MailboxExpired:
		return authz.ErrForbidden("sender mailbox expired")
	case r.TenantWideBlocked:
		return authz.ErrForbidden("company mailbox requires employee sender")
	case r.MailboxSenderErr != nil:
		return r.MailboxSenderErr
	case r.IdentityUnverified:
		return authz.ErrForbidden("sender identity revoked")
	}
	return nil
}

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
	res, err := ResolveSendAuthorization(ctx, s.store, a, j.TenantID, address, j.TemplateVersionID != nil)
	if err != nil {
		return err
	}
	if j.SenderMailboxID != nil && (res.Mailbox == nil || res.Mailbox.ID != *j.SenderMailboxID) {
		return authz.ErrForbidden("sender mailbox deleted or replaced")
	}
	if err := res.WorkerFailure(); err != nil {
		return err
	}
	if j.TemplateVersionID != nil {
		if j.SenderMailboxID == nil {
			return authz.ErrForbidden("template sender mailbox missing")
		}
		if s.governance == nil {
			return authz.ErrForbidden("published template governance unavailable")
		}
		if _, _, _, err := s.governance.TemplateForSend(ctx, j.TenantID, j.SenderUserID, j.SenderKeyID, *j.SenderMailboxID, *j.TemplateVersionID); err != nil {
			return err
		}
	}
	if j.ContentDigest != "" && j.ContentDigest != contentDigest(j) {
		return authz.ErrForbidden("queued content integrity mismatch")
	}
	return nil
}
