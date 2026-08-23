package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ============================================================
// User
// ============================================================

type UserRole string

const (
	RoleSuperAdmin UserRole = "super_admin"
	RoleAdmin      UserRole = "admin"
	RoleUser       UserRole = "user"
)

type User struct {
	ID                  uuid.UUID  `json:"id" db:"id"`
	TenantID            uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Email               string     `json:"email" db:"email"`
	PasswordHash        string     `json:"-" db:"password_hash"`
	DisplayName         string     `json:"display_name" db:"display_name"`
	Role                UserRole   `json:"role" db:"role"`
	IsActive            bool       `json:"is_active" db:"is_active"`
	PermissionProfileID *uuid.UUID `json:"permission_profile_id,omitempty" db:"permission_profile_id"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt         *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

type RefreshToken struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	UserID    uuid.UUID  `json:"user_id" db:"user_id"`
	TokenHash string     `json:"-" db:"token_hash"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
}

type AdminInvitation struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	Email      string     `json:"email" db:"email"`
	InviteCode string     `json:"-" db:"invite_code"`
	InvitedBy  *uuid.UUID `json:"invited_by,omitempty" db:"invited_by"`
	ExpiresAt  time.Time  `json:"expires_at" db:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty" db:"accepted_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// System settings
// ============================================================

type SystemSetting struct {
	Key         string    `json:"key" db:"key"`
	Value       string    `json:"value" db:"value"`
	Description string    `json:"description" db:"description"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Well-known setting keys.
const (
	SettingAutoCreateRouteRPM  = "auto_create_route_rpm"
	SettingAutoCreateTenantRPM = "auto_create_tenant_rpm"
	SettingMailboxNaming       = "mailbox_naming"
	SettingStripPlusTag        = "strip_plus_tag"
	SettingMonitorHistory      = "monitor_history"
	SettingFallbackRetentionH  = "fallback_retention_hours"
	SettingOpenRegistration    = "open_registration"
	SettingPublicIPRPM         = "public_ip_rpm"
)

// ============================================================
// Plan
// ============================================================

type Plan struct {
	ID                    uuid.UUID `json:"id" db:"id"`
	Name                  string    `json:"name" db:"name"`
	MaxDomains            int       `json:"max_domains" db:"max_domains"`
	MaxMailboxesPerDomain int       `json:"max_mailboxes_per_domain" db:"max_mailboxes_per_domain"`
	MaxMessagesPerMailbox int       `json:"max_messages_per_mailbox" db:"max_messages_per_mailbox"`
	MaxMessageBytes       int       `json:"max_message_bytes" db:"max_message_bytes"`
	RetentionHours        int       `json:"retention_hours" db:"retention_hours"`
	RPMLimit              int       `json:"rpm_limit" db:"rpm_limit"`
	DailyQuota            int       `json:"daily_quota" db:"daily_quota"`
	CreatedAt             time.Time `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================
// Tenant
// ============================================================

type Tenant struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	PlanID    uuid.UUID `json:"plan_id" db:"plan_id"`
	IsSuper   bool      `json:"is_super" db:"is_super"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type TenantOverride struct {
	ID                    uuid.UUID `json:"id" db:"id"`
	TenantID              uuid.UUID `json:"tenant_id" db:"tenant_id"`
	MaxDomains            *int      `json:"max_domains,omitempty" db:"max_domains"`
	MaxMailboxesPerDomain *int      `json:"max_mailboxes_per_domain,omitempty" db:"max_mailboxes_per_domain"`
	MaxMessagesPerMailbox *int      `json:"max_messages_per_mailbox,omitempty" db:"max_messages_per_mailbox"`
	MaxMessageBytes       *int      `json:"max_message_bytes,omitempty" db:"max_message_bytes"`
	RetentionHours        *int      `json:"retention_hours,omitempty" db:"retention_hours"`
	RPMLimit              *int      `json:"rpm_limit,omitempty" db:"rpm_limit"`
	DailyQuota            *int      `json:"daily_quota,omitempty" db:"daily_quota"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

type TenantAPIKey struct {
	ID             uuid.UUID   `json:"id" db:"id"`
	TenantID       uuid.UUID   `json:"tenant_id" db:"tenant_id"`
	KeyHash        string      `json:"-" db:"key_hash"`
	KeyPrefix      string      `json:"key_prefix" db:"key_prefix"`
	Label          string      `json:"label" db:"label"`
	Scopes         []string    `json:"scopes" db:"scopes"`
	OwnerUserID    *uuid.UUID  `json:"owner_user_id,omitempty" db:"owner_user_id"`
	AllowedZoneIDs []uuid.UUID `json:"allowed_zone_ids,omitempty" db:"allowed_zone_ids"`
	ExpiresAt      *time.Time  `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt      time.Time   `json:"created_at" db:"created_at"`
	LastUsedAt     *time.Time  `json:"last_used_at,omitempty" db:"last_used_at"`
	LastUsedIP     *string     `json:"last_used_ip,omitempty" db:"last_used_ip"`
}

// EffectiveConfig merges plan defaults with tenant overrides.
// Fields always contain a resolved, non-nil value.
type EffectiveConfig struct {
	MaxDomains            int `json:"max_domains"`
	MaxMailboxesPerDomain int `json:"max_mailboxes_per_domain"`
	MaxMessagesPerMailbox int `json:"max_messages_per_mailbox"`
	MaxMessageBytes       int `json:"max_message_bytes"`
	RetentionHours        int `json:"retention_hours"`
	RPMLimit              int `json:"rpm_limit"`
	DailyQuota            int `json:"daily_quota"`
}

// ============================================================
// Domain zone & route
// ============================================================

type ResourceVisibility string

const (
	VisibilityPrivate       ResourceVisibility = "private"
	VisibilityAuthenticated ResourceVisibility = "authenticated"
	VisibilityPublic        ResourceVisibility = "public"
)

func (v ResourceVisibility) Valid() bool {
	switch v {
	case VisibilityPrivate, VisibilityAuthenticated, VisibilityPublic:
		return true
	default:
		return false
	}
}

type DomainZone struct {
	ID                    uuid.UUID          `json:"id" db:"id"`
	TenantID              uuid.UUID          `json:"tenant_id" db:"tenant_id"`
	OwnerUserID           *uuid.UUID         `json:"owner_user_id,omitempty" db:"owner_user_id"`
	ParentZoneID          *uuid.UUID         `json:"parent_zone_id,omitempty" db:"parent_zone_id"`
	Domain                string             `json:"domain" db:"domain"`
	Visibility            ResourceVisibility `json:"visibility" db:"visibility"`
	AllowRandomSubdomains bool               `json:"allow_random_subdomains" db:"allow_random_subdomains"`
	IsVerified            bool               `json:"is_verified" db:"is_verified"`
	MXVerified            bool               `json:"mx_verified" db:"mx_verified"`
	TXTRecord             string             `json:"txt_record,omitempty" db:"txt_record"`
	DKIMPrivateKeyPEM     *string            `json:"-" db:"dkim_private_key_pem"`
	DKIMSelector          string             `json:"dkim_selector" db:"dkim_selector"`
	DKIMEnabled           bool               `json:"dkim_enabled" db:"dkim_enabled"`
	DKIMRequiredForSend   bool               `json:"dkim_required_for_send" db:"dkim_required_for_send"`
	CreatedAt             time.Time          `json:"created_at" db:"created_at"`
	VerifiedAt            *time.Time         `json:"verified_at,omitempty" db:"verified_at"`
}

// CanReceiveMessage reports whether the zone is ready to accept inbound mail:
// ownership must be verified and the MX records confirmed. This is the single
// home for the receive-readiness rule; DKIM/send configuration is deliberately
// kept separate.
func (z DomainZone) CanReceiveMessage() bool {
	return z.IsVerified && z.MXVerified
}

type RouteType string

const (
	RouteExact        RouteType = "exact"
	RouteWildcard     RouteType = "wildcard"
	RouteDeepWildcard RouteType = "deep_wildcard"
	RouteSequence     RouteType = "sequence"
)

func (r RouteType) Valid() bool {
	switch r {
	case RouteExact, RouteWildcard, RouteDeepWildcard, RouteSequence:
		return true
	default:
		return false
	}
}

type DomainRoute struct {
	ID                     uuid.UUID  `json:"id" db:"id"`
	ZoneID                 uuid.UUID  `json:"zone_id" db:"zone_id"`
	RouteType              RouteType  `json:"route_type" db:"route_type"`
	MatchValue             string     `json:"match_value" db:"match_value"`
	RangeStart             *int       `json:"range_start,omitempty" db:"range_start"`
	RangeEnd               *int       `json:"range_end,omitempty" db:"range_end"`
	AutoCreateMailbox      bool       `json:"auto_create_mailbox" db:"auto_create_mailbox"`
	RetentionHoursOverride *int       `json:"retention_hours_override,omitempty" db:"retention_hours_override"`
	AccessModeDefault      AccessMode `json:"access_mode_default" db:"access_mode_default"`
	CreatedAt              time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// Mailbox
// ============================================================

type AccessMode string

const (
	AccessPublic AccessMode = "public"
	AccessToken  AccessMode = "token"
	AccessAPIKey AccessMode = "api_key"
)

func (a AccessMode) Valid() bool {
	switch a {
	case AccessPublic, AccessToken, AccessAPIKey:
		return true
	default:
		return false
	}
}

type Mailbox struct {
	ID                     uuid.UUID  `json:"id" db:"id"`
	TenantID               uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	ZoneID                 uuid.UUID  `json:"zone_id" db:"zone_id"`
	RouteID                *uuid.UUID `json:"route_id,omitempty" db:"route_id"`
	LocalPart              string     `json:"local_part" db:"local_part"`
	ResolvedDomain         string     `json:"resolved_domain" db:"resolved_domain"`
	FullAddress            string     `json:"full_address" db:"full_address"`
	AccessMode             AccessMode `json:"access_mode" db:"access_mode"`
	PasswordHash           *string    `json:"-" db:"password_hash"`
	MessageCount           int64      `json:"-" db:"message_count"`
	RetentionHoursOverride *int       `json:"retention_hours_override,omitempty" db:"retention_hours_override"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt              time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// Message
// ============================================================

type Message struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	TenantID      uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	MailboxID     uuid.UUID       `json:"mailbox_id" db:"mailbox_id"`
	ZoneID        uuid.UUID       `json:"zone_id" db:"zone_id"`
	Sender        string          `json:"sender" db:"sender"`
	Recipients    []string        `json:"recipients" db:"recipients"`
	Subject       string          `json:"subject" db:"subject"`
	Size          int64           `json:"size" db:"size"`
	Seen          bool            `json:"seen" db:"seen"`
	RawObjectKey  string          `json:"raw_object_key,omitempty" db:"raw_object_key"`
	HeadersJSON   json.RawMessage `json:"headers,omitempty" db:"headers_json"`
	ReceivedAt    time.Time       `json:"received_at" db:"received_at"`
	ExpiresAt     time.Time       `json:"expires_at" db:"expires_at"`
	OTPCode       string          `json:"otp_code,omitempty" db:"otp_code"`
	OTPConfidence float32         `json:"otp_confidence,omitempty" db:"otp_confidence"`
}

// MessageDetail includes parsed body content.
type MessageDetail struct {
	Message
	TextBody     string `json:"text_body,omitempty"`
	HTMLBody     string `json:"html_body,omitempty"`
	BodyRedacted bool   `json:"body_redacted,omitempty"`
	BodyAccess   string `json:"body_access,omitempty"`
}

// ============================================================
// Audit log
// ============================================================

type AuditEntry struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	TenantID     *uuid.UUID      `json:"tenant_id,omitempty" db:"tenant_id"`
	Actor        string          `json:"actor" db:"actor"`
	Action       string          `json:"action" db:"action"`
	ResourceType string          `json:"resource_type" db:"resource_type"`
	ResourceID   *uuid.UUID      `json:"resource_id,omitempty" db:"resource_id"`
	Details      json.RawMessage `json:"details,omitempty" db:"details"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

type SMTPMetrics struct {
	SessionsOpened      int64 `json:"sessions_opened"`
	SessionsActive      int64 `json:"sessions_active"`
	RecipientsAccepted  int64 `json:"recipients_accepted"`
	RecipientsRejected  int64 `json:"recipients_rejected"`
	MessagesAccepted    int64 `json:"messages_accepted"`
	MessagesRejected    int64 `json:"messages_rejected"`
	DeliveriesSucceeded int64 `json:"deliveries_succeeded"`
	DeliveriesFailed    int64 `json:"deliveries_failed"`
	BytesReceived       int64 `json:"bytes_received"`
}

type WebhookMetrics struct {
	Enabled        bool  `json:"enabled"`
	Configured     int   `json:"configured"`
	Queued         int64 `json:"queued"`
	Delivered      int64 `json:"delivered"`
	Failed         int64 `json:"failed"`
	Retried        int64 `json:"retried"`
	DeadLetterSize int   `json:"dead_letter_size"`
}

type RealtimeMetrics struct {
	SubscribersCurrent int64 `json:"subscribers_current"`
	EventsPublished    int64 `json:"events_published"`
}

type MetricsSnapshot struct {
	StartedAt     time.Time       `json:"started_at"`
	UptimeSeconds int64           `json:"uptime_seconds"`
	SMTP          SMTPMetrics     `json:"smtp"`
	Webhooks      WebhookMetrics  `json:"webhooks"`
	Realtime      RealtimeMetrics `json:"realtime"`
	TimeSeries    []MetricPoint   `json:"time_series"`
}

type MetricPoint struct {
	At                time.Time `json:"at"`
	SMTPAccepted      int64     `json:"smtp_accepted"`
	SMTPRejected      int64     `json:"smtp_rejected"`
	DeliveriesOK      int64     `json:"deliveries_ok"`
	DeliveriesFailed  int64     `json:"deliveries_failed"`
	WebhooksDelivered int64     `json:"webhooks_delivered"`
	WebhooksFailed    int64     `json:"webhooks_failed"`
	RealtimePublished int64     `json:"realtime_published"`
}

type DeliveryStats struct {
	Key              string `json:"key"`
	Accepted         int64  `json:"accepted"`
	Rejected         int64  `json:"rejected"`
	DeliveriesOK     int64  `json:"deliveries_ok"`
	DeliveriesFailed int64  `json:"deliveries_failed"`
}

type DeadLetter struct {
	ID          string          `json:"id"`
	URL         string          `json:"url"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	LastError   string          `json:"last_error"`
	CreatedAt   time.Time       `json:"created_at"`
	LastTriedAt time.Time       `json:"last_tried_at"`
}

type OutboxEvent struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	EventType     string          `json:"event_type" db:"event_type"`
	Payload       json.RawMessage `json:"payload" db:"payload"`
	OccurredAt    time.Time       `json:"occurred_at" db:"occurred_at"`
	State         string          `json:"state" db:"state"`
	Attempts      int             `json:"attempts" db:"attempts"`
	LastError     string          `json:"last_error" db:"last_error"`
	NextAttemptAt time.Time       `json:"next_attempt_at" db:"next_attempt_at"`
	ClaimedAt     *time.Time      `json:"claimed_at,omitempty" db:"claimed_at"`
	LeaseUntil    *time.Time      `json:"lease_until,omitempty" db:"lease_until"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

type WebhookDelivery struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	EventID       uuid.UUID       `json:"event_id" db:"event_id"`
	URL           string          `json:"url" db:"url"`
	EventType     string          `json:"event_type" db:"event_type"`
	Payload       json.RawMessage `json:"payload" db:"payload"`
	State         string          `json:"state" db:"state"`
	Attempts      int             `json:"attempts" db:"attempts"`
	LastError     string          `json:"last_error" db:"last_error"`
	NextAttemptAt time.Time       `json:"next_attempt_at" db:"next_attempt_at"`
	ClaimedAt     *time.Time      `json:"claimed_at,omitempty" db:"claimed_at"`
	LeaseUntil    *time.Time      `json:"lease_until,omitempty" db:"lease_until"`
	LastTriedAt   *time.Time      `json:"last_tried_at,omitempty" db:"last_tried_at"`
	DeliveredAt   *time.Time      `json:"delivered_at,omitempty" db:"delivered_at"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

type IngestJob struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	Source        string          `json:"source" db:"source"`
	RemoteIP      string          `json:"remote_ip" db:"remote_ip"`
	MailFrom      string          `json:"mail_from" db:"mail_from"`
	Recipients    []string        `json:"recipients" db:"recipients"`
	RawObjectKey  string          `json:"raw_object_key" db:"raw_object_key"`
	Metadata      json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	State         string          `json:"state" db:"state"`
	Attempts      int             `json:"attempts" db:"attempts"`
	LastError     string          `json:"last_error" db:"last_error"`
	NextAttemptAt time.Time       `json:"next_attempt_at" db:"next_attempt_at"`
	ClaimedAt     *time.Time      `json:"claimed_at,omitempty" db:"claimed_at"`
	LeaseUntil    *time.Time      `json:"lease_until,omitempty" db:"lease_until"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

type SystemStats struct {
	TenantsCount    int             `json:"tenants_count"`
	PlansCount      int             `json:"plans_count"`
	DomainsCount    int             `json:"domains_count"`
	MailboxesCount  int             `json:"mailboxes_count"`
	MessagesCount   int             `json:"messages_count"`
	Metrics         MetricsSnapshot `json:"metrics"`
	RecentAudit     []*AuditEntry   `json:"recent_audit"`
	TenantDelivery  []DeliveryStats `json:"tenant_delivery"`
	MailboxDelivery []DeliveryStats `json:"mailbox_delivery"`
	DeadLetters     []DeadLetter    `json:"dead_letters"`
}

type SMTPPolicy struct {
	DefaultAccept       bool      `json:"default_accept"`
	AcceptDomains       []string  `json:"accept_domains"`
	RejectDomains       []string  `json:"reject_domains"`
	DefaultStore        bool      `json:"default_store"`
	StoreDomains        []string  `json:"store_domains"`
	DiscardDomains      []string  `json:"discard_domains"`
	RejectOriginDomains []string  `json:"reject_origin_domains"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type MonitorEvent struct {
	ID        uuid.UUID `json:"id"`
	Type      string    `json:"type"`
	Mailbox   string    `json:"mailbox"`
	MessageID string    `json:"message_id,omitempty"`
	Sender    string    `json:"sender,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	Size      int64     `json:"size,omitempty"`
	At        time.Time `json:"at"`
}

// ============================================================
// Pagination
// ============================================================

type Page struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

func (p Page) Offset() int { return (p.Page - 1) * p.PerPage }

func (p Page) Normalize() Page {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = 30
	}
	if p.PerPage > 100 {
		p.PerPage = 100
	}
	return p
}

// ============================================================
// Permission Profile
// ============================================================

type PermissionProfile struct {
	ID                uuid.UUID   `json:"id" db:"id"`
	TenantID          *uuid.UUID  `json:"tenant_id,omitempty" db:"tenant_id"`
	Name              string      `json:"name" db:"name"`
	Description       string      `json:"description" db:"description"`
	CanSend           bool        `json:"can_send" db:"can_send"`
	DailySendQuota    int         `json:"daily_send_quota" db:"daily_send_quota"`
	DailyReceiveQuota int         `json:"daily_receive_quota" db:"daily_receive_quota"`
	MaxMailboxes      int         `json:"max_mailboxes" db:"max_mailboxes"`
	MaxDomains        int         `json:"max_domains" db:"max_domains"`
	AllowedZoneIDs    []uuid.UUID `json:"allowed_zone_ids,omitempty" db:"allowed_zone_ids"`
	CanCreateDomains  bool        `json:"can_create_domains" db:"can_create_domains"`
	CanCreateRoutes   bool        `json:"can_create_routes" db:"can_create_routes"`
	CanCreateAPIKeys  bool        `json:"can_create_api_keys" db:"can_create_api_keys"`
	IsSystem          bool        `json:"is_system" db:"is_system"`
	CreatedAt         time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at" db:"updated_at"`
}

type UserPermissionOverride struct {
	ID                uuid.UUID   `json:"id" db:"id"`
	UserID            uuid.UUID   `json:"user_id" db:"user_id"`
	CanSend           *bool       `json:"can_send,omitempty" db:"can_send"`
	DailySendQuota    *int        `json:"daily_send_quota,omitempty" db:"daily_send_quota"`
	DailyReceiveQuota *int        `json:"daily_receive_quota,omitempty" db:"daily_receive_quota"`
	MaxMailboxes      *int        `json:"max_mailboxes,omitempty" db:"max_mailboxes"`
	MaxDomains        *int        `json:"max_domains,omitempty" db:"max_domains"`
	AllowedZoneIDs    []uuid.UUID `json:"allowed_zone_ids,omitempty" db:"allowed_zone_ids"`
	CanCreateDomains  *bool       `json:"can_create_domains,omitempty" db:"can_create_domains"`
	CanCreateRoutes   *bool       `json:"can_create_routes,omitempty" db:"can_create_routes"`
	CanCreateAPIKeys  *bool       `json:"can_create_api_keys,omitempty" db:"can_create_api_keys"`
	UpdatedAt         time.Time   `json:"updated_at" db:"updated_at"`
}

type EffectivePermission struct {
	CanSend           bool        `json:"can_send"`
	DailySendQuota    int         `json:"daily_send_quota"`
	DailyReceiveQuota int         `json:"daily_receive_quota"`
	MaxMailboxes      int         `json:"max_mailboxes"`
	MaxDomains        int         `json:"max_domains"`
	AllowedZoneIDs    []uuid.UUID `json:"allowed_zone_ids,omitempty"`
	CanCreateDomains  bool        `json:"can_create_domains"`
	CanCreateRoutes   bool        `json:"can_create_routes"`
	CanCreateAPIKeys  bool        `json:"can_create_api_keys"`
}

// AllowsZone reports whether zoneID is within the permission's allowed-zone
// list. A nil permission or an empty list means every zone is allowed. This is
// the canonical home for the zone-allowlist membership rule.
func (p *EffectivePermission) AllowsZone(zoneID uuid.UUID) bool {
	if p == nil {
		return true
	}
	return ZoneAllowed(p.AllowedZoneIDs, zoneID)
}

// ZoneAllowed reports whether zoneID is within allowedZoneIDs. An empty list
// means every zone is allowed.
func ZoneAllowed(allowedZoneIDs []uuid.UUID, zoneID uuid.UUID) bool {
	if len(allowedZoneIDs) == 0 {
		return true
	}
	for _, id := range allowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}

// IsUnlimited returns true if the quota value is 0, which means unlimited.
func IsUnlimited(quota int) bool {
	return quota == 0
}

// ============================================================
// Send Identities
// ============================================================

type SendIdentityType string

const (
	SendIdentityExact          SendIdentityType = "exact"
	SendIdentityDomainWildcard SendIdentityType = "domain_wildcard"
)

type SendIdentity struct {
	ID           uuid.UUID        `json:"id" db:"id"`
	TenantID     uuid.UUID        `json:"tenant_id" db:"tenant_id"`
	ZoneID       uuid.UUID        `json:"zone_id" db:"zone_id"`
	MailboxID    *uuid.UUID       `json:"mailbox_id,omitempty" db:"mailbox_id"`
	Address      string           `json:"address" db:"address"`
	IdentityType SendIdentityType `json:"identity_type" db:"identity_type"`
	Verified     bool             `json:"verified" db:"verified"`
	CreatedAt    time.Time        `json:"created_at" db:"created_at"`
}

// OutboundTemplate is a tenant-scoped email template. The SubjectTmpl, TextTmpl
// and HTMLTmpl fields hold Go template source; the template package parses them
// once at load time and re-renders per message with caller-supplied Vars. The
// HTML part is always rendered through html/template for automatic escaping.
type OutboundTemplate struct {
	ID          uuid.UUID `json:"id" db:"id"`
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`
	SubjectTmpl string    `json:"subject_tmpl" db:"subject_tmpl"`
	TextTmpl    string    `json:"text_tmpl,omitempty" db:"text_tmpl"`
	HTMLTmpl    string    `json:"html_tmpl,omitempty" db:"html_tmpl"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================
// Webhook Endpoints (tenant-level)
// ============================================================

type WebhookEndpoint struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	URL        string     `json:"url" db:"url"`
	Secret     *string    `json:"-" db:"secret"`
	EventTypes []string   `json:"event_types" db:"event_types"`
	IsActive   bool       `json:"is_active" db:"is_active"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

// ============================================================
// Outbound Attempts
// ============================================================

type OutboundAttempt struct {
	ID           uuid.UUID `json:"id" db:"id"`
	JobID        uuid.UUID `json:"job_id" db:"job_id"`
	TenantID     uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Adapter      string    `json:"adapter" db:"adapter"`
	Attempt      int       `json:"attempt" db:"attempt"`
	SMTPCode     int       `json:"smtp_code" db:"smtp_code"`
	SMTPResponse string    `json:"smtp_response" db:"smtp_response"`
	RemoteHost   string    `json:"remote_host" db:"remote_host"`
	StartedAt    time.Time `json:"started_at" db:"started_at"`
	FinishedAt   time.Time `json:"finished_at" db:"finished_at"`
	Error        string    `json:"error" db:"error"`
}

// ============================================================
// Suppression List
// ============================================================

type SuppressionEntry struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TenantID    uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Address     string     `json:"address" db:"address"`
	Reason      string     `json:"reason" db:"reason"`
	SourceJobID *uuid.UUID `json:"source_job_id,omitempty" db:"source_job_id"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// Outbound Job
// ============================================================

type OutboundState string

const (
	OutboundPending    OutboundState = "pending"
	OutboundProcessing OutboundState = "processing"
	OutboundSent       OutboundState = "sent"
	OutboundRetry      OutboundState = "retry"
	OutboundFailed     OutboundState = "failed"
	OutboundDead       OutboundState = "dead"
)

type OutboundJob struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	TenantID        uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	UserID          *uuid.UUID      `json:"user_id,omitempty" db:"user_id"`
	APIKeyID        *uuid.UUID      `json:"api_key_id,omitempty" db:"api_key_id"`
	MailFrom        string          `json:"mail_from" db:"mail_from"`
	RcptTo          []string        `json:"rcpt_to" db:"rcpt_to"`
	To              []string        `json:"to,omitempty" db:"to_addrs"`
	CC              []string        `json:"cc,omitempty" db:"cc_addrs"`
	BCC             []string        `json:"bcc,omitempty" db:"bcc_addrs"`
	Subject         string          `json:"subject" db:"subject"`
	TextBody        string          `json:"text_body,omitempty" db:"text_body"`
	HTMLBody        string          `json:"html_body,omitempty" db:"html_body"`
	HeadersJSON     json.RawMessage `json:"headers,omitempty" db:"headers_json"`
	RawMIME         []byte          `json:"-" db:"raw_mime"`
	ZoneID          uuid.UUID       `json:"zone_id" db:"zone_id"`
	State           OutboundState   `json:"state" db:"state"`
	Attempts        int             `json:"attempts" db:"attempts"`
	MaxAttempts     int             `json:"max_attempts" db:"max_attempts"`
	LastError       string          `json:"last_error,omitempty" db:"last_error"`
	NextAttemptAt   time.Time       `json:"next_attempt_at" db:"next_attempt_at"`
	ClaimedAt       *time.Time      `json:"claimed_at,omitempty" db:"claimed_at"`
	LeaseUntil      *time.Time      `json:"lease_until,omitempty" db:"lease_until"`
	SMTPCode        *int            `json:"smtp_code,omitempty" db:"smtp_code"`
	SMTPResponse    string          `json:"smtp_response,omitempty" db:"smtp_response"`
	MessageIDHeader string          `json:"message_id_header,omitempty" db:"message_id_header"`
	DeliveryToken   *uuid.UUID      `json:"delivery_token,omitempty" db:"delivery_token"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}
