package submissions

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func (s *Service) outboundReceipt(ctx context.Context, tenant *models.Tenant, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	if tenant == nil {
		return nil, ErrOutboundJobAuthRequired
	}
	if a.TenantID != tenant.ID {
		return nil, ErrOutboundJobNotFound
	}
	v, err := s.store.GetOutboundReceipt(ctx, a, id, scope)
	if err != nil {
		if e, ok := app.As(app.FromAuthz(err)); ok && (e.Kind == app.KindForbidden || e.Kind == app.KindNotFound) {
			return nil, ErrOutboundJobNotFound
		}
		return nil, err
	}
	if v == nil || v.Job == nil {
		return nil, ErrOutboundJobNotFound
	}
	return v, nil
}

func (s *Service) OutboundReceiptView(ctx context.Context, tenant *models.Tenant, a authz.Actor, id uuid.UUID) (*models.OutboundJob, error) {
	v, err := s.outboundReceipt(ctx, tenant, a, id, "send:read")
	if err != nil {
		return nil, err
	}
	return RedactOutboundJobView(v.Job, v.ContentAllowed), nil
}

// Every row's visibility and content decision came from the same store read.
// There is no per-row I/O that can mix current credentials with an old page.
func (s *Service) ListOutboundReceiptViews(ctx context.Context, tenant *models.Tenant, a authz.Actor, page models.Page) ([]*models.OutboundJob, int, error) {
	if tenant == nil {
		return nil, 0, ErrOutboundJobAuthRequired
	}
	if a.TenantID != tenant.ID {
		return nil, 0, ErrOutboundJobNotFound
	}
	rows, total, err := s.store.ListOutboundReceipts(ctx, a, page)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*models.OutboundJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, RedactOutboundJobView(row.Job, row.ContentAllowed))
	}
	return out, total, nil
}

func (s *Service) OutboundAttemptViews(ctx context.Context, tenant *models.Tenant, a authz.Actor, id uuid.UUID) ([]*models.OutboundAttempt, error) {
	if _, err := s.outboundReceipt(ctx, tenant, a, id, "send:read"); err != nil {
		return nil, err
	}
	attempts, err := s.store.ListOutboundAttempts(ctx, id)
	if err != nil {
		return nil, err
	}
	// Reading attempts can block across expiry or revocation. Only this last
	// credential/receipt check may authorize the previously gathered diagnostics.
	receipt, err := s.outboundReceipt(ctx, tenant, a, id, "send:read")
	if err != nil {
		return nil, err
	}
	out := make([]*models.OutboundAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, RedactOutboundAttemptView(attempt, receipt.ContentAllowed))
	}
	return out, nil
}
