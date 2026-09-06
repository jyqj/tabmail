// Package enterprise defines the company boundary independently of HTTP roles.
// Mailbox read, mailbox organization, and exact-address send-as are independent.
package enterprise

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

type Company struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	PrimaryZoneID uuid.UUID `json:"primary_zone_id"`
	Domain        string    `json:"domain"`
	Name          string    `json:"name"`
	OwnerID       uuid.UUID `json:"owner_id"`
	CreatedAt     time.Time `json:"created_at"`
}
type Member struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"company_role"`
	Active      bool      `json:"is_active"`
	DailyQuota  int       `json:"daily_send_quota"`
}
type Grant struct {
	MailboxID    uuid.UUID `json:"mailbox_id"`
	UserID       uuid.UUID `json:"user_id"`
	Address      string    `json:"address"`
	Read         bool      `json:"can_read"`
	Organize     bool      `json:"can_organize"`
	Send         bool      `json:"can_send"`
	TemplateOnly bool      `json:"template_only"`
}
type Template struct {
	ID         uuid.UUID      `json:"id"`
	TenantID   uuid.UUID      `json:"tenant_id"`
	FamilyID   uuid.UUID      `json:"family_id"`
	Version    int            `json:"version"`
	Name       string         `json:"name"`
	Subject    string         `json:"subject"`
	TextBody   string         `json:"text_body"`
	HTMLBody   string         `json:"html_body"`
	Variables  map[string]int `json:"variables"` // required string -> maximum rune length
	MailboxIDs []uuid.UUID    `json:"mailbox_ids"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
}
type Invitation struct {
	Member    Member    `json:"member"`
	Token     string    `json:"activation_token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Provision struct {
	LocalPart   string `json:"local_part"`
	DisplayName string `json:"display_name"`
	Role        string `json:"company_role"`
	DailyQuota  int    `json:"daily_send_quota"`
}

type Reader interface {
	GetCompany(context.Context, uuid.UUID) (*Company, error)
	GetCompanyMember(context.Context, uuid.UUID, uuid.UUID) (*Member, error)
	GetCompanyGrant(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*Grant, error)
	ListCompanyMailboxes(context.Context, uuid.UUID, uuid.UUID, models.Page) ([]*models.Mailbox, int, error)
	GetCompanyTemplate(context.Context, uuid.UUID, uuid.UUID) (*Template, error)
}
type Repository interface {
	Reader
	EnableCompany(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (*Company, error)
	ListCompanyMembers(context.Context, uuid.UUID) ([]Member, error)
	ProvisionCompanyMember(context.Context, uuid.UUID, uuid.UUID, Provision, string) (*Member, error)
	ActivateCompanyMember(context.Context, string, string) error
	ReissueCompanyInvitation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error
	UpdateCompanyMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, bool, int) error
	CreateCompanyMailbox(context.Context, uuid.UUID, uuid.UUID, string) (*models.Mailbox, error)
	ListCompanyGrants(context.Context, uuid.UUID, uuid.UUID, bool) ([]Grant, error)
	SetCompanyGrant(context.Context, uuid.UUID, uuid.UUID, Grant) error
	ListCompanyTemplates(context.Context, uuid.UUID, uuid.UUID, bool) ([]Template, error)
	SaveCompanyTemplate(context.Context, uuid.UUID, uuid.UUID, Template) (*Template, error)
	SetCompanyTemplateStatus(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error
	CreateCompanyJob(context.Context, *models.OutboundJob, *uuid.UUID) error
	ValidateCompanyJob(context.Context, *models.OutboundJob) error
}

// Lookup allows legacy-only test doubles/adapters to remain usable. Production's
// PgStore is compile-time asserted to implement Repository. Never cache this result.
func Lookup(ctx context.Context, st any, tenant uuid.UUID) (*Company, error) {
	r, ok := st.(Reader)
	if !ok {
		return nil, nil
	}
	return r.GetCompany(ctx, tenant)
}

func CheckMailbox(ctx context.Context, st any, tenant uuid.UUID, user *uuid.UUID, mailbox uuid.UUID, write bool) (bool, error) {
	c, err := Lookup(ctx, st, tenant)
	if err != nil || c == nil {
		return false, err
	}
	if user == nil {
		return true, app.Forbidden("company mailbox requires an employee session")
	}
	r := st.(Reader)
	m, err := r.GetCompanyMember(ctx, tenant, *user)
	if err != nil {
		return true, err
	}
	if m == nil || !m.Active || !ValidRole(m.Role) {
		return true, app.Forbidden("employee is not active")
	}
	g, err := r.GetCompanyGrant(ctx, tenant, *user, mailbox)
	if err != nil {
		return true, err
	}
	if g == nil || !g.Read || (write && (!g.Organize || m.Role == "viewer")) {
		return true, app.Forbidden("mailbox permission required")
	}
	return true, nil
}

func CheckSender(m *Member, g *Grant, hasTemplate bool) error {
	if m == nil || !m.Active || !ValidRole(m.Role) || g == nil || !g.Send || m.Role == "viewer" {
		return app.Forbidden("exact sender permission required")
	}
	if (m.Role == "restricted" || g.TemplateOnly) && !hasTemplate {
		return app.Forbidden("published template required")
	}
	return nil
}

var localPart = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func ValidLocalPart(s string) bool {
	return localPart.MatchString(s) && !strings.Contains(s, "..") && !strings.HasSuffix(s, ".")
}
func ValidRole(s string) bool {
	return s == "admin" || s == "employee" || s == "restricted" || s == "viewer"
}

// CanonicalAddress intentionally accepts only addr-spec, not display-name syntax.
// All authorization and SMTP envelope decisions use the same representation.
func CanonicalAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || strings.ContainsAny(s, "\r\n") || len(s) > 254 {
		return "", app.BadRequest("use a plain email address")
	}
	at := strings.LastIndexByte(a.Address, '@')
	return a.Address[:at+1] + strings.ToLower(a.Address[at+1:]), nil
}
func HashToken(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// JobDigest binds a stored submission to its precise content and recipients.
// Worker-generated transport metadata is deliberately excluded.
func JobDigest(j *models.OutboundJob) string {
	var headers any
	_ = json.Unmarshal(j.HeadersJSON, &headers)
	b, _ := json.Marshal(struct {
		Tenant              uuid.UUID
		User                *uuid.UUID
		Key                 *uuid.UUID
		Zone                uuid.UUID
		From                string
		To                  []string
		Subject, Text, HTML string
		Headers             any
	}{j.TenantID, j.UserID, j.APIKeyID, j.ZoneID, j.MailFrom, j.RcptTo, j.Subject, j.TextBody, j.HTMLBody, headers})
	return HashToken(string(b))
}

var ErrQuota = errors.New("company daily send quota exceeded")
