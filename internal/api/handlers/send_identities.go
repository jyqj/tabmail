package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	sendidentitiesapp "tabmail/internal/app/sendidentities"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type sendIdentityStore interface {
	store.Transactor
	app.AuditStore
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error)
	ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error)
	DeleteSendIdentity(ctx context.Context, id uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
}

// SendIdentityHandler serves the send identity CRUD endpoints.
type SendIdentityHandler struct {
	service *sendidentitiesapp.Service
	logger  zerolog.Logger
}

// NewSendIdentityHandler creates a new SendIdentityHandler.
func NewSendIdentityHandler(st sendIdentityStore, dispatcher *hooks.Dispatcher, logger zerolog.Logger) *SendIdentityHandler {
	return &SendIdentityHandler{
		service: sendidentitiesapp.NewService(st, dispatcher, logger),
		logger:  logger.With().Str("handler", "send_identities").Logger(),
	}
}

// List handles GET /api/v1/send-identities.
func (h *SendIdentityHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var tenantID uuid.UUID
	if tenant := middleware.TenantFromCtx(ctx); tenant != nil {
		tenantID = tenant.ID
	}
	items, err := h.service.List(ctx, middleware.ActorFromContext(ctx), tenantID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

// Create handles POST /api/v1/send-identities.
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
	si, err := h.service.Create(ctx, middleware.ActorFromContext(ctx), sendidentitiesapp.CreateRequest{
		ZoneID:  zoneID,
		Address: body.Address,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, si)
}

// Delete handles DELETE /api/v1/send-identities/{id}.
func (h *SendIdentityHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	ctx := r.Context()
	if err := h.service.Delete(ctx, middleware.ActorFromContext(ctx), id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}
