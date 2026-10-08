package adminapp

import (
	"math"

	"tabmail/internal/app"
	"tabmail/internal/models"
)

// Plans and nullable overrides persist the same seven PostgreSQL INT fields.
// This checks representability only; existing quota and retention semantics
// still decide what zero, negative values and inherited values mean.
func validateConfigIntegers(v models.TenantOverride) error {
	for _, field := range []struct {
		name  string
		value *int
	}{
		{"max_domains", v.MaxDomains},
		{"max_mailboxes_per_domain", v.MaxMailboxesPerDomain},
		{"max_messages_per_mailbox", v.MaxMessagesPerMailbox},
		{"max_message_bytes", v.MaxMessageBytes},
		{"retention_hours", v.RetentionHours},
		{"rpm_limit", v.RPMLimit},
		{"daily_quota", v.DailyQuota},
	} {
		if field.value != nil && (*field.value < math.MinInt32 || *field.value > math.MaxInt32) {
			return app.BadRequest(field.name + " must be a signed 32-bit integer")
		}
	}
	return nil
}

func validatePlanIntegers(p *models.Plan) error {
	return validateConfigIntegers(models.TenantOverride{
		MaxDomains: &p.MaxDomains, MaxMailboxesPerDomain: &p.MaxMailboxesPerDomain,
		MaxMessagesPerMailbox: &p.MaxMessagesPerMailbox, MaxMessageBytes: &p.MaxMessageBytes,
		RetentionHours: &p.RetentionHours, RPMLimit: &p.RPMLimit, DailyQuota: &p.DailyQuota,
	})
}
