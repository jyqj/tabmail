// Package company contains the employee-mail workflows. It reuses users,
// mailbox_grants and the existing queue; it is not a second identity system.
package company

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type Settings struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	Name          string    `json:"name"`
	PrimaryZoneID uuid.UUID `json:"primary_zone_id"`
	Domain        string    `json:"domain"`
	Revision      int       `json:"revision"`
	// MailSendPolicy is the tenant-wide outbound send policy default for every
	// company mailbox (free | template_required | disabled); per-mailbox
	// overrides live on mailboxes.send_policy. On input an empty value keeps
	// the current default unchanged, mirroring the "absent field = no change"
	// convention clients get by omitting optional JSON fields.
	MailSendPolicy string `json:"mail_send_policy,omitempty"`
}
type InvitationInput struct {
	Email               string     `json:"email"`
	LocalPart           string     `json:"local_part"`
	DisplayName         string     `json:"display_name"`
	PermissionProfileID *uuid.UUID `json:"permission_profile_id,omitempty"`
}
type Invitation struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	Address     string     `json:"mailbox_address"`
	DisplayName string     `json:"display_name"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
type MailboxInput struct {
	LocalPart      string     `json:"local_part"`
	Kind           string     `json:"kind"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id,omitempty"`
	RetentionHours *int       `json:"retention_hours,omitempty"`
}
type MailboxAccess struct {
	Mailbox      models.Mailbox `json:"mailbox"`
	CanRead      bool           `json:"can_read"`
	CanOrganize  bool           `json:"can_organize"`
	CanSend      bool           `json:"can_send"`
	TemplateOnly bool           `json:"template_only"`
	Revision     int64          `json:"revision"`
	// CanManage is administrative visibility only (authz.MailboxDecision):
	// it never confers content access and is deliberately not serialized.
	CanManage bool `json:"-"`
}
type Variable struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"` // text, email, integer, date, url
	Required  bool     `json:"required"`
	MaxLength int      `json:"max_length"`
	Options   []string `json:"options,omitempty"`
}
type TemplateDraft struct {
	Subject   string     `json:"subject"`
	TextBody  string     `json:"text_body"`
	HTMLBody  string     `json:"html_body"`
	Variables []Variable `json:"variables"`
}
type Template struct {
	ID        uuid.UUID     `json:"id"`
	Name      string        `json:"name"`
	Draft     TemplateDraft `json:"draft"`
	Revision  int           `json:"revision"`
	Retired   bool          `json:"retired"`
	UpdatedAt time.Time     `json:"updated_at"`
}
type TemplateVersion struct {
	ID          uuid.UUID     `json:"id"`
	TemplateID  uuid.UUID     `json:"template_id"`
	Name        string        `json:"name"`
	Version     int           `json:"version"`
	Snapshot    TemplateDraft `json:"snapshot"`
	ContentHash string        `json:"content_hash"`
	PublishedAt time.Time     `json:"published_at"`
	RevokedAt   *time.Time    `json:"revoked_at,omitempty"`
}
type TemplateGrant struct {
	TemplateID uuid.UUID `json:"template_id"`
	MailboxID  uuid.UUID `json:"mailbox_id"`
	UserID     uuid.UUID `json:"user_id"`
}
type DraftPayload struct {
	To                []string          `json:"to"`
	CC                []string          `json:"cc,omitempty"`
	BCC               []string          `json:"bcc,omitempty"`
	Subject           string            `json:"subject"`
	TextBody          string            `json:"text_body"`
	HTMLBody          string            `json:"html_body,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	TemplateVersionID *uuid.UUID        `json:"template_version_id,omitempty"`
	TemplateVars      map[string]string `json:"template_vars,omitempty"`
	AttachmentIDs     []uuid.UUID       `json:"attachment_ids,omitempty"`
}

// Draft template version eligibility states, resolved by the store with the
// fixed priority missing > revoked > retired > unauthorized > corrupt. They
// are interaction-only labels for the compose surface: they are never an
// authorization decision and every execution re-resolves and re-authorizes
// against the live version row (TemplateForSend).
const (
	TemplateVersionMissing      = "missing"
	TemplateVersionRevoked      = "revoked"
	TemplateVersionCorrupt      = "corrupt"
	TemplateVersionRetired      = "retired"
	TemplateVersionUnauthorized = "unauthorized"
	TemplateVersionUsable       = "usable"
)

// DraftTemplateVersion surfaces a draft's pinned template version eligibility.
// Snapshot is embedded only in the usable state — a revoked or retired
// version's content never leaves the store, matching the Preview denial.
type DraftTemplateVersion struct {
	ID         uuid.UUID      `json:"id"`
	TemplateID uuid.UUID      `json:"template_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Version    int            `json:"version,omitempty"`
	Status     string         `json:"status"`
	Snapshot   *TemplateDraft `json:"snapshot,omitempty"`
}

type Draft struct {
	ID              uuid.UUID             `json:"id"`
	MailboxID       uuid.UUID             `json:"mailbox_id"`
	Payload         DraftPayload          `json:"payload"`
	TemplateVersion *DraftTemplateVersion `json:"template_version,omitempty"`
	Revision        int                   `json:"revision"`
	UpdatedAt       time.Time             `json:"updated_at"`
}
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	MailboxID   uuid.UUID `json:"mailbox_id"`
	UserID      uuid.UUID `json:"-"`
	ObjectKey   string    `json:"-"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"-"`
	State       string    `json:"state"`
}
type MailEvent struct {
	Sequence  int64      `json:"sequence"`
	Type      string     `json:"type"`
	MessageID *uuid.UUID `json:"message_id,omitempty"`
}
type Recipient struct {
	Address    string    `json:"address"`
	State      string    `json:"state"`
	SMTPCode   int       `json:"smtp_code"`
	Diagnostic string    `json:"diagnostic,omitempty"`
	Attempts   int       `json:"attempts"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type RecoveryTarget struct {
	MailboxID uuid.UUID `json:"mailbox_id"`
	Address   string    `json:"address"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
}
type RecoveryReceipt struct {
	ID        uuid.UUID        `json:"id"`
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
	RawKey    string           `json:"-"`
	RawHash   string           `json:"-"`
	RawSize   int64            `json:"raw_size"`
	Targets   []RecoveryTarget `json:"targets"`
}

var localPartPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func ValidLocalPart(s string) bool {
	return localPartPattern.MatchString(s) && !strings.Contains(s, "..") && !strings.HasSuffix(s, ".")
}
func Hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func Digest(v any) string  { b, _ := json.Marshal(v); return Hash(string(b)) }

// The Repository surface is partitioned into role interfaces so consumers can
// depend on the business capability they use instead of the whole workflow
// face. The roles are a strict partition: Repository embeds all of them and
// adds nothing, so every implementation of the roles is a Repository and vice
// versa (asserted in types_iface_test.go).

// SettingsService is the company-level configuration surface: read and
// reconfigure tenant settings, plus the production readiness probe the
// assembly uses to gate company routes on a live store.
type SettingsService interface {
	GetCompanySettings(context.Context, uuid.UUID) (*Settings, error)
	ConfigureCompany(context.Context, authz.Actor, Settings) (*Settings, error)
	Readiness(context.Context) error
}

// EmployeeService is the employee lifecycle: invite, list/revoke invitations,
// activate, and offboard.
type EmployeeService interface {
	InviteEmployee(context.Context, authz.Actor, InvitationInput, string) (*Invitation, error)
	ListEmployeeInvitations(context.Context, authz.Actor) ([]Invitation, error)
	RevokeEmployeeInvitation(context.Context, authz.Actor, uuid.UUID) error
	ActivateEmployee(context.Context, string, string) error
	OffboardEmployee(context.Context, authz.Actor, uuid.UUID, uuid.UUID, string) error
}

// MailboxAdminService is company mailbox administration: query and create
// work mailboxes, convert/transfer ownership, and manage grants and the
// per-mailbox send policy.
type MailboxGrantSnapshot struct {
	Revision int64                 `json:"revision"`
	Grants   []models.MailboxGrant `json:"grants"`
}

type MailboxAdminService interface {
	GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*MailboxAccess, error)
	ListWorkMailboxes(context.Context, authz.Actor) ([]MailboxAccess, error)
	CreateWorkMailbox(context.Context, authz.Actor, MailboxInput) (*models.Mailbox, error)
	ConvertSharedMailbox(context.Context, authz.Actor, uuid.UUID, int64, string) error
	TransferWorkMailbox(context.Context, authz.Actor, uuid.UUID, uuid.UUID, int64, string) error
	ListWorkGrants(context.Context, authz.Actor, uuid.UUID) (*MailboxGrantSnapshot, error)
	SetWorkGrant(context.Context, authz.Actor, models.MailboxGrant, int64) error
	// SetWorkMailboxSendPolicy stores the mailbox-level send-policy override.
	// A nil policy clears the override so the mailbox inherits the company
	// default again.
	SetWorkMailboxSendPolicy(context.Context, authz.Actor, uuid.UUID, *string, int64) error
}

// TemplateAdminService is template governance for administrators: CRUD,
// publish/retire, version history, per-mailbox grants, and the usable list.
type TemplateAdminService interface {
	ListMailTemplates(context.Context, authz.Actor) ([]Template, error)
	SaveMailTemplate(context.Context, authz.Actor, Template) (*Template, error)
	PublishMailTemplate(context.Context, authz.Actor, uuid.UUID, int) (*TemplateVersion, error)
	SetMailTemplateRetired(context.Context, authz.Actor, uuid.UUID, int, bool) error
	// RevokeMailTemplateVersion is the emergency one-way revoke of a single
	// published version: every not-yet-started delivery attempt of that version
	// is stopped, while already-delivered outcomes, history and sibling versions
	// are untouched. revision is the template CAS the caller read; a first-time
	// revoke requires it and bumps the template revision, an already-revoked
	// version is an idempotent no-op. There is no unrevoke — fixing a mistake
	// means publishing a new version.
	RevokeMailTemplateVersion(context.Context, authz.Actor, uuid.UUID, int, int) error
	ListTemplateVersions(context.Context, authz.Actor, uuid.UUID) ([]TemplateVersion, error)
	ListTemplateGrants(context.Context, authz.Actor, uuid.UUID) ([]TemplateGrant, error)
	SetTemplateGrant(context.Context, authz.Actor, TemplateGrant, bool) error
	ListUsableTemplates(context.Context, authz.Actor, uuid.UUID) ([]TemplateVersion, error)
}

// TemplateSendReader is the narrow published-template lookup the outbound
// engine depends on. It is structurally identical to outbound.TemplateGovernance,
// which stays the dependency interface on the outbound side.
type TemplateSendReader interface {
	TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*TemplateVersion, string, string, error)
}

// DraftWorkspace owns draft revisions, not attachment storage.
type DraftWorkspace interface {
	ListMailDrafts(context.Context, authz.Actor) ([]Draft, error)
	GetMailDraft(context.Context, authz.Actor, uuid.UUID) (*Draft, error)
	SaveMailDraft(context.Context, authz.Actor, Draft) (*Draft, error)
	DeleteMailDraft(context.Context, authz.Actor, uuid.UUID, int) error
}

// AttachmentRepository owns the authorized reserve/finish handshake.
type AttachmentRepository interface {
	ReserveMailAttachment(context.Context, authz.Actor, Attachment) (*Attachment, error)
	FinishMailAttachment(context.Context, authz.Actor, uuid.UUID, string) error
	GetWorkAttachment(context.Context, authz.Actor, uuid.UUID) (*Attachment, error)
}

// DraftService preserves the aggregate production adapter.
type DraftService interface {
	DraftWorkspace
	AttachmentRepository
}

// MailReadService is the mailbox content surface: list and mutate messages,
// and replay the durable mailbox event stream.
type MailReadService interface {
	// GetWorkMessage returns internal metadata only after current member, tenant,
	// exact mailbox identity and read rights have been checked in one transaction.
	GetWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*models.Message, error)
	ListWorkMessages(context.Context, authz.Actor, uuid.UUID, string, string, models.Page) ([]*models.Message, int, error)
	MutateWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID, string) error
	ListMailboxEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]MailEvent, int64, error)
}

// SubmissionReceipts is the employee's own/readable-mailbox operation history.
// Receipt visibility alone never grants sent-content access.
type SubmissionReceipts interface {
	ListSubmissions(context.Context, authz.Actor, models.Page) ([]Submission, int, error)
	GetSubmission(context.Context, authz.Actor, uuid.UUID) (*Submission, error)
}

// SubmissionContentReader requires a CURRENT read right on the sender mailbox.
// An author, owner or administrator is not exempt from revocation.
type SubmissionContentReader interface {
	GetSubmissionContent(context.Context, authz.Actor, uuid.UUID) (*SubmissionContent, error)
	ListSubmissionAttachments(context.Context, authz.Actor, uuid.UUID) ([]SubmissionAttachment, error)
	GetSubmissionAttachment(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*SubmissionAttachment, error)
}

// DeliveryRecovery is an operations capability, not an employee content port.
type DeliveryRecovery interface {
	ListOutboundRecipients(context.Context, uuid.UUID, uuid.UUID) ([]Recipient, error)
	ReconcileOutbound(context.Context, authz.Actor, uuid.UUID, time.Time, []Recipient, string) error
}

// SubmissionReader preserves the aggregate production adapter.
type SubmissionReader interface {
	SubmissionReceipts
	SubmissionContentReader
	DeliveryRecovery
}

// RecoveryService is the durable recovery-receipt surface: list, inspect,
// and retry failed receipt replays.
type RecoveryService interface {
	ListRecoveryReceipts(context.Context, authz.Actor, models.Page) ([]RecoveryReceipt, int, error)
	InspectRecoveryReceipt(context.Context, authz.Actor, uuid.UUID, string) (*RecoveryReceipt, error)
	RetryRecoveryReceipt(context.Context, authz.Actor, uuid.UUID, time.Time, []uuid.UUID, string, string) error
}

// Repository is the optional production workflow surface. Legacy test adapters
// need not invent fake transactions to expose endpoints they cannot implement.
type Repository interface {
	SettingsService
	EmployeeService
	MailboxAdminService
	TemplateAdminService
	TemplateSendReader
	DraftService
	MailReadService
	SubmissionReader
	RecoveryService
}
