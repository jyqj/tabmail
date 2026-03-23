package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	mailboxapp "tabmail/internal/app/mailboxes"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

type mailboxStore interface {
	app.AuditStore
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	ListMailboxes(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
}

type MailboxHandler struct {
	service *mailboxapp.Service
	logger  zerolog.Logger
}

func NewMailboxHandler(s mailboxStore, obj store.ObjectStore, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, l zerolog.Logger) *MailboxHandler {
	service := mailboxapp.NewService(s, obj, dispatcher, namingMode, stripPlus, tokenSecret, l)
	return &MailboxHandler{service: service, logger: l.With().Str("handler", "mailboxes").Logger()}
}

func (h *MailboxHandler) List(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	items, total, err := h.service.List(r.Context(), middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), pg)
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
	item, err := h.service.Create(r.Context(), middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), mailboxapp.CreateRequest{
		Address:                body.Address,
		Password:               body.Password,
		AccessMode:             body.AccessMode,
		RetentionHoursOverride: body.RetentionHoursOverride,
		ExpiresAt:              body.ExpiresAt,
	}, actorFromRequest(r))
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
	if err := h.service.Delete(r.Context(), middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), id, actorFromRequest(r)); err != nil {
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
	item, err := h.service.IssueToken(r.Context(), body.Address, body.Password, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}
