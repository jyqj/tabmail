package submissions

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

// ReplayReceiptView separates current command admission from content access.
// A revoked JWT/key must not become a successful redacted POST simply because
// a content predicate failed closed. Conversely, new-send policy changes do
// not undo a committed historical submission or require another send attempt.
func (s *Service) ReplayReceiptView(ctx context.Context, a authz.Actor, original *models.OutboundJob) (*company.OutboundReceipt, error) {
	if original == nil || original.TenantID != a.TenantID {
		return nil, app.Forbidden("submission replay authority unavailable")
	}
	var user, key *uuid.UUID
	switch a.Type {
	case authz.PrincipalUser:
		id := a.ID
		user = &id
	case authz.PrincipalAPIKey:
		id := a.ID
		key = &id
	default:
		return nil, app.Forbidden("current submission principal required")
	}
	identity := outbound.SubmissionActor(user, key)
	if identity == "" || original.SubmitActor != identity {
		return nil, app.Forbidden("submission belongs to another command principal")
	}
	receipt, err := s.store.GetOutboundReceipt(ctx, a, original.ID, "send:write")
	if err != nil {
		if v, ok := app.As(app.FromAuthz(err)); ok && (v.Kind == app.KindForbidden || v.Kind == app.KindNotFound) {
			return nil, app.Forbidden("submission replay authority no longer current")
		}
		return nil, err
	}
	if receipt == nil || receipt.Job == nil || receipt.Job.ID != original.ID || receipt.Job.SubmitActor != identity || receipt.Job.TenantID != a.TenantID {
		return nil, app.Forbidden("submission replay receipt unavailable")
	}
	return s.projectReceipt(ctx, a, receipt, true), nil
}

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
	if v == nil || v.Job == nil || v.Job.ID != id || v.Job.TenantID != a.TenantID {
		return nil, ErrOutboundJobNotFound
	}
	return v, nil
}

func (s *Service) OutboundReceiptView(ctx context.Context, tenant *models.Tenant, a authz.Actor, id uuid.UUID) (*company.OutboundReceipt, error) {
	v, err := s.outboundReceipt(ctx, tenant, a, id, "send:read")
	if err != nil {
		return nil, err
	}
	return s.projectReceipt(ctx, a, v, true), nil
}

// Every row's visibility and content decision came from the same store read.
// There is no per-row I/O that can mix current credentials with an old page.
func (s *Service) ListOutboundReceiptViews(ctx context.Context, tenant *models.Tenant, a authz.Actor, page models.Page) ([]*company.OutboundReceipt, int, error) {
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
	out := make([]*company.OutboundReceipt, 0, len(rows))
	for _, row := range rows {
		if row.Job == nil || row.Job.TenantID != a.TenantID {
			return nil, 0, app.Internal(errors.New("invalid authorized receipt snapshot"))
		}
		out = append(out, s.projectReceipt(ctx, a, &row, false))
	}
	return out, total, nil
}

// Ordinary attempt history exposes only the safe job-level attempt count and
// full-ledger aggregate. Protocol/remote-target/individual attempt rows belong
// to the independent controlled inspection surface, never to this receipt.
func (s *Service) OutboundAttemptViews(ctx context.Context, tenant *models.Tenant, a authz.Actor, id uuid.UUID) (*company.OutboundReceipt, error) {
	return s.OutboundReceiptView(ctx, tenant, a, id)
}

// CommittedReceiptView runs AFTER a successful fresh submit/retry commit.
// Failed presentation authority is not a failed command: return only a safe
// committed-ID fallback with unknown progress and no usable capabilities.
func (s *Service) CommittedReceiptView(ctx context.Context, a authz.Actor, job *models.OutboundJob) *company.OutboundReceipt {
	if job == nil {
		return nil
	}
	r, err := s.store.GetOutboundReceipt(ctx, a, job.ID, "send:write")
	if err != nil || r == nil || r.Job == nil || r.Job.ID != job.ID || r.Job.TenantID != a.TenantID {
		if err != nil {
			s.logger.Err(err).Str("job_id", job.ID.String()).Msg("command committed; receipt authority unavailable")
		}
		return company.CommittedOutboundReceiptFallback(job)
	}
	return s.projectReceipt(ctx, a, r, true)
}

func (s *Service) projectReceipt(ctx context.Context, a authz.Actor, r *store.OutboundReceipt, retryHint bool) *company.OutboundReceipt {
	if r == nil || r.Job == nil {
		return nil
	}
	view := company.ProjectOutboundReceipt(r.Job, r.RecipientStates, r.LedgerKnown)
	caps := &company.SubmissionCapabilities{ViewContent: r.ContentAllowed, RetryBlockReason: CapabilityUnknown}
	if retryHint {
		switch {
		case view.DeliveryUncertain || r.Job.InFlightDomain != "":
			caps.RetryBlockReason = CapabilityDeliveryUncertain
		case view.Progress.Completeness != "known":
			caps.RetryBlockReason = CapabilityUnknown
		case r.Job.State != models.OutboundDead && r.Job.State != models.OutboundFailed:
			caps.RetryBlockReason = CapabilityStateNotRetryable
		default:
			if s.outbound == nil {
				break
			}
			if err := s.RetryAuthority(ctx, a, r.Job); err != nil {
				if authz.IsAuthzError(err) {
					caps.RetryBlockReason = CapabilitySenderAuthority
				}
				break
			}
			// A receipt/read right cannot substitute for the original sender's
			// currently valid authority, which the actual retry also revalidates.
			if err := s.outbound.ValidateJobAuthorization(ctx, r.Job); err != nil {
				if errors.Is(err, store.ErrOutboundUncertain) {
					caps.RetryBlockReason = CapabilityDeliveryUncertain
				} else if authz.IsAuthzError(err) {
					caps.RetryBlockReason = CapabilitySenderAuthority
				}
				break
			}
			caps.Retry = true
			caps.RetryBlockReason = ""
		}
	}
	view.Capabilities = caps
	return view
}
