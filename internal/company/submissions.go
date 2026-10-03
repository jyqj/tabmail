package company

import (
	"time"

	"github.com/google/uuid"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
)

// SubmissionRecipient is the per-address outcome as recorded in the durable
// outbound_recipients ledger.
type SubmissionRecipient struct {
	Address string `json:"address"`
	State   string `json:"state"`
}

// Submission preserves the company receipt port name while sharing the strict
// ordinary-operation DTO. Content is available only through SubmissionContent.
type Submission = OutboundReceipt

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

// SubmissionContent is the current-readable durable sent-content projection.
// Structured BCC is exposed only here, never on the ordinary receipt. A
// complete snapshot includes known-empty BCC; legacy_unknown means the original
// structured recipients were not recoverable, not that BCC was empty. Custom
// headers still pass through the wire-safe display filter (including no Bcc).
// Current sender-mailbox read rights are mandatory; historical authors and
// administrative roles confer no bypass after revocation or lifecycle expiry.
type SubmissionContent struct {
	ID                    uuid.UUID         `json:"id"`
	Subject               string            `json:"subject"`
	MailFrom              string            `json:"from"`
	To                    []string          `json:"to"`
	CC                    []string          `json:"cc,omitempty"`
	BCC                   []string          `json:"bcc"`
	RecipientCompleteness string            `json:"recipient_completeness"`
	Headers               map[string]string `json:"headers,omitempty"`
	TextBody              string            `json:"text_body,omitempty"`
	HTMLBody              string            `json:"html_body,omitempty"`
	CreatedAt             time.Time         `json:"created_at"`
	ContentRedacted       bool              `json:"content_redacted"`
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

// Public company DTO aliases keep compatibility; delivery owns state policy.
const (
	SubmissionSubmitted         = delivery.SubmissionSubmitted
	SubmissionCancelled         = delivery.SubmissionCancelled
	SubmissionWaiting           = delivery.SubmissionWaiting
	SubmissionSending           = delivery.SubmissionSending
	SubmissionPartiallyAccepted = delivery.SubmissionPartiallyAccepted
	SubmissionAccepted          = delivery.SubmissionAccepted
	SubmissionNeedsAttention    = delivery.SubmissionNeedsAttention
)

func DeriveSubmissionStatus(state models.OutboundState, recipientStates []string) string {
	return delivery.DeriveSubmissionStatus(state, recipientStates)
}
