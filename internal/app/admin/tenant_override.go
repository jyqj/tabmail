package adminapp

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"tabmail/internal/app"
)

// TenantOverrideSnapshot is the editable raw configuration, never the plan's
// effective values. Every nullable field is present, including for no row.
type TenantOverrideSnapshot struct {
	TenantID              uuid.UUID `json:"tenant_id"`
	MaxDomains            *int      `json:"max_domains"`
	MaxMailboxesPerDomain *int      `json:"max_mailboxes_per_domain"`
	MaxMessagesPerMailbox *int      `json:"max_messages_per_mailbox"`
	MaxMessageBytes       *int      `json:"max_message_bytes"`
	RetentionHours        *int      `json:"retention_hours"`
	RPMLimit              *int      `json:"rpm_limit"`
	DailyQuota            *int      `json:"daily_quota"`
}

func (s *Service) GetTenantOverride(ctx context.Context, tenantID uuid.UUID) (*TenantOverrideSnapshot, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if tenant == nil {
		return nil, app.NotFound("tenant not found")
	}
	if tenant.ID != tenantID {
		return nil, app.Internal(errors.New("tenant lookup identity mismatch"))
	}
	stored, err := s.store.GetOverride(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	out := &TenantOverrideSnapshot{TenantID: tenantID}
	if stored == nil {
		return out, nil
	}
	if stored.TenantID != tenantID {
		return nil, app.Internal(errors.New("tenant override identity mismatch"))
	}
	out.MaxDomains = copyOverrideInteger(stored.MaxDomains)
	out.MaxMailboxesPerDomain = copyOverrideInteger(stored.MaxMailboxesPerDomain)
	out.MaxMessagesPerMailbox = copyOverrideInteger(stored.MaxMessagesPerMailbox)
	out.MaxMessageBytes = copyOverrideInteger(stored.MaxMessageBytes)
	out.RetentionHours = copyOverrideInteger(stored.RetentionHours)
	out.RPMLimit = copyOverrideInteger(stored.RPMLimit)
	out.DailyQuota = copyOverrideInteger(stored.DailyQuota)
	return out, nil
}

func copyOverrideInteger(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
