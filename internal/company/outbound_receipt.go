package company

import (
	"time"

	"github.com/google/uuid"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
)

// OutboundReceipt is the only ordinary outbound-operation wire projection.
// It cannot carry a subject, sender/recipient address, body, headers, protocol
// diagnostic, lease, credential or storage reference. Read authority is a hint
// to the independent live-content endpoint, never a switch to expose raw jobs.
type OutboundReceipt struct {
	ID                uuid.UUID               `json:"id"`
	TenantID          *uuid.UUID              `json:"tenant_id,omitempty"`
	State             models.OutboundState    `json:"state"`
	Status            string                  `json:"status"`
	Progress          OutboundReceiptProgress `json:"progress"`
	CreatedAt         *time.Time              `json:"created_at,omitempty"`
	UpdatedAt         *time.Time              `json:"updated_at,omitempty"`
	AttemptCount      *int                    `json:"attempt_count,omitempty"`
	NextRetry         *time.Time              `json:"next_retry,omitempty"`
	DeliveryUncertain bool                    `json:"delivery_uncertain"`
	Capabilities      *SubmissionCapabilities `json:"capabilities,omitempty"`
}

type OutboundReceiptProgress struct {
	Completeness string                 `json:"completeness"`
	Counts       *OutboundReceiptCounts `json:"counts,omitempty"`
}

// Counts describe the COMPLETE ledger; no address/category filter is applied.
type OutboundReceiptCounts struct {
	Total     int `json:"total"`
	Accepted  int `json:"accepted"`
	Pending   int `json:"pending"`
	Temporary int `json:"temporary"`
	Permanent int `json:"permanent"`
	Uncertain int `json:"uncertain"`
}

// ProjectOutboundReceipt is projection, not authorization. The caller must
// supply an authorized snapshot. Unknown/missing/malformed outcome evidence
// never becomes a zero-recipient successful delivery.
func ProjectOutboundReceipt(job *models.OutboundJob, states []string, ledgerKnown bool) *OutboundReceipt {
	if job == nil {
		return nil
	}
	tenant, attempts := job.TenantID, job.Attempts
	out := &OutboundReceipt{ID: job.ID, TenantID: &tenant, State: receiptJobState(job.State), Status: delivery.SubmissionNeedsAttention, Progress: OutboundReceiptProgress{Completeness: "unknown"}}
	if !job.CreatedAt.IsZero() {
		created := job.CreatedAt
		out.CreatedAt = &created
	}
	if !job.UpdatedAt.IsZero() {
		updated := job.UpdatedAt
		out.UpdatedAt = &updated
	}
	if attempts >= 0 {
		out.AttemptCount = &attempts
	}
	if job.State == models.OutboundRetry && !job.NextAttemptAt.IsZero() {
		next := job.NextAttemptAt
		out.NextRetry = &next
	}
	counts := &OutboundReceiptCounts{Total: len(states)}
	known := ledgerKnown && job.RecipientLedger && len(states) > 0 && out.State != "unknown"
	for _, state := range states {
		switch state {
		case delivery.Accepted:
			counts.Accepted++
		case delivery.Pending:
			counts.Pending++
		case delivery.Temporary:
			counts.Temporary++
		case delivery.Permanent:
			counts.Permanent++
		case delivery.Uncertain:
			counts.Uncertain++
			out.DeliveryUncertain = true
		default:
			known = false
		}
	}
	statusStates := append([]string{}, states...)
	if job.InFlightDomain != "" && job.State != models.OutboundProcessing {
		out.DeliveryUncertain = true
		statusStates = append(statusStates, delivery.Uncertain)
	}
	if known {
		out.Progress = OutboundReceiptProgress{Completeness: "known", Counts: counts}
		out.Status = delivery.DeriveSubmissionStatus(job.State, statusStates)
	}
	return out
}

// CommittedOutboundReceiptFallback is only for an ALREADY committed command.
// A failed later display-authority read cannot report submission failure and
// encourage a duplicate send. No current tenant/ledger/capability is claimed.
func CommittedOutboundReceiptFallback(job *models.OutboundJob) *OutboundReceipt {
	if job == nil {
		return nil
	}
	return &OutboundReceipt{ID: job.ID, State: receiptJobState(job.State), Status: delivery.SubmissionNeedsAttention, Progress: OutboundReceiptProgress{Completeness: "unknown"}, Capabilities: &SubmissionCapabilities{RetryBlockReason: "unknown"}}
}

func receiptJobState(state models.OutboundState) models.OutboundState {
	switch state {
	case models.OutboundPending, models.OutboundProcessing, models.OutboundSent, models.OutboundRetry, models.OutboundFailed, models.OutboundDead, models.OutboundCancelled:
		return state
	default:
		return "unknown"
	}
}
