package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	mailboxapp "tabmail/internal/app/mailboxes"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/store"
)

type mailboxStore interface {
	app.AuditStore
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListZonesScoped(ctx context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	ListMailboxesScoped(ctx context.Context, scope authz.ZoneListFilter, pg models.Page) ([]*models.Mailbox, int, error)
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ForTenant(tenantID uuid.UUID) store.TenantScoped
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
}

type MailboxHandler struct {
	service     *mailboxapp.Service
	rateLimiter *middleware.RateLimiter
	logger      zerolog.Logger
}

func NewMailboxHandler(s mailboxStore, obj store.ObjectStore, objects *rawobject.Store, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, rl *middleware.RateLimiter, l zerolog.Logger) *MailboxHandler {
	service := mailboxapp.NewService(s, obj, objects, dispatcher, namingMode, stripPlus, tokenSecret, l)
	return &MailboxHandler{service: service, rateLimiter: rl, logger: l.With().Str("handler", "mailboxes").Logger()}
}

func (h *MailboxHandler) List(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	actor := middleware.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	items, total, err := h.service.List(r.Context(), actor, tenant, pg)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

func (h *MailboxHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address                string            `json:"address"`
		Password               string            `json:"password,omitempty"`
		AccessMode             models.AccessMode `json:"access_mode,omitempty"`
		RetentionHoursOverride *int              `json:"retention_hours_override,omitempty"`
		ExpiresAt              *string           `json:"expires_at,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil || body.Address == "" {
		errBadRequest(w, "address is required")
		return
	}
	actor := middleware.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	item, err := h.service.Create(r.Context(), actor, tenant, mailboxapp.CreateRequest{
		Address:                body.Address,
		Password:               body.Password,
		AccessMode:             body.AccessMode,
		RetentionHoursOverride: body.RetentionHoursOverride,
		ExpiresAt:              body.ExpiresAt,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *MailboxHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	actor := middleware.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	if err := h.service.Delete(r.Context(), actor, tenant, id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *MailboxHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address  string `json:"address"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	if h.rateLimiter != nil && body.Address != "" {
		allowed, err := h.rateLimiter.CheckAddressRateLimit(r.Context(), body.Address, 5, time.Minute)
		if err == nil && !allowed {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, envelope{Error: &apiErr{Code: "RATE_LIMITED", Message: "too many token requests for this address"}})
			return
		}
	}
	item, err := h.service.IssueToken(r.Context(), body.Address, body.Password, middleware.ActorFromContext(r.Context()).AuditLabel())
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}
