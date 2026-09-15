package outbound

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// Per-domain checkpointing prevents ordinary partial-domain retries from
// resending successful domains. It is NOT exactly-once SMTP. The pre-delivery
// marker deliberately survives ambiguous DATA/commit outcomes and process
// death; neither auto retries nor manual retry may replay those jobs blindly.
func (s *Service) deliverDomains(ctx context.Context, j *models.OutboundJob, token *uuid.UUID, mime []byte) error {
	grouped := groupByDomain(j.RcptTo)
	domains := make([]string, 0, len(grouped))
	for d := range grouped {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	done := map[string]bool{}
	for _, d := range j.DeliveredDomains {
		done[d] = true
	}
	var failures []error
	for _, domain := range domains {
		if done[domain] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.ValidateJobAuthorization(ctx, j); err != nil {
			if authz.IsAuthzError(err) {
				return s.store.MarkOutboundJobFailed(ctx, j.ID, token, "authorization revoked: "+err.Error(), false)
			}
			return err
		}
		if err := s.store.BeginOutboundDomain(ctx, j.ID, token, domain); err != nil {
			return err
		}
		attemptJob := *j
		attemptJob.RcptTo = grouped[domain]
		result, deliveryErr := s.adapter.Deliver(ctx, &attemptJob, mime)
		if result != nil {
			attempt := &models.OutboundAttempt{ID: uuid.New(), JobID: j.ID, TenantID: j.TenantID, Adapter: result.Adapter,
				Attempt: j.Attempts, SMTPCode: result.SMTPCode, SMTPResponse: result.SMTPResponse, RemoteHost: result.RemoteHost,
				StartedAt: result.StartedAt, FinishedAt: result.FinishedAt, Error: result.Error}
			if err := s.store.CreateOutboundAttempt(ctx, attempt); err != nil {
				s.logger.Warn().Err(err).Msg("recording delivery attempt")
			}
		}
		if errors.Is(deliveryErr, store.ErrOutboundUncertain) {
			return s.store.MarkOutboundJobFailed(ctx, j.ID, token, deliveryErr.Error(), false)
		}
		if err := s.store.CompleteOutboundDomain(ctx, j.ID, token, domain, deliveryErr == nil); err != nil {
			// Leave the marker in place. A later claimant holds, rather than resends,
			// if the remote acceptance checkpoint could not be persisted.
			return fmt.Errorf("%w: domain checkpoint: %v", store.ErrOutboundUncertain, err)
		}
		if deliveryErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", domain, deliveryErr))
			continue
		}
		j.DeliveredDomains = append(j.DeliveredDomains, domain)
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	if len(domains) == 0 {
		return errors.New("no deliverable recipient domains")
	}
	return s.store.MarkOutboundJobSent(ctx, j.ID, token, 250, "Accepted by next hop", j.MessageIDHeader)
}
