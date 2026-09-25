package company

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"time"
)

type Overview struct {
	Mailboxes          int `json:"mailboxes"`
	ActiveEmployees    int `json:"active_employees"`
	PendingInvitations int `json:"pending_invitations"`
	Queued             int `json:"queued"`
	Uncertain          int `json:"uncertain"`
	IndexFailed        int `json:"index_failed"`
}
type AdminAudit struct {
	ID           uuid.UUID  `json:"id"`
	Actor        string     `json:"actor"`
	Action       string     `json:"action"`
	ResourceType string     `json:"resource_type"`
	ResourceID   *uuid.UUID `json:"resource_id,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
type ConsoleReader interface {
	ExplainMailboxAccess(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*AccessExplanation, error)
	CompanyOverview(context.Context, authz.Actor) (*Overview, error)
	ListCompanyAudit(context.Context, authz.Actor, models.Page) ([]AdminAudit, int, error)
}

// AccessExplanation is an administrative projection, never an authorization
// credential. Execution still uses the normal mailbox authorization seam.
type AccessExplanation struct {
	MailboxID    uuid.UUID `json:"mailbox_id"`
	UserID       uuid.UUID `json:"user_id"`
	Source       string    `json:"source"`
	Active       bool      `json:"active"`
	CanRead      bool      `json:"can_read"`
	CanOrganize  bool      `json:"can_organize"`
	CanSend      bool      `json:"can_send"`
	TemplateOnly bool      `json:"template_only"`
	SendPolicy   string    `json:"send_policy"`
	Reasons      []string  `json:"reasons"`
}
