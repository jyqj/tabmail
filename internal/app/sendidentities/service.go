// Package sendidentitiesapp holds the send identity flows behind the
// /send-identities routes. The handlers in internal/api/handlers only decode
// requests and shape responses; the tenancy checks and identity-type rules
// live here.
package sendidentitiesapp

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type storeRepo interface {
	store.Transactor
	app.AuditStore
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error)
	ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error)
	DeleteSendIdentity(ctx context.Context, id uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
}

type Service struct {
	store      storeRepo
	dispatcher *hooks.Dispatcher
	logger     zerolog.Logger
}

func NewService(s storeRepo, dispatcher *hooks.Dispatcher, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, logger: logger.With().Str("service", "send_identities").Logger()}
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

// Create registers a send identity on one of the tenant's zones. Send identity
// mutation is an administrative operation because the identity becomes an
// authorization boundary for outbound mail. Exact and wildcard addresses are
// canonicalized and must belong to the selected zone.
func (s *Service) Create(ctx context.Context, actor authz.Actor, req CreateRequest) (*models.SendIdentity, error) {
	if actor.TenantID == uuid.Nil {
		return nil, app.Forbidden("authentication required")
	}
	if !actor.IsSuperAdmin && !actor.IsAdmin {
		return nil, app.Forbidden("send identity management requires admin access")
	}
	zone, err := s.store.GetZone(ctx, req.ZoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil || zone.TenantID != actor.TenantID {
		return nil, app.NotFound("zone not found")
	}

	address, identityType, domain, err := normalizeIdentityAddress(req.Address)
	if err != nil {
		return nil, app.BadRequest(err.Error())
	}
	if domain != normalizeDomain(zone.Domain) {
		return nil, app.BadRequest("send identity address must belong to the selected zone")
	}

	si := &models.SendIdentity{
		ID:           uuid.New(),
		TenantID:     actor.TenantID,
		ZoneID:       req.ZoneID,
		Address:      address,
		IdentityType: identityType,
		Verified:     zone.IsVerified && zone.MXVerified,
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(actor.TenantID), Actor: actor.AuditLabel(), Action: "send_identity.create", ResourceType: "send_identity", ResourceID: app.UUIDPtr(si.ID), Details: app.MustJSON(map[string]any{"zone_id": si.ZoneID, "address": si.Address, "identity_type": si.IdentityType, "verified": si.Verified})}
	event := hooks.Event{Type: "send_identity.created", TenantID: actor.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"send_identity_id": si.ID.String(), "zone_id": si.ZoneID.String(), "address": si.Address, "identity_type": si.IdentityType, "verified": si.Verified}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		if err := s.store.CreateSendIdentity(txCtx, si); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "duplicate") || strings.Contains(lower, "unique") || strings.Contains(lower, "23505") {
				return app.Conflict("send identity already exists")
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return si, nil
}

// Delete removes a send identity owned by the caller's tenant. Like creation,
// deletion is administrative because it changes who can submit outbound mail.
func (s *Service) Delete(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	if actor.TenantID == uuid.Nil {
		return app.Forbidden("authentication required")
	}
	if !actor.IsSuperAdmin && !actor.IsAdmin {
		return app.Forbidden("send identity management requires admin access")
	}
	si, err := s.store.GetSendIdentity(ctx, id)
	if err != nil {
		return app.Internal(err)
	}
	if si == nil || si.TenantID != actor.TenantID {
		return app.NotFound("send identity not found")
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(si.TenantID), Actor: actor.AuditLabel(), Action: "send_identity.delete", ResourceType: "send_identity", ResourceID: app.UUIDPtr(si.ID), Details: app.MustJSON(map[string]any{"zone_id": si.ZoneID, "address": si.Address, "identity_type": si.IdentityType})}
	event := hooks.Event{Type: "send_identity.deleted", TenantID: si.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"send_identity_id": si.ID.String(), "zone_id": si.ZoneID.String(), "address": si.Address, "identity_type": si.IdentityType}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeleteSendIdentity(txCtx, id)
	})
}

func normalizeIdentityAddress(raw string) (string, models.SendIdentityType, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errors.New("address is required")
	}
	if strings.HasPrefix(raw, "*@") {
		domain := normalizeDomain(strings.TrimPrefix(raw, "*@"))
		if !validMailboxDomain(domain) {
			return "", "", "", errors.New("invalid wildcard send identity")
		}
		return "*@" + domain, models.SendIdentityDomainWildcard, domain, nil
	}

	parsed, err := mail.ParseAddress(raw)
	if err != nil || strings.TrimSpace(parsed.Address) == "" {
		return "", "", "", errors.New("invalid send identity address")
	}
	address := strings.ToLower(strings.TrimSpace(parsed.Address))
	idx := strings.LastIndex(address, "@")
	if idx <= 0 || idx == len(address)-1 {
		return "", "", "", errors.New("invalid send identity address")
	}
	domain := normalizeDomain(address[idx+1:])
	address = address[:idx+1] + domain
	return address, models.SendIdentityExact, domain, nil
}

func normalizeDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func validMailboxDomain(domain string) bool {
	if domain == "" || strings.ContainsAny(domain, " *@<>") {
		return false
	}
	parsed, err := mail.ParseAddress("probe@" + domain)
	return err == nil && strings.EqualFold(parsed.Address, "probe@"+domain)
}

// filterByAllowedZones narrows identities through the authz seam. ZoneAllowed
// treats admins and an absent/empty allowlist as all-zones-allowed.
func filterByAllowedZones(actor authz.Actor, items []*models.SendIdentity) []*models.SendIdentity {
	out := make([]*models.SendIdentity, 0, len(items))
	for _, si := range items {
		if authz.ZoneAllowed(actor, si.ZoneID) {
			out = append(out, si)
		}
	}
	return out
}
