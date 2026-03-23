package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type AdminHandler struct {
	store         store.Store
	dispatcher    *hooks.Dispatcher
	defaultPolicy models.SMTPPolicy
	logger        zerolog.Logger
}

func NewAdminHandler(s store.Store, dispatcher *hooks.Dispatcher, defaultPolicy models.SMTPPolicy, l zerolog.Logger) *AdminHandler {
	return &AdminHandler{store: s, dispatcher: dispatcher, defaultPolicy: defaultPolicy, logger: l.With().Str("handler", "admin").Logger()}
}

// ---- Tenants --------------------------------------------------------

func (h *AdminHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListTenants(r.Context())
	if err != nil {
		h.logger.Err(err).Msg("list tenants")
		errInternal(w)
		return
	}
	ok(w, list)
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
	plan, err := h.store.GetPlan(r.Context(), planID)
	if err != nil {
		errInternal(w)
		return
	}
	if plan == nil {
		errBadRequest(w, "plan_id does not exist")
		return
	}
	t := &models.Tenant{Name: body.Name, PlanID: planID}
	if err := h.store.CreateTenant(r.Context(), t); err != nil {
		h.logger.Err(err).Msg("create tenant")
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "tenant.create",
		ResourceType: "tenant",
		ResourceID:   uuidPtr(t.ID),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(map[string]any{"name": t.Name, "plan_id": t.PlanID}),
	})
	created(w, t)
}

func (h *AdminHandler) UpdateTenantOverride(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	tenant, err := h.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		errInternal(w)
		return
	}
	if tenant == nil {
		errNotFound(w, "tenant not found")
		return
	}
	var body models.TenantOverride
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	body.TenantID = tenantID
	if err := h.store.UpsertOverride(r.Context(), &body); err != nil {
		h.logger.Err(err).Msg("upsert override")
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(tenantID),
		Action:       "tenant.override.upsert",
		ResourceType: "tenant_override",
		ResourceID:   uuidPtr(body.ID),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(body),
	})
	ok(w, body)
}

func (h *AdminHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.store.DeleteTenant(r.Context(), id); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "tenant.delete",
		ResourceType: "tenant",
		ResourceID:   uuidPtr(id),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(map[string]any{"tenant_id": id}),
	})
	noContent(w)
}

func (h *AdminHandler) GetEffectiveConfig(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	cfg, err := h.store.EffectiveConfig(r.Context(), id)
	if err != nil {
		h.logger.Err(err).Msg("effective config")
		errInternal(w)
		return
	}
	ok(w, cfg)
}

// ---- Plans ----------------------------------------------------------

func (h *AdminHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListPlans(r.Context())
	if err != nil {
		errInternal(w)
		return
	}
	ok(w, list)
}

func (h *AdminHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var p models.Plan
	if err := decodeBody(r, &p); err != nil || p.Name == "" {
		errBadRequest(w, "name is required")
		return
	}
	if err := h.store.CreatePlan(r.Context(), &p); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "plan.create",
		ResourceType: "plan",
		ResourceID:   uuidPtr(p.ID),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(map[string]any{"name": p.Name}),
	})
	created(w, p)
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
	if err := h.store.UpdatePlan(r.Context(), &p); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "plan.update",
		ResourceType: "plan",
		ResourceID:   uuidPtr(p.ID),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(p),
	})
	ok(w, p)
}

func (h *AdminHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.store.DeletePlan(r.Context(), id); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "plan.delete",
		ResourceType: "plan",
		ResourceID:   uuidPtr(id),
		Actor:        actorFromRequest(r),
	})
	noContent(w)
}

// ---- API Keys -------------------------------------------------------

func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	tenant, err := h.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		errInternal(w)
		return
	}
	if tenant == nil {
		errNotFound(w, "tenant not found")
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

	raw := generateKey()
	hash := sha256.Sum256([]byte(raw))
	scopes := body.Scopes
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}

	k := &models.TenantAPIKey{
		TenantID:  tenantID,
		KeyHash:   hex.EncodeToString(hash[:]),
		KeyPrefix: raw[:12],
		Label:     body.Label,
		Scopes:    scopes,
	}
	if err := h.store.CreateAPIKey(r.Context(), k); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(tenantID),
		Action:       "api_key.create",
		ResourceType: "tenant_api_key",
		ResourceID:   uuidPtr(k.ID),
		Actor:        actorFromRequest(r),
		Details:      mustJSON(map[string]any{"label": k.Label, "key_prefix": k.KeyPrefix, "scopes": k.Scopes}),
	})
	created(w, map[string]any{
		"id":         k.ID,
		"key":        raw,
		"key_prefix": k.KeyPrefix,
		"label":      k.Label,
		"scopes":     k.Scopes,
		"created_at": k.CreatedAt,
	})
}

func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	keys, err := h.store.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		errInternal(w)
		return
	}
	ok(w, keys)
}

func (h *AdminHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := uuid.Parse(chi.URLParam(r, "keyId"))
	if err != nil {
		errBadRequest(w, "invalid key id")
		return
	}
	if err := h.store.DeleteAPIKey(r.Context(), keyID); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "api_key.delete",
		ResourceType: "tenant_api_key",
		ResourceID:   uuidPtr(keyID),
		Actor:        actorFromRequest(r),
	})
	noContent(w)
}

// ---- Stats ----------------------------------------------------------

func (h *AdminHandler) Stats(w http.ResponseWriter, r *http.Request) {
	tenants, _ := h.store.ListTenants(r.Context())
	plans, _ := h.store.ListPlans(r.Context())
	domains, _ := h.store.CountAllZones(r.Context())
	mailboxes, _ := h.store.CountAllMailboxes(r.Context())
	messages, _ := h.store.CountAllMessages(r.Context())
	audit, _ := h.store.ListAuditEntries(r.Context(), 12)
	deadLetters := []models.DeadLetter{}
	deadLetterSize := 0
	webhooksEnabled := false
	if h.dispatcher != nil {
		webhooksEnabled = h.dispatcher.Enabled()
		dl := h.dispatcher.DeadLetters(10)
		deadLetterSize = h.dispatcher.DeadLetterSize()
		deadLetters = append(deadLetters, dl...)
	}
	ok(w, models.SystemStats{
		TenantsCount:    len(tenants),
		PlansCount:      len(plans),
		DomainsCount:    domains,
		MailboxesCount:  mailboxes,
		MessagesCount:   messages,
		Metrics:         metrics.Snapshot(webhooksEnabled, deadLetterSize),
		RecentAudit:     audit,
		TenantDelivery:  metrics.TopTenantDelivery(10),
		MailboxDelivery: metrics.TopMailboxDelivery(10),
		DeadLetters:     deadLetters,
	})
}

func (h *AdminHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	entries, total, err := h.store.ListAuditEntriesPaged(r.Context(), pg)
	if err != nil {
		h.logger.Err(err).Msg("list audit")
		errInternal(w)
		return
	}
	if entries == nil {
		entries = []*models.AuditEntry{}
	}
	okList(w, entries, total, pg.Page, pg.PerPage)
}

func (h *AdminHandler) GetSMTPPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.GetSMTPPolicy(r.Context())
	if err != nil {
		errInternal(w)
		return
	}
	if p == nil {
		p = &h.defaultPolicy
	}
	ok(w, p)
}

func (h *AdminHandler) UpdateSMTPPolicy(w http.ResponseWriter, r *http.Request) {
	var body models.SMTPPolicy
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	if err := h.store.UpsertSMTPPolicy(r.Context(), &body); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		Action:       "smtp_policy.update",
		ResourceType: "smtp_policy",
		Actor:        actorFromRequest(r),
		Details:      mustJSON(body),
	})
	ok(w, body)
}

func generateKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return fmt.Sprintf("tb_%s", hex.EncodeToString(b))
}
