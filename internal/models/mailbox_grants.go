package models

import (
	"github.com/google/uuid"
	"time"
)

// MailboxGrant is an exact mailbox relationship, never a domain-wide grant.
// Ported from archive/ingress-operations-console's enterprise.Grant; membership
// and published-template governance intentionally remain a P1 concern.
type MailboxGrant struct {
	TenantID     uuid.UUID  `json:"tenant_id"`
	MailboxID    uuid.UUID  `json:"mailbox_id"`
	UserID       uuid.UUID  `json:"user_id"`
	CanRead      bool       `json:"can_read"`
	CanOrganize  bool       `json:"can_organize"`
	CanSend      bool       `json:"can_send"`
	TemplateOnly bool       `json:"template_only"`
	GrantedBy    *uuid.UUID `json:"granted_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// MessageExpiry uses nil, not year 1 or now(), to express permanent retention.
// Owned mailboxes ignore legacy hourly plans, including existing message TTLs.
func MessageExpiry(mb *Mailbox, hours int, now time.Time) *time.Time {
	if hours == 0 || (mb != nil && mb.OwnerUserID != nil) {
		return nil
	}
	t := now.Add(time.Duration(hours) * time.Hour)
	return &t
}
