package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type sendIdentityStore interface {
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error)
	ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error)
	DeleteSendIdentity(ctx context.Context, id uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
}

// SendIdentityHandler serves the send identity CRUD endpoints.
type SendIdentityHandler struct {
	store  sendIdentityStore
	logger zerolog.Logger
}

// NewSendIdentityHandler creates a new SendIdentityHandler.
func NewSendIdentityHandler(st sendIdentityStore, logger zerolog.Logger) *SendIdentityHandler {
	return &SendIdentityHandler{
		store:  st,
		logger: logger.With().Str("handler", "send_identities").Logger(),
	}
}

// List handles GET /api/v1/send-identities — list all send identities for the tenant.
// Admins see all identities; regular users see only those in their AllowedZoneIDs.
func (h *SendIdentityHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	items, err := h.store.ListSendIdentities(ctx, tenant.ID)
	if err != nil {
		h.logger.Err(err).Msg("listing send identities")
		errInternal(w)
		return
	}

	// Filter by AllowedZoneIDs for non-admin callers.
	actor := authz.ActorFromContext(ctx)
	if !actor.IsSuperAdmin && !actor.IsAdmin {
		items = filterSendIdentitiesByZone(actor, items)
	}

	if items == nil {
		items = []*models.SendIdentity{}
	}
	ok(w, items)
}

// Create handles POST /api/v1/send-identities — create a new send identity.
// Only admin users can create send identities.
func (h *SendIdentityHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ZoneID  string `json:"zone_id"`
		Address string `json:"address"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	if body.ZoneID == "" || body.Address == "" {
		errBadRequest(w, "zone_id and address are required")
		return
	}

	zoneID, err := uuid.Parse(body.ZoneID)
	if err != nil {
		errBadRequest(w, "invalid zone_id")
		return
	}

	ctx := r.Context()
	actor := authz.ActorFromContext(ctx)
	if actor.TenantID == uuid.Nil {
		errForbidden(w, "authentication required")
		return
	}

	// Verify the zone exists and belongs to this tenant. Keep NotFound
	// semantics (info-hiding) rather than authorizing with a 403.
	zone, err := h.store.GetZone(ctx, zoneID)
	if err != nil {
		h.logger.Err(err).Msg("looking up zone for send identity")
		errInternal(w)
		return
	}
	if zone == nil || zone.TenantID != actor.TenantID {
		errNotFound(w, "zone not found")
		return
	}

	// Determine identity type from address.
	address := strings.TrimSpace(body.Address)
	identityType := models.SendIdentityExact
	if strings.HasPrefix(address, "*@") {
		identityType = models.SendIdentityDomainWildcard
	}

	si := &models.SendIdentity{
		TenantID:     actor.TenantID,
		ZoneID:       zoneID,
		Address:      address,
		IdentityType: identityType,
		Verified:     zone.IsVerified,
	}
	if err := h.store.CreateSendIdentity(ctx, si); err != nil {
		h.logger.Err(err).Msg("creating send identity")
		errBadRequest(w, "failed to create send identity: "+err.Error())
		return
	}

	created(w, si)
}

// Delete handles DELETE /api/v1/send-identities/{id} — delete a send identity.
// Only admin users can delete send identities.
func (h *SendIdentityHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	ctx := r.Context()
	actor := authz.ActorFromContext(ctx)
	if actor.TenantID == uuid.Nil {
		errForbidden(w, "authentication required")
		return
	}

	// Verify the identity exists and belongs to this tenant. Keep NotFound
	// semantics (info-hiding) rather than authorizing with a 403.
	si, err := h.store.GetSendIdentity(ctx, id)
	if err != nil {
		h.logger.Err(err).Msg("looking up send identity")
		errInternal(w)
		return
	}
	if si == nil || si.TenantID != actor.TenantID {
		errNotFound(w, "send identity not found")
		return
	}

	if err := h.store.DeleteSendIdentity(ctx, id); err != nil {
		h.logger.Err(err).Msg("deleting send identity")
		errInternal(w)
		return
	}

	noContent(w)
}

// filterSendIdentitiesByZone filters send identities based on the actor's
// allowed-zone list via the authz seam. ZoneAllowed treats admins and an
// absent/empty allowlist as all-zones-allowed, matching the previous inline
// filter (which was only invoked for non-admin callers).
func filterSendIdentitiesByZone(actor authz.Actor, items []*models.SendIdentity) []*models.SendIdentity {
	out := make([]*models.SendIdentity, 0, len(items))
	for _, si := range items {
		if authz.ZoneAllowed(actor, si.ZoneID) {
			out = append(out, si)
		}
	}
	return out
}
