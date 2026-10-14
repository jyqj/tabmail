package company

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// PermissionRevision is a compound observation of persistent state, not a
// timestamp or override-row identity. Decimal strings avoid JS integer loss.
type PermissionRevision struct {
	UserID          uuid.UUID  `json:"user_id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	UserRevision    string     `json:"user_revision"`
	ProfileID       *uuid.UUID `json:"profile_id"`
	ProfileRevision *string    `json:"profile_revision"`
}

func (r PermissionRevision) Validate() error {
	if r.UserID == uuid.Nil || r.TenantID == uuid.Nil {
		return fmt.Errorf("revision identity is required")
	}
	valid := func(s string) bool {
		n, e := strconv.ParseInt(s, 10, 64)
		return e == nil && n > 0 && strconv.FormatInt(n, 10) == s
	}
	if !valid(r.UserRevision) {
		return fmt.Errorf("positive decimal user revision required")
	}
	if (r.ProfileID == nil) != (r.ProfileRevision == nil) {
		return fmt.Errorf("profile identity and revision must be observed together")
	}
	if r.ProfileID != nil && (*r.ProfileID == uuid.Nil || !valid(*r.ProfileRevision)) {
		return fmt.Errorf("invalid profile observation")
	}
	return nil
}
func (r PermissionRevision) Equal(o PermissionRevision) bool {
	if r.UserID != o.UserID || r.TenantID != o.TenantID || r.UserRevision != o.UserRevision || (r.ProfileID == nil) != (o.ProfileID == nil) || (r.ProfileRevision == nil) != (o.ProfileRevision == nil) {
		return false
	}
	return r.ProfileID == nil || (*r.ProfileID == *o.ProfileID && *r.ProfileRevision == *o.ProfileRevision)
}

type DomainAccess struct {
	Mode    string      `json:"mode"`
	ZoneIDs []uuid.UUID `json:"zone_ids"`
}

func (d DomainAccess) Validate() error {
	switch d.Mode {
	case "inherit", "all", "none":
		if len(d.ZoneIDs) != 0 {
			return fmt.Errorf("%s cannot include zone IDs", d.Mode)
		}
	case "list":
		if len(d.ZoneIDs) == 0 {
			return fmt.Errorf("list requires at least one zone")
		}
		seen := map[uuid.UUID]bool{}
		for _, id := range d.ZoneIDs {
			if id == uuid.Nil || seen[id] {
				return fmt.Errorf("zone IDs must be nonzero and distinct")
			}
			seen[id] = true
		}
	default:
		return fmt.Errorf("unknown domain access mode")
	}
	return nil
}

// PermissionField preserves omitted/NULL/false/zero independently.
type PermissionField[T any] struct {
	Present bool
	Inherit bool
	Value   T
}

func (f *PermissionField[T]) UnmarshalJSON(raw []byte) error {
	*f = PermissionField[T]{Present: true}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		f.Inherit = true
		return nil
	}
	return json.Unmarshal(raw, &f.Value)
}

type PermissionPatch struct {
	CanSend           PermissionField[bool]         `json:"can_send"`
	DailySendQuota    PermissionField[int]          `json:"daily_send_quota"`
	DailyReceiveQuota PermissionField[int]          `json:"daily_receive_quota"`
	MaxMailboxes      PermissionField[int]          `json:"max_mailboxes"`
	MaxDomains        PermissionField[int]          `json:"max_domains"`
	CanCreateDomains  PermissionField[bool]         `json:"can_create_domains"`
	CanCreateRoutes   PermissionField[bool]         `json:"can_create_routes"`
	CanCreateAPIKeys  PermissionField[bool]         `json:"can_create_api_keys"`
	DomainAccess      PermissionField[DomainAccess] `json:"domain_access"`
}

func (p PermissionPatch) Validate() error {
	for _, f := range []PermissionField[int]{p.DailySendQuota, p.DailyReceiveQuota, p.MaxMailboxes, p.MaxDomains} {
		if f.Present && !f.Inherit && (f.Value < 0 || int64(f.Value) > 2147483647) {
			return fmt.Errorf("quota must be a nonnegative database integer")
		}
	}
	if p.DomainAccess.Present && !p.DomainAccess.Inherit {
		return p.DomainAccess.Value.Validate()
	}
	return nil
}
func (p PermissionPatch) Empty() bool {
	return !p.CanSend.Present && !p.DailySendQuota.Present && !p.DailyReceiveQuota.Present && !p.MaxMailboxes.Present && !p.MaxDomains.Present && !p.CanCreateDomains.Present && !p.CanCreateRoutes.Present && !p.CanCreateAPIKeys.Present && !p.DomainAccess.Present
}

type PermissionEditorCommand struct {
	ExpectedRevision PermissionRevision `json:"expected_revision"`
	Patch            PermissionPatch    `json:"patch"`
}

type RawPermissionOverrides struct {
	CanSend           *bool        `json:"can_send"`
	DailySendQuota    *int         `json:"daily_send_quota"`
	DailyReceiveQuota *int         `json:"daily_receive_quota"`
	MaxMailboxes      *int         `json:"max_mailboxes"`
	MaxDomains        *int         `json:"max_domains"`
	AllowedZoneIDs    []uuid.UUID  `json:"allowed_zone_ids"`
	CanCreateDomains  *bool        `json:"can_create_domains"`
	CanCreateRoutes   *bool        `json:"can_create_routes"`
	CanCreateAPIKeys  *bool        `json:"can_create_api_keys"`
	DomainAccess      DomainAccess `json:"domain_access"`
}
type PermissionEditorCapabilities struct {
	Patch         bool `json:"patch"`
	AssignProfile bool `json:"assign_profile"`
}
type PermissionEditorSnapshot struct {
	UserID       uuid.UUID                    `json:"user_id"`
	TenantID     uuid.UUID                    `json:"tenant_id"`
	Profile      *models.PermissionProfile    `json:"profile"`
	Overrides    *RawPermissionOverrides      `json:"overrides"`
	Effective    *models.EffectivePermission  `json:"effective"`
	FieldSources map[string]string            `json:"field_sources"`
	Revision     PermissionRevision           `json:"revision"`
	Capabilities PermissionEditorCapabilities `json:"capabilities"`
}

// PermissionAssignmentCommand observes both the existing editor and newly
// selected profile. Assignment and override intent are one atomic command.
type PermissionAssignmentCommand struct {
	ExpectedRevision PermissionRevision `json:"expected_revision"`
	ProfileID        *uuid.UUID         `json:"profile_id"`
	ProfileRevision  *string            `json:"profile_revision"`
	Patch            PermissionPatch    `json:"patch"`
}
type PermissionProfileDeletionPreview struct {
	ProfileID       uuid.UUID                         `json:"profile_id"`
	ProfileRevision string                            `json:"profile_revision"`
	Members         []PermissionRevision              `json:"members"`
	Changes         []PermissionProfileDeletionChange `json:"changes"`
}

type PermissionProfileDeletionChange struct {
	Revision PermissionRevision          `json:"revision"`
	Before   *models.EffectivePermission `json:"before"`
	After    *models.EffectivePermission `json:"after"`
}
