package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

var ErrOutboundUncertain = errors.New("outbound acceptance uncertain; operator reconciliation required")
var ErrOutboundNotRetryable = errors.New("outbound job is not safely retryable")

type OutboundProgress interface {
	BeginOutboundDomain(context.Context, uuid.UUID, *uuid.UUID, string) error
	CompleteOutboundDomain(context.Context, uuid.UUID, *uuid.UUID, string, bool) error
}
