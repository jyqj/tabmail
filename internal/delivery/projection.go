package delivery

import "tabmail/internal/models"

// User-facing submission statuses (the Status field above).
const (
	SubmissionSubmitted         = "submitted"
	SubmissionCancelled         = "cancelled"
	SubmissionWaiting           = "waiting"
	SubmissionSending           = "sending"
	SubmissionPartiallyAccepted = "partially_accepted"
	SubmissionAccepted          = "accepted"
	SubmissionNeedsAttention    = "needs_attention"
)

// DeriveSubmissionStatus maps (job.state, per-recipient ledger states) onto a
// user-facing status. Precedence: uncertainty beats everything, then active
// sending, then queued, then the ledger outcome. Legacy jobs without a ledger
// fall back to the job state alone. A caller that observes in-flight ambiguity
// (job.InFlightDomain != "" with a non-processing state) appends "uncertain"
// to the ledger so the ambiguity surfaces as needs_attention.
func DeriveSubmissionStatus(state models.OutboundState, recipientStates []string) string {
	for _, s := range recipientStates {
		if s == Uncertain {
			return SubmissionNeedsAttention
		}
	}
	switch state {
	case models.OutboundCancelled:
		return SubmissionCancelled
	case models.OutboundProcessing:
		return SubmissionSending
	case models.OutboundPending:
		return SubmissionSubmitted
	}
	if len(recipientStates) == 0 {
		switch state {
		case models.OutboundSent:
			return SubmissionAccepted
		case models.OutboundRetry:
			return SubmissionSending
		default:
			return SubmissionNeedsAttention
		}
	}
	accepted := 0
	for _, s := range recipientStates {
		if s == Accepted {
			accepted++
		}
	}
	if accepted == len(recipientStates) {
		return SubmissionAccepted
	}
	if accepted == 0 {
		for _, s := range recipientStates {
			if s == Permanent {
				return SubmissionNeedsAttention
			}
		}
		switch state {
		case models.OutboundSent, models.OutboundFailed, models.OutboundDead:
			return SubmissionNeedsAttention
		default:
			return SubmissionSending
		}
	}
	return SubmissionPartiallyAccepted
}
