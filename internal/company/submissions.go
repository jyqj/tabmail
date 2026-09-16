package company

import (
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// SubmissionRecipient is the per-address outcome as recorded in the durable
// outbound_recipients ledger.
type SubmissionRecipient struct {
	Address string `json:"address"`
	State   string `json:"state"`
}

// Submission is the employee-facing projection of an outbound submission.
// It deliberately excludes every queue-internal field — attempts, leases,
// SMTP responses, delivery tokens — which remain the recovery/operations
// surface only.
type Submission struct {
	ID                uuid.UUID             `json:"id"`
	MailboxID         uuid.UUID             `json:"mailbox_id"`
	MailFrom          string                `json:"from"`
	Subject           string                `json:"subject"`
	Recipients        []SubmissionRecipient `json:"recipients"`
	Status            string                `json:"status"`
	TemplateVersionID *uuid.UUID            `json:"template_version_id,omitempty"`
	// DraftConsumed reports that this submission originated from a consumed
	// mail draft (provenance only; the draft id itself is not exposed).
	DraftConsumed   bool      `json:"draft_consumed"`
	AttachmentCount int       `json:"attachment_count"`
	CreatedAt       time.Time `json:"created_at"`
	// ContentRedacted is always false here: the projection exposes no message
	// content at all, so there is nothing to redact. The field keeps the
	// submission view contract-aligned with the outbound job view.
	ContentRedacted   bool `json:"content_redacted"`
	DeliveryUncertain bool `json:"delivery_uncertain"`
}

// User-facing submission statuses (the Status field above).
const (
	SubmissionSubmitted         = "submitted"
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
		if s == "uncertain" {
			return SubmissionNeedsAttention
		}
	}
	switch state {
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
		if s == "accepted" {
			accepted++
		}
	}
	if accepted == len(recipientStates) {
		return SubmissionAccepted
	}
	if accepted == 0 {
		for _, s := range recipientStates {
			if s == "permanent" {
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
