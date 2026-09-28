package submissions

import (
	"context"
	"slices"

	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Restricted-view placeholder copy. These are the single authoritative
// strings: handlers must not re-derive their own wording, or two endpoints
// could describe the same redaction differently.
const (
	// RestrictedJobError replaces a job's LastError when the viewer lacks
	// content authority; status and SMTP code stay visible.
	RestrictedJobError = "Delivery details restricted; inspect status and SMTP code"
	// RestrictedProtocolResponse replaces SMTP protocol transcripts on jobs.
	RestrictedProtocolResponse = "Protocol response restricted"
	// RestrictedAttemptError replaces a delivery attempt's diagnostic text.
	RestrictedAttemptError = "Delivery details restricted"
	// RestrictedRecipientDiagnostic replaces a recipient ledger diagnostic.
	RestrictedRecipientDiagnostic = RestrictedAttemptError
)

// StripJobSecrets copies job with the delivery claim token and raw MIME
// payload removed. It carries no visibility policy — both authorized and
// restricted views must lose these fields — so the break-glass inspect path
// shares the same copy rule as the redacted view. A nil job stays nil.
func StripJobSecrets(job *models.OutboundJob) *models.OutboundJob {
	if job == nil {
		return nil
	}
	cp := *job
	cp.To = slices.Clone(job.To)
	cp.CC = slices.Clone(job.CC)
	cp.BCC = slices.Clone(job.BCC)
	cp.RcptTo = slices.Clone(job.RcptTo)
	cp.HeadersJSON = slices.Clone(job.HeadersJSON)
	cp.AttachmentIDs = slices.Clone(job.AttachmentIDs)
	cp.DeliveredDomains = slices.Clone(job.DeliveredDomains)
	cp.DeliveryToken = nil
	cp.RawMIME = nil
	return &cp
}

// RedactOutboundJobView is the pure projection behind
// Service.RedactOutboundJob: contentAllowed is the caller's resolved
// ContentAllowed decision and the only policy input. Copy before redacting:
// cached/shared store objects and delivery state must not be mutated by
// presentation. RcptTo contains BCC; protocol errors may echo it, so a
// restricted view's RcptTo becomes To+CC only and protocol text is replaced
// with the placeholder copy. A nil job yields a nil view (fail closed:
// nothing is emitted).
func RedactOutboundJobView(job *models.OutboundJob, contentAllowed bool) *models.OutboundJob {
	if job == nil {
		return nil
	}
	cp := StripJobSecrets(job)
	cp.ContentRedacted = !contentAllowed
	cp.DeliveryUncertain = job.InFlightDomain != "" && job.State != models.OutboundProcessing
	if !contentAllowed {
		cp.TextBody = ""
		cp.HTMLBody = ""
		cp.BCC = nil
		cp.HeadersJSON = nil
		cp.AttachmentIDs = nil
		cp.InFlightDomain = ""
		cp.DeliveredDomains = []string{} // historical progress may reveal BCC-only domains
		cp.RcptTo = append(append([]string{}, job.To...), job.CC...)
		if cp.LastError != "" {
			cp.LastError = RestrictedJobError
		}
		if cp.SMTPResponse != "" {
			cp.SMTPResponse = RestrictedProtocolResponse
		}
	}
	return cp
}

// RedactOutboundAttemptView is the pure projection for one delivery attempt.
// A restricted view must not echo unreadable diagnostics or protocol
// transcripts; non-empty text is replaced with the placeholder copy. A nil
// attempt yields nil (nothing to leak, nothing emitted).
func RedactOutboundAttemptView(a *models.OutboundAttempt, contentAllowed bool) *models.OutboundAttempt {
	if a == nil {
		return nil
	}
	cp := *a
	if !contentAllowed {
		cp.RemoteHost = "" // an MX/relay target can identify an unreadable recipient domain
		if cp.Error != "" {
			cp.Error = RestrictedAttemptError
		}
		if cp.SMTPResponse != "" {
			cp.SMTPResponse = RestrictedProtocolResponse
		}
	}
	return &cp
}

// FilterRecipientsForJobView projects the recipient ledger rows through a job
// view built by RedactOutboundJobView. An authorized view returns the rows
// untouched; a restricted view keeps only the addresses the job view still
// exposes (To+CC, BCC hidden) and replaces each diagnostic with the
// placeholder copy. A nil view fails closed: no rows are emitted, so a
// missing decision can never widen visibility.
func FilterRecipientsForJobView(view *models.OutboundJob, rows []company.Recipient) []company.Recipient {
	if view == nil {
		return []company.Recipient{}
	}
	if !view.ContentRedacted {
		return append([]company.Recipient{}, rows...)
	}
	visible := map[string]bool{}
	for _, a := range view.RcptTo {
		visible[a] = true
	}
	filtered := []company.Recipient{}
	for _, row := range rows {
		if visible[row.Address] {
			row.Diagnostic = RestrictedRecipientDiagnostic
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// RedactOutboundAttempts resolves content authority once via ContentAllowed
// (the single decision seam) and returns the attempt views. The input slice
// and its elements are not mutated.
func (s *Service) RedactOutboundAttempts(ctx context.Context, actor authz.Actor, job *models.OutboundJob, attempts []*models.OutboundAttempt) ([]*models.OutboundAttempt, error) {
	allowed, err := s.ContentAllowed(ctx, actor, job)
	if err != nil {
		return nil, err
	}
	out := make([]*models.OutboundAttempt, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, RedactOutboundAttemptView(a, allowed))
	}
	return out, nil
}
