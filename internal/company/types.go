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
type Draft struct {
	ID        uuid.UUID    `json:"id"`
	MailboxID uuid.UUID    `json:"mailbox_id"`
	Payload   DraftPayload `json:"payload"`
	Revision  int          `json:"revision"`
	UpdatedAt time.Time    `json:"updated_at"`
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

// Repository is the optional production workflow surface. Legacy test adapters
// need not invent fake transactions to expose endpoints they cannot implement.
type Repository interface {
	GetCompanySettings(context.Context, uuid.UUID) (*Settings, error)
	ConfigureCompany(context.Context, authz.Actor, Settings) (*Settings, error)
	InviteEmployee(context.Context, authz.Actor, InvitationInput, string) (*Invitation, error)
	ListEmployeeInvitations(context.Context, authz.Actor) ([]Invitation, error)
	RevokeEmployeeInvitation(context.Context, authz.Actor, uuid.UUID) error
	ActivateEmployee(context.Context, string, string) error
	GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*MailboxAccess, error)
	ListWorkMailboxes(context.Context, authz.Actor) ([]MailboxAccess, error)
	CreateWorkMailbox(context.Context, authz.Actor, MailboxInput) (*models.Mailbox, error)
	ConvertSharedMailbox(context.Context, authz.Actor, uuid.UUID, int64, string) error
	TransferWorkMailbox(context.Context, authz.Actor, uuid.UUID, uuid.UUID, int64, string) error
	OffboardEmployee(context.Context, authz.Actor, uuid.UUID, uuid.UUID, string) error
	ListWorkGrants(context.Context, authz.Actor, uuid.UUID) ([]models.MailboxGrant, error)
	SetWorkGrant(context.Context, authz.Actor, models.MailboxGrant) error
	ListMailTemplates(context.Context, authz.Actor) ([]Template, error)
	SaveMailTemplate(context.Context, authz.Actor, Template) (*Template, error)
	PublishMailTemplate(context.Context, authz.Actor, uuid.UUID, int) (*TemplateVersion, error)
	SetMailTemplateRetired(context.Context, authz.Actor, uuid.UUID, int, bool) error
	ListTemplateVersions(context.Context, authz.Actor, uuid.UUID) ([]TemplateVersion, error)
	ListTemplateGrants(context.Context, authz.Actor, uuid.UUID) ([]TemplateGrant, error)
	SetTemplateGrant(context.Context, authz.Actor, TemplateGrant, bool) error
	ListUsableTemplates(context.Context, authz.Actor, uuid.UUID) ([]TemplateVersion, error)
	TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*TemplateVersion, string, string, error)
	ListMailDrafts(context.Context, authz.Actor) ([]Draft, error)
	GetMailDraft(context.Context, authz.Actor, uuid.UUID) (*Draft, error)
	SaveMailDraft(context.Context, authz.Actor, Draft) (*Draft, error)
	DeleteMailDraft(context.Context, authz.Actor, uuid.UUID, int) error
	ReserveMailAttachment(context.Context, authz.Actor, Attachment) (*Attachment, error)
	FinishMailAttachment(context.Context, authz.Actor, uuid.UUID, string) error
	GetWorkAttachment(context.Context, authz.Actor, uuid.UUID) (*Attachment, error)
	ListWorkMessages(context.Context, authz.Actor, uuid.UUID, string, string, models.Page) ([]*models.Message, int, error)
	MutateWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID, string) error
	ListSubmissions(context.Context, authz.Actor, models.Page) ([]Submission, int, error)
	GetSubmission(context.Context, authz.Actor, uuid.UUID) (*Submission, error)
	ListMailboxEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]MailEvent, int64, error)
	ListRecoveryReceipts(context.Context, authz.Actor, models.Page) ([]RecoveryReceipt, int, error)
	InspectRecoveryReceipt(context.Context, authz.Actor, uuid.UUID, string) (*RecoveryReceipt, error)
	RetryRecoveryReceipt(context.Context, authz.Actor, uuid.UUID, time.Time, []uuid.UUID, string, string) error
	ListOutboundRecipients(context.Context, uuid.UUID, uuid.UUID) ([]Recipient, error)
	ReconcileOutbound(context.Context, authz.Actor, uuid.UUID, time.Time, []Recipient, string) error
	Readiness(context.Context) error
}
