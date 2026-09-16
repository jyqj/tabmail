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
	uid := actor.EffectiveUserID()
	if uid != nil && mb.OwnerUserID != nil && *mb.OwnerUserID == *uid {
		d.CanRead, d.CanOrganize, d.CanSend = true, true, true
	} else if grant != nil && grant.TenantID == mb.TenantID && grant.MailboxID == mb.ID && uid != nil && grant.UserID == *uid {
		d.CanRead, d.CanOrganize, d.CanSend, d.TemplateOnly = grant.CanRead, grant.CanOrganize, grant.CanSend, grant.TemplateOnly
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

func CheckMailboxSender(ctx context.Context, st MailboxGrantReader, actor Actor, mb *models.Mailbox, hasPublishedTemplate bool) error {
	if actor.IsGlobalAdmin() {
		return nil
	}
	g, err := MailboxRights(ctx, st, actor.TenantID, actor.EffectiveUserID(), mb)
	if err != nil {
		return err
	}
	if g == nil || !g.CanSend {
		return ErrForbidden("exact mailbox send_as permission required")
	}
	// Only the immutable published-version path may satisfy template-only.
	// Its usage grant/provenance is checked before enqueue and every attempt.
	if g.TemplateOnly && !hasPublishedTemplate {
		return ErrForbidden("a granted published template version is required")
	}

	return nil
}
