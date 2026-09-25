package company

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"time"
)

type OffboardingOptions struct {
	// seal preserves private drafts without giving the successor their content.
	// transfer_owned transfers only drafts in mailboxes owned by the departing
	// employee. Others are sealed. discard explicitly deletes active drafts.
	Drafts string `json:"drafts"`
}
type OffboardingImpact struct {
	Mailboxes          int `json:"mailboxes"`
	Drafts             int `json:"drafts"`
	TransferableDrafts int `json:"transferable_drafts"`
	Attachments        int `json:"attachments"`
	APIKeys            int `json:"api_keys"`
	Grants             int `json:"grants"`
	Queued             int `json:"queued"`
	InFlight           int `json:"in_flight"`
	Uncertain          int `json:"uncertain"`
}
type OffboardingPlan struct {
	ID          uuid.UUID          `json:"id"`
	TargetID    uuid.UUID          `json:"target_id"`
	SuccessorID uuid.UUID          `json:"successor_id"`
	Options     OffboardingOptions `json:"options"`
	Impact      OffboardingImpact  `json:"impact"`
	Reason      string             `json:"reason"`
	State       string             `json:"state"`
	ExpiresAt   time.Time          `json:"expires_at"`
	ExecutedAt  *time.Time         `json:"executed_at,omitempty"`
}
type OffboardingPlanner interface {
	PreviewOffboarding(context.Context, authz.Actor, uuid.UUID, uuid.UUID, OffboardingOptions, string) (*OffboardingPlan, error)
	ExecuteOffboarding(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*OffboardingPlan, error)
}
