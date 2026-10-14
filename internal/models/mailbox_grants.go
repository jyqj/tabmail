package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
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

// ErrRetentionExpiry identifies a finite retention policy that cannot be stored
// and returned using the existing timestamp and JSON message representations.
var ErrRetentionExpiry = errors.New("retention_hours must produce a finite UTC expiry within years 0000 through 9999")

// MessageExpiry uses nil, not year 1 or now(), to express permanent retention.
// Owned mailboxes ignore legacy hourly plans, including existing message TTLs.
// Finite hours are elapsed hours, independent of daylight saving transitions.
func MessageExpiry(mb *Mailbox, hours int, now time.Time) (*time.Time, error) {
	if hours == 0 || (mb != nil && (mb.OwnerUserID != nil || (mb.Kind == "shared" && mb.RetentionHoursOverride == nil))) {
		return nil, nil
	}
	// JSON's RFC3339 year range is narrower than PostgreSQL's timestamptz
	// range. Canonical UTC also avoids historical zone offsets outside the
	// existing wire format. Validate before multiplying even a native-size int.
	at := now.UTC()
	if at.Year() < 0 || at.Year() > 9999 {
		return nil, ErrRetentionExpiry
	}
	seconds := at.Unix()
	minSeconds := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	maxSeconds := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Unix()
	h := int64(hours)
	// These differences are bounded by the validated year range. Division
	// toward zero gives the lower ceiling and upper floor, retaining nanos.
	if h < (minSeconds-seconds)/3600 || h > (maxSeconds-seconds)/3600 {
		return nil, ErrRetentionExpiry
	}
	t := time.Unix(seconds+h*3600, int64(at.Nanosecond())).UTC()
	return &t, nil
}
