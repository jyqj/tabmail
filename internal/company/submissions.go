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
	// Capabilities carries interaction hints only (never authorization
	// credentials); it is omitted when the submissions engine is not wired.
	Capabilities *SubmissionCapabilities `json:"capabilities,omitempty"`
}

// SubmissionCapabilities is the interaction-hint block for a submission
// receipt. These fields express what the interface may offer; the backend
// re-runs the full authorization chain on every actual action, so a stale or
// degraded capability can never widen access. Any degraded computation yields
// an all-false block with RetryBlockReason "unknown" instead of an error.
type SubmissionCapabilities struct {
	// ViewContent mirrors the current content-read verdict; the content
	// endpoints re-check it server-side on every call.
	ViewContent bool `json:"view_content"`
	// Retry reports whether a retry POST is expected to be accepted today.
	Retry bool `json:"retry"`
	// RetryBlockReason is a coarse enum explaining a false Retry: empty,
	// "delivery_uncertain", "state_not_retryable", "sender_authority", or
	// "unknown" when the computation degraded.
	RetryBlockReason string `json:"retry_block_reason"`
}

// SubmissionContent is the sent-message body projection for a submission the
// actor may read. Recipients come from the structural To/CC columns — BCC is
// envelope-only and is never projected. Custom headers pass through the
// outbound safe-display filter, so stored-but-blocked header names (for
// example a caller-supplied "Bcc") never reach a viewer.
//
// ContentRedacted is false for successful content reads: current sender-mailbox
// read permission is required. Historical authors may retain a submission
// receipt after revocation, but content/list/download return the same 404 as
// an unknown submission. Administrator status is not a content bypass.
type SubmissionContent struct {
	ID              uuid.UUID         `json:"id"`
	Subject         string            `json:"subject"`
	MailFrom        string            `json:"from"`
	To              []string          `json:"to"`
	CC              []string          `json:"cc,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	TextBody        string            `json:"text_body,omitempty"`
	HTMLBody        string            `json:"html_body,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	ContentRedacted bool              `json:"content_redacted"`
}

// SubmissionAttachment is the metadata projection of an attachment pinned to a
// sent submission via outbound_attachments. ObjectKey and SHA256 never
// serialize (json:"-"): the storage key is internal and the download handler
// consumes them server-side only.
type SubmissionAttachment struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	State       string    `json:"state"`
	ObjectKey   string    `json:"-"`
	SHA256      string    `json:"-"`
}

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
		if s == "uncertain" {
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
