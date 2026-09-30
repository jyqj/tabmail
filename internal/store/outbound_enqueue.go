package store

import (
	"context"
	"tabmail/internal/models"
)

// AtomicOutboundEnqueue binds the existing send policy to the enqueue
// transaction. The fenced reader must be used synchronously, without pool or
// network I/O. Draft consumption and required audit share the same commit.
type AtomicOutboundEnqueue interface {
	CreateOutboundJobAuthorized(context.Context, *models.OutboundJob, OutboundQuotaReservation, *DraftConsumption, OutboundEnqueueValidator) (bool, error)
}

// The current fenced policy may replace an early quota snapshot, including
// transitions between a finite limit and zero (unlimited). Retry has no new
// quota reservation and deliberately keeps its separate validator contract.
type OutboundEnqueueValidator func(context.Context, OutboundRetryReader, *models.OutboundJob, OutboundQuotaReservation) (OutboundQuotaReservation, error)
