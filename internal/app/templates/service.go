// Package templates owns rendering and distinguishes management preview from
// use of a published version. Database commands remain atomic in the repository.
package templates

import (
	"context"
	"errors"

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
	if in.Version != nil {
		id := *in.Version
		in.Version = &id
	}
	source, e := s.previewSource(ctx, a, in)
	if e != nil {
		return nil, e
	}
	identity := source.identity
	subject, text, html, e := company.Render(source.draft, in.Vars, identity.employee, identity.companyName, identity.address)
	if e = errors.Join(e, ctx.Err()); e != nil {
		return nil, e
	}
	// Match submission's final content requirement after expansion and HTML
	// sanitization. Generic Render still allows optional fields to be omitted.
	if subject == "" {
		return nil, app.BadRequest("subject is required")
	}
	if text == "" && html == "" {
		return nil, app.BadRequest("text_body or html_body required")
	}
	// Re-read the same authority and rendering inputs before releasing bytes.
	// This is a finite pre-release check, not a lock across rendering or a
	// promise to revoke a result after it has already been returned.
	current, e := s.previewSource(ctx, a, in)
	if e != nil {
		return nil, e
	}
	if current.identity != identity {
		return nil, app.Conflict("template preview sources changed; reload")
	}
	if in.Draft != nil {
		// The final identity/settings read can itself outlive management rights
		// or a mailbox rename. Finish on the current mailbox authority, with no
		// further I/O before release. Published lookup already ends on its
		// repository's combined current send/grant decision.
		mailbox, e := s.previewMailbox(ctx, a, in.Mailbox)
		if e != nil {
			return nil, e
		}
		if !mailbox.CanManage {
			return nil, app.Forbidden("template editing requires current administrator")
		}
		if mailbox.Mailbox.FullAddress != identity.address {
			return nil, app.Conflict("template preview sources changed; reload")
		}
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return &Rendered{subject, text, html}, nil
}

type previewIdentity struct {
	mailbox, tenant, template, version uuid.UUID
	versionNumber                      int
	address, employee, companyName     string
	contentHash                        string
}

type previewSource struct {
	draft    company.TemplateDraft
	identity previewIdentity
}

func (s *Service) previewSource(ctx context.Context, a authz.Actor, in PreviewInput) (*previewSource, error) {
	mb, e := s.previewMailbox(ctx, a, in.Mailbox)
	if e != nil {
		return nil, e
	}
	source := &previewSource{identity: previewIdentity{mailbox: mb.Mailbox.ID, tenant: mb.Mailbox.TenantID, address: mb.Mailbox.FullAddress}}
	if in.Draft != nil {
		// Management visibility deliberately does not imply or require content
		// read/send rights. The repository refreshes the current administrator.
		if !mb.CanManage {
			return nil, app.Forbidden("template editing requires current administrator")
		}
		if s.identities == nil {
			return nil, app.Internal(errors.New("template identity reader unavailable"))
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		u, e := s.identities.GetUser(ctx, a.ID)
		if e = errors.Join(e, ctx.Err()); e != nil {
			return nil, e
		}
		// Match the repository's current-member rule: a current super admin
		// may manage a selected company outside their home tenant. Stored role
		// and session version, not cached administrator flags, decide eligibility.
		if _, current := authz.RefreshMemberActor(a, a.TenantID, u); !current {
			return nil, app.Forbidden("active employee required")
		}
		source.identity.employee = u.DisplayName
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		c, e := s.GetCompanySettings(ctx, a.TenantID)
		if e = errors.Join(e, ctx.Err()); e != nil {
			return nil, e
		}
		if c == nil {
			return nil, app.BadRequest("configure company first")
		}
		if c.TenantID != a.TenantID {
			return nil, app.Forbidden("company settings unavailable in this company")
		}
		source.identity.companyName, source.draft = c.Name, *in.Draft
	} else if in.Version != nil {
		if !mb.CanSend {
			return nil, app.Forbidden("mailbox send permission required")
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		version, emp, co, e := s.TemplateForSend(ctx, a.TenantID, &a.ID, nil, in.Mailbox, *in.Version)
		if e = errors.Join(e, ctx.Err()); e != nil {
			return nil, e
		}
		if version == nil || version.ID != *in.Version || version.RevokedAt != nil {
			return nil, app.Forbidden("published template unavailable or changed")
		}
		hash := company.Digest(version.Snapshot)
		if hash != version.ContentHash {
			return nil, app.Forbidden("published template integrity mismatch")
		}
		source.draft = version.Snapshot
		source.identity.template, source.identity.version = version.TemplateID, version.ID
		source.identity.versionNumber, source.identity.contentHash = version.Version, hash
		source.identity.employee, source.identity.companyName = emp, co
	} else {
		return nil, app.BadRequest("draft or published version required")
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return source, nil
}

func (s *Service) previewMailbox(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	mailbox, e := s.GetWorkMailbox(ctx, a, id)
	if e = errors.Join(e, ctx.Err()); e != nil {
		return nil, e
	}
	if mailbox == nil || mailbox.Mailbox.ID != id || mailbox.Mailbox.TenantID != a.TenantID {
		return nil, app.NotFound("mailbox not found")
	}
	return mailbox, nil
}
