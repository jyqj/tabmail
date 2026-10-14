package outbound

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// Repository is the outbound consumer's role port, not the all-purpose
// store.Store assembly interface. Recipient checkpoints are mandatory: a
// missing ledger adapter must be a compile-time error, never a legacy branch.
// Every submission uses the fenced enqueue transaction; draft consumption is
// an optional command argument, never a fallback to unvalidated creation.
// Idempotent lookup remains explicit for consumers that use command keys.
type Repository interface {
	store.AtomicOutboundEnqueue
	SendAddressStore
	store.OutboundRecipientLedger
	outboundClaimMark
	GetZone(context.Context, uuid.UUID) (*models.DomainZone, error)
	GetUser(context.Context, uuid.UUID) (*models.User, error)
	GetAPIKey(context.Context, uuid.UUID) (*models.TenantAPIKey, error)
	EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error)
	CreateOutboundJob(context.Context, *models.OutboundJob) error
	CreateOutboundJobWithQuota(context.Context, *models.OutboundJob, store.OutboundQuotaReservation) error
	CreateOutboundAttempt(context.Context, *models.OutboundAttempt) error
	MarkOutboundJobSent(context.Context, uuid.UUID, *uuid.UUID, int, string, string) error
	IsSuppressed(context.Context, uuid.UUID, string) (bool, error)
}
