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

// DeriveSubmissionStatus maps the job and complete recipient ledger to a
// receipt status. Accepted means next-hop acceptance, not final delivery.
// Missing or malformed evidence always needs attention, even for an active or
// cancelled job. Nil is not a legacy provenance marker: historical jobs have
// been conservatively backfilled by the required recipient-ledger migration.
// A caller that observes in-flight ambiguity (job.InFlightDomain != "" with a
// non-processing state) appends "uncertain" to surface needs_attention.
func DeriveSubmissionStatus(state models.OutboundState, recipientStates []string) string {
	if len(recipientStates) == 0 {
		return SubmissionNeedsAttention
	}
	switch state {
	case models.OutboundPending, models.OutboundProcessing, models.OutboundRetry,
		models.OutboundSent, models.OutboundFailed, models.OutboundDead, models.OutboundCancelled:
	default:
		return SubmissionNeedsAttention
	}
	accepted, permanent := 0, false
	for _, recipientState := range recipientStates {
		switch recipientState {
		case Accepted:
			accepted++
		case Pending, Temporary:
		case Permanent:
			permanent = true
		default:
			// Uncertain and unknown states cannot prove acceptance or cancellation.
			return SubmissionNeedsAttention
		}
	}
	switch state {
	case models.OutboundProcessing:
		return SubmissionSending
	case models.OutboundPending:
		return SubmissionSubmitted
	}
	if accepted == len(recipientStates) {
		return SubmissionAccepted
	}
	if accepted > 0 {
		return SubmissionPartiallyAccepted
	}
	// Cancellation does not recall bytes already accepted by a next hop. Only
	// a valid ledger with no accepted targets supports an unqualified cancel.
	if state == models.OutboundCancelled {
		return SubmissionCancelled
	}
	if permanent {
		return SubmissionNeedsAttention
	}
	switch state {
	case models.OutboundSent, models.OutboundFailed, models.OutboundDead:
		return SubmissionNeedsAttention
	default:
		return SubmissionSending
	}
}
