package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	webhooksapp "tabmail/internal/app/webhooks"
	"tabmail/internal/models"
)

type webhookEndpointStore interface {
	CreateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	ListWebhookEndpoints(ctx context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error)
	GetWebhookEndpoint(ctx context.Context, id uuid.UUID) (*models.WebhookEndpoint, error)
	UpdateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	DeleteWebhookEndpoint(ctx context.Context, id uuid.UUID) error
}

type WebhookEndpointHandler struct {
	service *webhooksapp.Service
	logger  zerolog.Logger
}

func NewWebhookEndpointHandler(s webhookEndpointStore, l zerolog.Logger) *WebhookEndpointHandler {
	return &WebhookEndpointHandler{
		service: webhooksapp.NewService(s, l),
		logger:  l.With().Str("handler", "webhook_endpoints").Logger(),
	}
}

func (h *WebhookEndpointHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), webhookCaller(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *WebhookEndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL        string   `json:"url"`
		Secret     string   `json:"secret"`
		EventTypes []string `json:"event_types"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	ep, err := h.service.Create(r.Context(), webhookCaller(r), webhooksapp.CreateRequest{
		URL: body.URL, Secret: body.Secret, EventTypes: body.EventTypes,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, ep)
}

func (h *WebhookEndpointHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		URL        *string  `json:"url"`
		EventTypes []string `json:"event_types"`
		IsActive   *bool    `json:"is_active"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	ep, err := h.service.Update(r.Context(), webhookCaller(r), id, webhooksapp.UpdateRequest{
		URL: body.URL, EventTypes: body.EventTypes, IsActive: body.IsActive,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, ep)
}

func (h *WebhookEndpointHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.service.Delete(r.Context(), webhookCaller(r), id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func webhookCaller(r *http.Request) webhooksapp.Caller {
	var userID *uuid.UUID
	if user := middleware.UserFromCtx(r.Context()); user != nil {
		id := user.ID
		userID = &id
	}
	return webhooksapp.Caller{
		Actor:  middleware.ActorFromContext(r.Context()),
		UserID: userID,
		HasScope: func(scope string) bool {
			return middleware.HasScope(r.Context(), scope)
		},
	}
}
