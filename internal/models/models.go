package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
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
	ID         uuid.UUID  `json:"id" db:"id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	KeyHash    string     `json:"-" db:"key_hash"`
	KeyPrefix  string     `json:"key_prefix" db:"key_prefix"`
	Label      string     `json:"label" db:"label"`
	Scopes     []string   `json:"scopes" db:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
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

type DomainZone struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Domain     string     `json:"domain" db:"domain"`
	IsVerified bool       `json:"is_verified" db:"is_verified"`
	MXVerified bool       `json:"mx_verified" db:"mx_verified"`
	TXTRecord  string     `json:"txt_record,omitempty" db:"txt_record"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	VerifiedAt *time.Time `json:"verified_at,omitempty" db:"verified_at"`
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
	RetentionHoursOverride *int       `json:"retention_hours_override,omitempty" db:"retention_hours_override"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt              time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// Message
// ============================================================

type Message struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	TenantID     uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	MailboxID    uuid.UUID       `json:"mailbox_id" db:"mailbox_id"`
	ZoneID       uuid.UUID       `json:"zone_id" db:"zone_id"`
	Sender       string          `json:"sender" db:"sender"`
	Recipients   []string        `json:"recipients" db:"recipients"`
	Subject      string          `json:"subject" db:"subject"`
	Size         int64           `json:"size" db:"size"`
	Seen         bool            `json:"seen" db:"seen"`
	RawObjectKey string          `json:"raw_object_key,omitempty" db:"raw_object_key"`
	HeadersJSON  json.RawMessage `json:"headers,omitempty" db:"headers_json"`
	ReceivedAt   time.Time       `json:"received_at" db:"received_at"`
	ExpiresAt    time.Time       `json:"expires_at" db:"expires_at"`
}

// MessageDetail includes parsed body content.
type MessageDetail struct {
	Message
	TextBody string `json:"text_body,omitempty"`
	HTMLBody string `json:"html_body,omitempty"`
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
