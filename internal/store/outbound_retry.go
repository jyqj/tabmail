package store

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// OutboundRetryReader is valid only during the retry transaction callback.
// Adapters protect all returned authorization dependencies until commit. The
// callback must be synchronous and must not use the outer store or network.
type OutboundRetryReader interface {
	authz.MailboxGrantReader
	ScopedStore
	GetUser(context.Context, uuid.UUID) (*models.User, error)
	GetAPIKey(context.Context, uuid.UUID) (*models.TenantAPIKey, error)
	GetZone(context.Context, uuid.UUID) (*models.DomainZone, error)
	EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error)
	FindSendIdentityForAddress(context.Context, uuid.UUID, string) (*models.SendIdentity, error)
	TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error)
}

// OutboundRetryValidator runs the SAME application policy against the adapter's
// fenced reader; a missing validator must fail closed. It may be invoked twice:
// before waiting on the job and again on the actual locked job generation.
type OutboundRetryValidator func(context.Context, OutboundRetryReader, *models.OutboundJob) error

type AtomicOutboundRetry interface {
	RequeueOutboundJobAuthorized(context.Context, authz.Actor, *models.OutboundJob, OutboundRetryValidator) (*models.OutboundJob, error)
}
