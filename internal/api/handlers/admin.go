package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	adminapp "tabmail/internal/app/admin"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
)

type adminStore interface {
	adminapp.Store
}

type AdminHandler struct {
	service *adminapp.Service
	logger  zerolog.Logger
}

func NewAdminHandler(s adminStore, dispatcher *hooks.Dispatcher, defaultPolicy models.SMTPPolicy, l zerolog.Logger) *AdminHandler {
	service := adminapp.NewService(s, dispatcher, defaultPolicy, l)
	return &AdminHandler{service: service, logger: l.With().Str("handler", "admin").Logger()}
}

func (h *AdminHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListTenants(r.Context())
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *AdminHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		PlanID string `json:"plan_id"`
	}
	if err := decodeBody(r, &body); err != nil || body.Name == "" || body.PlanID == "" {
		errBadRequest(w, "name and plan_id are required")
		return
	}
	planID, err := uuid.Parse(body.PlanID)
	if err != nil {
		errBadRequest(w, "invalid plan_id")
		return
	}
	item, err := h.service.CreateTenant(r.Context(), body.Name, planID, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *AdminHandler) UpdateTenantOverride(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	var body models.TenantOverride
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	item, err := h.service.UpdateTenantOverride(r.Context(), tenantID, body, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *AdminHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.service.DeleteTenant(r.Context(), id, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *AdminHandler) GetEffectiveConfig(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	cfg, err := h.service.GetEffectiveConfig(r.Context(), id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, cfg)
}

func (h *AdminHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListPlans(r.Context())
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *AdminHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var p models.Plan
	if err := decodeBody(r, &p); err != nil || p.Name == "" {
		errBadRequest(w, "name is required")
		return
	}
	item, err := h.service.CreatePlan(r.Context(), &p, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *AdminHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var p models.Plan
	if err := decodeBody(r, &p); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	p.ID = id
	item, err := h.service.UpdatePlan(r.Context(), &p, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *AdminHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.service.DeletePlan(r.Context(), id, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	var body struct {
		Label  string   `json:"label"`
		Scopes []string `json:"scopes,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	item, err := h.service.CreateAPIKey(r.Context(), tenantID, body.Label, body.Scopes, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	items, err := h.service.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *AdminHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := uuid.Parse(chi.URLParam(r, "keyId"))
	if err != nil {
		errBadRequest(w, "invalid key id")
		return
	}
	if err := h.service.DeleteAPIKey(r.Context(), keyID, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *AdminHandler) Stats(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Stats(r.Context())
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *AdminHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	items, total, err := h.service.ListAudit(r.Context(), pg)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

func (h *AdminHandler) ListIngestJobs(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	items, total, err := h.service.ListIngestJobs(r.Context(), pg, r.URL.Query().Get("state"), r.URL.Query().Get("source"), r.URL.Query().Get("recipient"))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

func (h *AdminHandler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	items, total, err := h.service.ListWebhookDeliveries(r.Context(), pg, r.URL.Query().Get("state"), r.URL.Query().Get("event_type"), r.URL.Query().Get("url"))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

func (h *AdminHandler) GetSMTPPolicy(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetSMTPPolicy(r.Context())
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *AdminHandler) UpdateSMTPPolicy(w http.ResponseWriter, r *http.Request) {
	var body models.SMTPPolicy
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	item, err := h.service.UpdateSMTPPolicy(r.Context(), &body, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}
