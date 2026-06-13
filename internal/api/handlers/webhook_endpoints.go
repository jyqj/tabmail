package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
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
	store  webhookEndpointStore
	logger zerolog.Logger
}

var webhookEventTypeRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)

// TODO: Dispatcher still uses configured static webhook URLs. When tenant
// endpoints are wired, re-validate the resolved destination IP at dispatch time
// to avoid DNS rebinding/private-network SSRF.
func NewWebhookEndpointHandler(s webhookEndpointStore, l zerolog.Logger) *WebhookEndpointHandler {
	return &WebhookEndpointHandler{store: s, logger: l.With().Str("handler", "webhook_endpoints").Logger()}
}

func (h *WebhookEndpointHandler) List(w http.ResponseWriter, r *http.Request) {
	if !authorizeWebhookEndpointAccess(w, r, "webhooks:read") {
		return
	}
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "tenant required")
		return
	}
	items, err := h.store.ListWebhookEndpoints(r.Context(), tenant.ID)
	if err != nil {
		h.logger.Err(err).Msg("list webhook endpoints")
		errInternal(w)
		return
	}
	if items == nil {
		items = []*models.WebhookEndpoint{}
	}
	ok(w, items)
}

func (h *WebhookEndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !authorizeWebhookEndpointAccess(w, r, "webhooks:write") {
		return
	}
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "tenant required")
		return
	}
	var body struct {
		URL        string   `json:"url"`
		Secret     string   `json:"secret"`
		EventTypes []string `json:"event_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	endpointURL, err := validateWebhookEndpointURL(body.URL)
	if err != nil {
		errBadRequest(w, err.Error())
		return
	}
	eventTypes, err := sanitizeWebhookEventTypes(body.EventTypes)
	if err != nil {
		errBadRequest(w, err.Error())
		return
	}
	secret, err := sanitizeWebhookSecret(body.Secret)
	if err != nil {
		errBadRequest(w, err.Error())
		return
	}
	var userID *uuid.UUID
	if user := middleware.UserFromCtx(r.Context()); user != nil {
		userID = &user.ID
	}
	now := time.Now()
	ep := &models.WebhookEndpoint{
		ID:         uuid.New(),
		TenantID:   tenant.ID,
		URL:        endpointURL,
		EventTypes: eventTypes,
		IsActive:   true,
		CreatedBy:  userID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	ep.Secret = secret
	if err := h.store.CreateWebhookEndpoint(r.Context(), ep); err != nil {
		h.logger.Err(err).Msg("create webhook endpoint")
		errInternal(w)
		return
	}
	created(w, ep)
}

func (h *WebhookEndpointHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !authorizeWebhookEndpointAccess(w, r, "webhooks:write") {
		return
	}
	actor := authz.ActorFromContext(r.Context())
	if actor.TenantID == uuid.Nil {
		errForbidden(w, "tenant required")
		return
	}
	epID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	existing, err := h.store.GetWebhookEndpoint(r.Context(), epID)
	if err != nil {
		errInternal(w)
		return
	}
	// Keep NotFound semantics (info-hiding) for cross-tenant access.
	if existing == nil || existing.TenantID != actor.TenantID {
		errNotFound(w, "webhook endpoint not found")
		return
	}
	var body struct {
		URL        *string  `json:"url"`
		EventTypes []string `json:"event_types"`
		IsActive   *bool    `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	if body.URL != nil {
		endpointURL, err := validateWebhookEndpointURL(*body.URL)
		if err != nil {
			errBadRequest(w, err.Error())
			return
		}
		existing.URL = endpointURL
	}
	if body.EventTypes != nil {
		eventTypes, err := sanitizeWebhookEventTypes(body.EventTypes)
		if err != nil {
			errBadRequest(w, err.Error())
			return
		}
		existing.EventTypes = eventTypes
	}
	if body.IsActive != nil {
		existing.IsActive = *body.IsActive
	}
	if err := h.store.UpdateWebhookEndpoint(r.Context(), existing); err != nil {
		h.logger.Err(err).Msg("update webhook endpoint")
		errInternal(w)
		return
	}
	ok(w, existing)
}

func (h *WebhookEndpointHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !authorizeWebhookEndpointAccess(w, r, "webhooks:write") {
		return
	}
	actor := authz.ActorFromContext(r.Context())
	if actor.TenantID == uuid.Nil {
		errForbidden(w, "tenant required")
		return
	}
	epID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	existing, err := h.store.GetWebhookEndpoint(r.Context(), epID)
	if err != nil {
		errInternal(w)
		return
	}
	// Keep NotFound semantics (info-hiding) for cross-tenant access.
	if existing == nil || existing.TenantID != actor.TenantID {
		errNotFound(w, "webhook endpoint not found")
		return
	}
	if err := h.store.DeleteWebhookEndpoint(r.Context(), epID); err != nil {
		h.logger.Err(err).Msg("delete webhook endpoint")
		errInternal(w)
		return
	}
	noContent(w)
}

// authorizeWebhookEndpointAccess gates webhook endpoint management through the
// actor seam: admins (super or tenant) are allowed, API key callers must carry
// the required scope, and plain JWT users are forbidden. The plain-user denial
// is intentionally stricter than the routes' RequireScopes middleware, which
// passes any logged-in JWT user; this helper adds that rule on purpose.
func authorizeWebhookEndpointAccess(w http.ResponseWriter, r *http.Request, requiredScope string) bool {
	actor := authz.ActorFromContext(r.Context())
	switch {
	case actor.IsSuperAdmin || actor.IsAdmin:
		return true
	case actor.Type == authz.PrincipalAPIKey:
		if middleware.HasScope(r.Context(), requiredScope) {
			return true
		}
		errForbidden(w, "insufficient api key scope")
		return false
	default:
		errForbidden(w, "webhook endpoint access requires admin or webhook-scoped api key")
		return false
	}
}

func validateWebhookEndpointURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if len(raw) > 2048 {
		return "", errors.New("url is too long")
	}
	if containsControlRune(raw) {
		return "", errors.New("url contains invalid control characters")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("webhook url must use https")
	}
	if parsed.User != nil {
		return "", errors.New("webhook url must not contain credentials")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", errors.New("webhook url host is required")
	}
	if strings.Contains(host, "%") {
		return "", errors.New("webhook url host is not allowed")
	}
	normalizedHost := strings.TrimSuffix(strings.ToLower(host), ".")
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		return "", errors.New("webhook url host is not allowed")
	}
	for _, candidate := range []string{host, normalizedHost} {
		if addr, err := netip.ParseAddr(candidate); err == nil {
			addr = addr.Unmap()
			if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
				return "", errors.New("webhook url host is not allowed")
			}
			break
		}
		if candidate != normalizedHost {
			continue
		}
		if ipLikeHostname(candidate) {
			return "", errors.New("webhook url host is not allowed")
		}
	}

	parsed.Scheme = "https"
	return parsed.String(), nil
}

func sanitizeWebhookEventTypes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	if len(raw) > 32 {
		return nil, errors.New("too many event_types")
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		eventType := strings.ToLower(strings.TrimSpace(item))
		if eventType == "" {
			return nil, errors.New("event_types must not contain empty values")
		}
		if !webhookEventTypeRE.MatchString(eventType) {
			return nil, errors.New("event_types contains invalid value")
		}
		if _, ok := seen[eventType]; ok {
			continue
		}
		seen[eventType] = struct{}{}
		out = append(out, eventType)
	}
	return out, nil
}

func sanitizeWebhookSecret(raw string) (*string, error) {
	secret := strings.TrimSpace(raw)
	if secret == "" {
		return nil, nil
	}
	if len(secret) > 1024 {
		return nil, errors.New("secret is too long")
	}
	if containsControlRune(secret) {
		return nil, errors.New("secret contains invalid control characters")
	}
	return &secret, nil
}

func containsControlRune(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

func ipLikeHostname(host string) bool {
	return strings.Count(host, ".") == 3 && strings.IndexFunc(host, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}) == -1
}
