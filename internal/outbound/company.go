package outbound

import (
	"context"

	"tabmail/internal/app"
	"tabmail/internal/enterprise"
)

func (s *Service) prepareCompanySend(ctx context.Context, req *SendRequest) (bool, error) {
	c, err := enterprise.Lookup(ctx, s.store, req.TenantID)
	if err != nil || c == nil {
		return false, err
	}
	if req.UserID == nil || req.APIKeyID != nil || req.ZoneID != c.PrimaryZoneID {
		return true, app.Forbidden("employee sender in the primary domain required")
	}
	repo := s.store.(enterprise.Repository)
	m, err := repo.GetCompanyMember(ctx, req.TenantID, *req.UserID)
	if err != nil {
		return true, err
	}
	address, err := enterprise.CanonicalAddress(req.From)
	if err != nil {
		return true, err
	}
	req.From = address
	mb, err := s.store.ForTenant(req.TenantID).GetMailboxByAddress(ctx, address)
	if err != nil {
		return true, err
	}
	if mb == nil || mb.ZoneID != c.PrimaryZoneID {
		return true, app.Forbidden("exact sender not provisioned")
	}
	g, err := repo.GetCompanyGrant(ctx, req.TenantID, *req.UserID, mb.ID)
	if err != nil {
		return true, err
	}
	if err = enterprise.CheckSender(m, g, req.TemplateID != nil); err != nil {
		return true, err
	}
	if len(req.Headers) > 0 {
		return true, app.BadRequest("custom headers are not supported for company mail")
	}
	for _, group := range [][]string{req.To, req.CC, req.BCC} {
		for i, a := range group {
			canonical, err := enterprise.CanonicalAddress(a)
			if err != nil {
				return true, err
			}
			group[i] = canonical
		}
	}
	if req.TemplateID != nil {
		if req.Subject != "" || req.TextBody != "" || req.HTMLBody != "" {
			return true, app.BadRequest("template sends accept variables, not client-rendered content")
		}
		t, err := repo.GetCompanyTemplate(ctx, req.TenantID, *req.TemplateID)
		if err != nil {
			return true, err
		}
		if t == nil || t.Status != "published" {
			return true, app.Forbidden("published template required")
		}
		bound := false
		for _, id := range t.MailboxIDs {
			bound = bound || id == mb.ID
		}
		if !bound {
			return true, app.Forbidden("template not authorized for this sender")
		}
		req.Subject, req.TextBody, req.HTMLBody, err = enterprise.Render(*t, req.Variables, m.DisplayName, c.Name)
		if err != nil {
			return true, err
		}
	} else if len(req.Variables) > 0 {
		return true, app.BadRequest("variables require a template")
	}
	return true, nil
}
