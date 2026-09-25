// Package employees owns the public employee offboarding use case. Legacy
// one-step storage adapters are deliberately not part of this port.
package employees

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

type Service struct{ repo company.OffboardingPlanner }

func New(repo company.OffboardingPlanner) *Service { return &Service{repo: repo} }
func (s *Service) Preview(ctx context.Context, a authz.Actor, target, successor uuid.UUID, options company.OffboardingOptions, reason string) (*company.OffboardingPlan, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 || len(reason) > 1000 || target == uuid.Nil || successor == uuid.Nil || target == successor || target == a.ID {
		return nil, app.BadRequest("distinct employee/successor and an 8-1000 byte reason required")
	}
	if options.Drafts == "" {
		options.Drafts = "seal"
	}
	switch options.Drafts {
	case "seal", "transfer_owned", "discard":
	default:
		return nil, app.BadRequest("invalid draft disposition")
	}
	return s.repo.PreviewOffboarding(ctx, a, target, successor, options, reason)
}
func (s *Service) Execute(ctx context.Context, a authz.Actor, target, plan uuid.UUID) (*company.OffboardingPlan, error) {
	if plan == uuid.Nil {
		return nil, app.BadRequest("preview plan_id is required")
	}
	return s.repo.ExecuteOffboarding(ctx, a, target, plan)
}
