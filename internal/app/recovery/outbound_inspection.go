package recovery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/app/credentials"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// Service extends the existing recovery application boundary; persistence
// owns the authority fence, complete snapshot and necessary audit transaction.
type Service struct {
	inspector company.OutboundRecoveryInspector
}

func New(inspector company.OutboundRecoveryInspector) *Service { return &Service{inspector: inspector} }
func (s *Service) InspectOutboundRecovery(ctx context.Context, a authz.Actor, id uuid.UUID, reason string) (*company.OutboundInspection, error) {
	reason, e := credentials.AuditReason(reason)
	if e != nil {
		return nil, app.BadRequest("inspection reason must be 8-1000 bytes")
	}
	if id == uuid.Nil {
		return nil, app.BadRequest("outbound job ID required")
	}
	if s == nil || s.inspector == nil {
		return nil, app.Internal(errors.New("outbound recovery unavailable"))
	}
	v, e := s.inspector.InspectOutboundRecovery(ctx, a, id, reason)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, app.NotFound("send job not found")
	}
	return v, nil
}
