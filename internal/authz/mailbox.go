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
