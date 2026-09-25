// Package templates owns rendering and distinguishes management preview from
// use of a published version. Database commands remain atomic in the repository.
package templates

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type Repository interface {
	company.TemplateAdminService
	company.TemplateSendReader
	GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*company.MailboxAccess, error)
	GetCompanySettings(context.Context, uuid.UUID) (*company.Settings, error)
}
type IdentityReader interface {
	GetUser(context.Context, uuid.UUID) (*models.User, error)
}
type Service struct {
	Repository
	identities IdentityReader
}

func New(repo Repository, identities IdentityReader) *Service { return &Service{repo, identities} }

type PreviewInput struct {
	Mailbox uuid.UUID              `json:"mailbox_id"`
	Version *uuid.UUID             `json:"template_version_id"`
	Draft   *company.TemplateDraft `json:"draft"`
	Vars    map[string]string      `json:"vars"`
}
type Rendered struct {
	Subject  string `json:"subject"`
	TextBody string `json:"text_body"`
	HTMLBody string `json:"html_body"`
}

func (s *Service) Preview(ctx context.Context, a authz.Actor, in PreviewInput) (*Rendered, error) {
	mb, e := s.GetWorkMailbox(ctx, a, in.Mailbox)
	if e != nil {
		return nil, e
	}
	var draft company.TemplateDraft
	var employee, name string
	if in.Draft != nil {
		if !mb.CanManage {
			return nil, app.Forbidden("template editing requires current administrator")
		}
		draft = *in.Draft
		u, e := s.identities.GetUser(ctx, a.ID)
		if e != nil {
			return nil, e
		}
		if u == nil || !u.IsActive || u.TenantID != a.TenantID {
			return nil, app.Forbidden("active employee required")
		}
		employee = u.DisplayName
		c, e := s.GetCompanySettings(ctx, a.TenantID)
		if e != nil {
			return nil, e
		}
		if c == nil {
			return nil, app.BadRequest("configure company first")
		}
		name = c.Name
	} else if in.Version != nil {
		version, emp, co, e := s.TemplateForSend(ctx, a.TenantID, &a.ID, nil, in.Mailbox, *in.Version)
		if e != nil {
			return nil, e
		}
		draft = version.Snapshot
		employee = emp
		name = co
	} else {
		return nil, app.BadRequest("draft or published version required")
	}
	subject, text, html, e := company.Render(draft, in.Vars, employee, name, mb.Mailbox.FullAddress)
	if e != nil {
		return nil, e
	}
	return &Rendered{subject, text, html}, nil
}
