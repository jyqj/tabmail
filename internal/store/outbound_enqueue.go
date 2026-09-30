package store

import (
	"context"
	"tabmail/internal/models"
)

// AtomicOutboundEnqueue binds the existing send policy to the enqueue
// transaction. The fenced reader must be used synchronously, without pool or
// network I/O. Draft consumption and required audit share the same commit.
type AtomicOutboundEnqueue interface {
	CreateOutboundJobAuthorized(context.Context, *models.OutboundJob, OutboundQuotaReservation, *DraftConsumption, OutboundRetryValidator) (bool, error)
}
