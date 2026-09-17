package authz

import (
	"context"
	"github.com/google/uuid"
	"net/mail"
	"strings"
	"tabmail/internal/models"
	"time"
)

type MailboxGrantReader interface {
	GetMailboxGrant(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.MailboxGrant, error)
}

// MailSendPolicy is the outbound send policy attached to a company (tenant
// default) or to one mailbox (override). The effective value reaches the
// decisions below on the Mailbox row itself — the store resolves
// COALESCE(mailboxes.send_policy, tenants.mail_send_policy) in its canonical
// mailbox select, so there is exactly one source and no extra query.
type MailSendPolicy string

const (
	SendPolicyFree             MailSendPolicy = "free"
	SendPolicyTemplateRequired MailSendPolicy = "template_required"
	SendPolicyDisabled         MailSendPolicy = "disabled"
)

// MailboxSendPolicy resolves the effective policy carried on a Mailbox. A nil
// or empty value (struct built outside the store) reads as 'free'; real rows
// always carry the COALESCE'd value because tenants.mail_send_policy is NOT
// NULL DEFAULT 'free' and both columns are CHECK-constrained to the three
// known values.
func MailboxSendPolicy(mb *models.Mailbox) MailSendPolicy {
	if mb == nil || mb.SendPolicy == nil || *mb.SendPolicy == "" {
		return SendPolicyFree
	}
	return MailSendPolicy(*mb.SendPolicy)
}

// MailboxRights follows the archived CheckMailbox/CheckSender model: bind the
// actor to one canonical mailbox, then check independent actions. An admin
// role does not itself confer normal content access. Tenant-wide integration
// keys retain their explicitly documented legacy boundary outside this helper.
func MailboxRights(ctx context.Context, st MailboxGrantReader, tenant uuid.UUID, user *uuid.UUID, mb *models.Mailbox) (*models.MailboxGrant, error) {
	if mb == nil || user == nil || tenant == uuid.Nil || mb.TenantID != tenant {
		return nil, nil
	}
	if mb.ExpiresAt != nil && !mb.ExpiresAt.After(time.Now()) {
		return nil, nil
	}
	if mb.OwnerUserID != nil && *mb.OwnerUserID == *user {
		return &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: *user, CanRead: true, CanOrganize: true, CanSend: true}, nil
	}
	g, err := st.GetMailboxGrant(ctx, tenant, mb.ID, *user)
	if err != nil {
		return nil, err
	}
	if g != nil && (g.TenantID != tenant || g.MailboxID != mb.ID || g.UserID != *user) {
		return nil, nil
	}
	return g, nil
}

// MailboxDecision is the unified verdict for one actor on one work mailbox.
// Content rights (read/organize/send/template_only) derive only from mailbox
// ownership or an explicit grant; CanManage is management visibility — the
// right to see the mailbox's metadata in administrative listings and detail
// views — and never implies content access. Company-mail paths surface both
// halves through this single function so the store and handlers cannot drift.
type MailboxDecision struct {
	CanRead      bool
	CanOrganize  bool
	CanSend      bool
	TemplateOnly bool
	CanManage    bool
}

// EvaluateMailboxAccess is the single decision point for company-mailbox
// access. It consolidates the rules previously re-derived in the store's
// mailboxAccessTx, GetWorkMailbox and ListWorkMailboxes:
//
//   - an expired mailbox yields no content rights;
//   - a permission profile restricted to other zones yields no content rights
//     (a non-nil profile is authoritative, for admins too);
//   - the effective owner holds read/organize/send intrinsically;
//   - otherwise a matching grant carries the flags verbatim (a grant that
//     does not match the actor/mailbox/tenant is ignored);
//   - the mailbox's effective send policy binds every actor, owner included:
//     'disabled' removes CanSend outright, 'template_required' turns any send
//     right — intrinsic or granted — into a template-only one;
//   - a non-admin whose profile lacks can_send (or has no profile) cannot
//     send — admins keep granted send rights;
//   - tenant admins always retain CanManage visibility.
//
// The actor carries its profile via Actor.Permission, mirroring how companyTx
// reloads effective permissions inside the transaction.
func EvaluateMailboxAccess(actor Actor, mb *models.Mailbox, grant *models.MailboxGrant) MailboxDecision {
	d := MailboxDecision{CanManage: actor.IsTenantAdmin()}
	if mb == nil || mb.TenantID != actor.TenantID {
		return MailboxDecision{CanManage: d.CanManage}
	}
	if mb.ExpiresAt != nil && !mb.ExpiresAt.After(time.Now()) {
		return d
	}
	if actor.Permission != nil && !models.ZoneAllowed(actor.Permission.AllowedZoneIDs, mb.ZoneID) {
		return d
	}
	policy := MailboxSendPolicy(mb)
	uid := actor.EffectiveUserID()
	if uid != nil && mb.OwnerUserID != nil && *mb.OwnerUserID == *uid {
		d.CanRead, d.CanOrganize = true, true
		d.CanSend = policy != SendPolicyDisabled
		d.TemplateOnly = policy == SendPolicyTemplateRequired
	} else if grant != nil && grant.TenantID == mb.TenantID && grant.MailboxID == mb.ID && uid != nil && grant.UserID == *uid {
		d.CanRead, d.CanOrganize, d.CanSend, d.TemplateOnly = grant.CanRead, grant.CanOrganize, grant.CanSend, grant.TemplateOnly
		d.TemplateOnly = d.TemplateOnly || policy == SendPolicyTemplateRequired
		if policy == SendPolicyDisabled {
			d.CanSend = false
		}
	}
	if !actor.IsTenantAdmin() && (actor.Permission == nil || !actor.Permission.CanSend) {
		d.CanSend = false
	}
	return d
}

// CanonicalSender ports the archive's plain-address requirement. Authorization
// and the SMTP envelope must use the same representation, not display names.
func CanonicalSender(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || strings.ContainsAny(s, "\r\n") || len(s) > 254 {
		return "", ErrForbidden("use a plain email address")
	}
	return strings.ToLower(a.Address), nil
}

// CheckMailboxSender is the send verdict for one actor on one mailbox address.
// No role bypasses it any more: the exact send right is demanded of owners,
// tenant admins and global admins alike, and the mailbox's effective send
// policy (carried on Mailbox.SendPolicy by the store) binds them all —
// 'disabled' refuses the send outright, 'template_required' admits only the
// immutable published-version path. Its usage grant/provenance is checked
// before enqueue and every attempt.
func CheckMailboxSender(ctx context.Context, st MailboxGrantReader, actor Actor, mb *models.Mailbox, hasPublishedTemplate bool) error {
	g, err := MailboxRights(ctx, st, actor.TenantID, actor.EffectiveUserID(), mb)
	if err != nil {
		return err
	}
	if g == nil || !g.CanSend {
		// No mailbox exists at this address: there is no mailbox for a policy
		// to gate, so a global admin keeps the verified-identity fallback it
		// always used. A real mailbox demands the exact send right of every
		// actor — management visibility never implies sending.
		if mb == nil && actor.IsGlobalAdmin() {
			return nil
		}
		return ErrForbidden("exact mailbox send_as permission required")
	}
	switch MailboxSendPolicy(mb) {
	case SendPolicyDisabled:
		return ErrForbidden("mailbox sending disabled by company policy")
	case SendPolicyTemplateRequired:
		if !hasPublishedTemplate {
			return ErrForbidden("a granted published template version is required")
		}
	}
	// Only the immutable published-version path may satisfy template-only.
	if g.TemplateOnly && !hasPublishedTemplate {
		return ErrForbidden("a granted published template version is required")
	}

	return nil
}
