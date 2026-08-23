// Package sendidentitiesapp holds the send identity flows behind the
// /send-identities routes. The handlers in internal/api/handlers only decode
// requests and shape responses; the tenancy checks and identity-type rules
// live here.
package sendidentitiesapp

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type storeRepo interface {
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error)
	ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error)
	DeleteSendIdentity(ctx context.Context, id uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
}

type Service struct {
	store  storeRepo
	logger zerolog.Logger
}

func NewService(s storeRepo, logger zerolog.Logger) *Service {
	return &Service{store: s, logger: logger.With().Str("service", "send_identities").Logger()}
}

// List returns the tenant's send identities. Admins see all identities;
// other callers see only those in zones their permission allowlist covers.
func (s *Service) List(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) ([]*models.SendIdentity, error) {
	if tenantID == uuid.Nil {
		return nil, app.Forbidden("authentication required")
	}
	items, err := s.store.ListSendIdentities(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if !actor.IsSuperAdmin && !actor.IsAdmin {
		items = filterByAllowedZones(actor, items)
	}
	if items == nil {
		items = []*models.SendIdentity{}
	}
	return items, nil
}

// CreateRequest is the identity a caller wants to send as.
type CreateRequest struct {
	ZoneID  uuid.UUID
	Address string
}

// Create registers a send identity on one of the tenant's zones. An address
// starting with "*@" registers a domain wildcard, anything else an exact
// address. A zone in another tenant reports as missing rather than forbidden,
// so the route never confirms that a zone id exists elsewhere.
func (s *Service) Create(ctx context.Context, actor authz.Actor, req CreateRequest) (*models.SendIdentity, error) {
	if actor.TenantID == uuid.Nil {
		return nil, app.Forbidden("authentication required")
	}
	zone, err := s.store.GetZone(ctx, req.ZoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil || zone.TenantID != actor.TenantID {
		return nil, app.NotFound("zone not found")
	}

	address := strings.TrimSpace(req.Address)
	identityType := models.SendIdentityExact
	if strings.HasPrefix(address, "*@") {
		identityType = models.SendIdentityDomainWildcard
	}

	si := &models.SendIdentity{
		TenantID:     actor.TenantID,
		ZoneID:       req.ZoneID,
		Address:      address,
		IdentityType: identityType,
		Verified:     zone.IsVerified,
	}
	if err := s.store.CreateSendIdentity(ctx, si); err != nil {
		return nil, app.BadRequest("failed to create send identity: " + err.Error())
	}
	return si, nil
}

// Delete removes a send identity owned by the caller's tenant, keeping
// NotFound semantics for identities that live in other tenants.
func (s *Service) Delete(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	if actor.TenantID == uuid.Nil {
		return app.Forbidden("authentication required")
	}
	si, err := s.store.GetSendIdentity(ctx, id)
	if err != nil {
		return app.Internal(err)
	}
	if si == nil || si.TenantID != actor.TenantID {
		return app.NotFound("send identity not found")
	}
	if err := s.store.DeleteSendIdentity(ctx, id); err != nil {
		return app.Internal(err)
	}
	return nil
}

// filterByAllowedZones narrows identities through the authz seam. ZoneAllowed
// treats admins and an absent/empty allowlist as all-zones-allowed, matching
// the behavior the handler previously implemented inline.
func filterByAllowedZones(actor authz.Actor, items []*models.SendIdentity) []*models.SendIdentity {
	out := make([]*models.SendIdentity, 0, len(items))
	for _, si := range items {
		if authz.ZoneAllowed(actor, si.ZoneID) {
			out = append(out, si)
		}
	}
	return out
}
