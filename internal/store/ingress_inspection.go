package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// IngressInspection is an operator snapshot, not a raw-message or employee API.
// ExpectedBytes describes accepted metadata, not an object-store health probe.
type IngressInspection struct {
	ID               uuid.UUID       `json:"id"`
	State            string          `json:"state"`
	RecoveryManaged  bool            `json:"recovery_managed"`
	Attempts         int             `json:"attempts"`
	LastError        string          `json:"last_error"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	NextAttemptAt    time.Time       `json:"next_attempt_at"`
	ExpectedBytes    *int64          `json:"expected_bytes"`
	CanRetry         bool            `json:"can_retry"`
	RetryBlockReason string          `json:"retry_block_reason"`
	Targets          []IngressTarget `json:"targets"`
}

// Inspection and reviewed retry are additive. Existing ledger adapters and the
// reason-only recovery API keep their original contract.
type IngressInspector interface {
	InspectIngress(context.Context, uuid.UUID) (*IngressInspection, error)
	RetryReviewedIngress(context.Context, uuid.UUID, string, string, time.Time) error
}
