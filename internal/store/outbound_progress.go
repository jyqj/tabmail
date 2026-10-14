package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/company"
)

var ErrOutboundUncertain = errors.New("outbound acceptance uncertain; operator reconciliation required")
var ErrOutboundNotRetryable = errors.New("outbound job is not safely retryable")

// ErrDeliveryTokenMismatch is shared by all adapters and survives wrapping.
var ErrDeliveryTokenMismatch = errors.New("delivery token mismatch: job was re-claimed")

// OutboundRecipientLedger is the mandatory durable checkpoint role. The
// retired domain checkpoint interface is deliberately not an optional fallback.
type OutboundRecipientLedger interface {
	ListOutboundRecipients(context.Context, uuid.UUID, uuid.UUID) ([]company.Recipient, error)
	BeginOutboundRecipient(context.Context, uuid.UUID, *uuid.UUID, string) (bool, error)
	CompleteOutboundRecipient(context.Context, uuid.UUID, *uuid.UUID, string, string, int, string) error
}
