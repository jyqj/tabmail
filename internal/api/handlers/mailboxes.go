package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/hooks"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

type MailboxHandler struct {
	store       store.Store
	obj         store.ObjectStore
	dispatcher  *hooks.Dispatcher
	namingMode  policy.NamingMode
	stripPlus   bool
	tokenSecret string
	logger      zerolog.Logger
}

func NewMailboxHandler(s store.Store, obj store.ObjectStore, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, l zerolog.Logger) *MailboxHandler {
	return &MailboxHandler{store: s, obj: obj, dispatcher: dispatcher, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret, logger: l.With().Str("handler", "mailboxes").Logger()}
}

// List GET /api/v1/mailboxes
func (h *MailboxHandler) List(w http.ResponseWriter, r *http.Request) {
	t := middleware.TenantFromCtx(r.Context())
	if t == nil {
		errForbidden(w, "no tenant context")
		return
	}
	if middleware.IsAdmin(r.Context()) && t.ID == uuid.Nil {
		errBadRequest(w, "admin requests to tenant-scoped endpoints must include X-Tenant-ID")
		return
	}
	pg := pageFromReq(r)
	list, total, err := h.store.ListMailboxes(r.Context(), t.ID, pg)
	if err != nil {
		h.logger.Err(err).Msg("list mailboxes")
		errInternal(w)
		return
	}
	okList(w, list, total, pg.Page, pg.PerPage)
}

// Create POST /api/v1/mailboxes
func (h *MailboxHandler) Create(w http.ResponseWriter, r *http.Request) {
	t := middleware.TenantFromCtx(r.Context())
	if t == nil {
		errForbidden(w, "no tenant context")
		return
	}
	if middleware.IsAdmin(r.Context()) && t.ID == uuid.Nil {
		errBadRequest(w, "admin requests to tenant-scoped endpoints must include X-Tenant-ID")
		return
	}

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
	body.Address = strings.ToLower(strings.TrimSpace(body.Address))
	local, domain, err := policy.NormalizeAddressParts(body.Address, h.stripPlus)
	if err != nil {
		errBadRequest(w, "invalid email address")
		return
	}
	mailboxKey, err := policy.ExtractMailbox(body.Address, h.namingMode, h.stripPlus)
	if err != nil {
		errBadRequest(w, "invalid mailbox address")
		return
	}

	zone, err := h.store.GetZoneByDomain(r.Context(), domain)
	if err != nil {
		errInternal(w)
		return
	}
	if zone == nil {
		errBadRequest(w, fmt.Sprintf("domain %s is not registered", domain))
		return
	}
	if !middleware.IsAdmin(r.Context()) && zone.TenantID != t.ID {
		errForbidden(w, "domain belongs to another tenant")
		return
	}

	cfg, err := h.store.EffectiveConfig(r.Context(), t.ID)
	if err != nil {
		errInternal(w)
		return
	}
	count, err := h.store.CountMailboxes(r.Context(), zone.ID)
	if err != nil {
		errInternal(w)
		return
	}
	if !t.IsSuper && count >= cfg.MaxMailboxesPerDomain {
		errForbidden(w, fmt.Sprintf("mailbox limit reached (%d)", cfg.MaxMailboxesPerDomain))
		return
	}

	am := body.AccessMode
	if am == "" {
		am = models.AccessPublic
	}
	if !am.Valid() {
		errBadRequest(w, "invalid access_mode")
		return
	}
	if am == models.AccessToken && strings.TrimSpace(body.Password) == "" {
		errBadRequest(w, "password is required when access_mode=token")
		return
	}
	if body.RetentionHoursOverride != nil && *body.RetentionHoursOverride <= 0 {
		errBadRequest(w, "retention_hours_override must be greater than 0")
		return
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil && strings.TrimSpace(*body.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.ExpiresAt))
		if err != nil {
			errBadRequest(w, "invalid expires_at")
			return
		}
		if !parsed.After(time.Now()) {
			errBadRequest(w, "expires_at must be in the future")
			return
		}
		expiresAt = &parsed
	}

	mb := &models.Mailbox{
		TenantID:               t.ID,
		ZoneID:                 zone.ID,
		LocalPart:              local,
		ResolvedDomain:         domain,
		FullAddress:            mailboxKey,
		AccessMode:             am,
		RetentionHoursOverride: body.RetentionHoursOverride,
		ExpiresAt:              expiresAt,
	}

	if body.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			errInternal(w)
			return
		}
		s := string(hash)
		mb.PasswordHash = &s
	}

	if err := h.store.CreateMailbox(r.Context(), mb); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			errConflict(w, "address already exists")
			return
		}
		h.logger.Err(err).Msg("create mailbox")
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(t.ID),
		Actor:        actorFromRequest(r),
		Action:       "mailbox.create",
		ResourceType: "mailbox",
		ResourceID:   uuidPtr(mb.ID),
		Details: mustJSON(map[string]any{
			"address":                  mb.FullAddress,
			"access_mode":              mb.AccessMode,
			"retention_hours_override": mb.RetentionHoursOverride,
			"expires_at":               mb.ExpiresAt,
		}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "mailbox.created",
			TenantID:   t.ID.String(),
			Mailbox:    mb.FullAddress,
			OccurredAt: time.Now().UTC(),
			Metadata: map[string]any{
				"mailbox_id":  mb.ID.String(),
				"access_mode": mb.AccessMode,
			},
		})
	}
	created(w, mb)
}

// Delete DELETE /api/v1/mailboxes/{id}
func (h *MailboxHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	t := middleware.TenantFromCtx(r.Context())
	mb, err := h.store.GetMailbox(r.Context(), id)
	if err != nil {
		errInternal(w)
		return
	}
	if mb == nil {
		errNotFound(w, "mailbox not found")
		return
	}
	if !middleware.IsAdmin(r.Context()) && mb.TenantID != t.ID {
		errForbidden(w, "not your mailbox")
		return
	}
	keys, err := h.store.ListMailboxObjectKeys(r.Context(), mb.ID)
	if err != nil {
		errInternal(w)
		return
	}
	if err := h.store.DeleteMailbox(r.Context(), id); err != nil {
		errInternal(w)
		return
	}
	for _, key := range uniqueStrings(keys) {
		refs, err := h.store.CountMessagesByObjectKey(r.Context(), key)
		if err != nil {
			h.logger.Warn().Err(err).Str("key", key).Msg("count object references during mailbox delete")
			continue
		}
		if refs == 0 {
			if err := h.obj.Delete(r.Context(), key); err != nil {
				h.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during mailbox delete")
			}
		}
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(mb.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "mailbox.delete",
		ResourceType: "mailbox",
		ResourceID:   uuidPtr(mb.ID),
		Details:      mustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "mailbox.deleted",
			TenantID:   mb.TenantID.String(),
			Mailbox:    mb.FullAddress,
			OccurredAt: time.Now().UTC(),
			Metadata: map[string]any{
				"mailbox_id":      mb.ID.String(),
				"deleted_objects": len(keys),
			},
		})
	}
	noContent(w)
}

// IssueToken POST /api/v1/token
func (h *MailboxHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address  string `json:"address"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	addr := strings.ToLower(strings.TrimSpace(body.Address))
	if addr == "" || strings.TrimSpace(body.Password) == "" {
		errBadRequest(w, "address and password are required")
		return
	}
	mailboxKey, err := policy.ExtractMailbox(addr, h.namingMode, h.stripPlus)
	if err != nil {
		errBadRequest(w, "invalid address")
		return
	}
	mb, err := h.store.GetMailboxByAddress(r.Context(), mailboxKey)
	if err != nil {
		errInternal(w)
		return
	}
	if mb == nil {
		errNotFound(w, "mailbox not found")
		return
	}
	if mb.AccessMode != models.AccessToken || mb.PasswordHash == nil {
		errForbidden(w, "mailbox does not support token authentication")
		return
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		errForbidden(w, "mailbox expired")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*mb.PasswordHash), []byte(body.Password)); err != nil {
		errForbidden(w, "invalid credentials")
		return
	}
	ttl := 24 * time.Hour
	if mb.ExpiresAt != nil {
		remaining := time.Until(*mb.ExpiresAt)
		if remaining <= 0 {
			errForbidden(w, "mailbox expired")
			return
		}
		if remaining < ttl {
			ttl = remaining
		}
	}
	token, err := mailtoken.Issue(h.tokenSecret, mb.ID.String(), mb.FullAddress, ttl)
	if err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(mb.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "mailbox.issue_token",
		ResourceType: "mailbox",
		ResourceID:   uuidPtr(mb.ID),
		Details:      mustJSON(map[string]any{"address": mb.FullAddress, "ttl_seconds": int(ttl.Seconds())}),
	})
	ok(w, map[string]any{"token": token, "expires_in": int(ttl.Seconds())})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func actorFromRequest(r *http.Request) string {
	if middleware.IsAdmin(r.Context()) {
		return "admin"
	}
	if t := middleware.TenantFromCtx(r.Context()); t != nil && t.ID != uuid.Nil {
		return t.ID.String()
	}
	return "public"
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
