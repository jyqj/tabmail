// Package webhooksapp holds the tenant webhook endpoint flows behind the
// /webhook-endpoints routes. The handlers in internal/api/handlers only decode
// requests and shape responses; the destination validation that keeps a tenant
// from pointing TabMail at a private address lives here.
package webhooksapp

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type storeRepo interface {
	CreateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	ListWebhookEndpoints(ctx context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error)
	GetWebhookEndpoint(ctx context.Context, id uuid.UUID) (*models.WebhookEndpoint, error)
	UpdateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	DeleteWebhookEndpoint(ctx context.Context, id uuid.UUID) error
}

// Caller is the identity a webhook endpoint request acts under.
type Caller struct {
	Actor authz.Actor
	// UserID attributes a created endpoint to a person, and is nil for API keys
	// that are not tied to one.
	UserID *uuid.UUID
	// HasScope reports whether an API key caller presented the given scope.
	// Only the transport layer knows how scopes arrive, so the service asks
	// through this seam instead of reading a request context.
	HasScope func(scope string) bool
}

const (
	scopeRead  = "webhooks:read"
	scopeWrite = "webhooks:write"
)

type Service struct {
	store  storeRepo
	logger zerolog.Logger
}

func NewService(s storeRepo, logger zerolog.Logger) *Service {
	return &Service{store: s, logger: logger.With().Str("service", "webhooks").Logger()}
}

// CreateRequest is the endpoint a tenant wants TabMail to call.
type CreateRequest struct {
	URL        string
	Secret     string
	EventTypes []string
}

// UpdateRequest describes a partial update: a nil field is left as it is.
type UpdateRequest struct {
	URL        *string
	EventTypes []string
	IsActive   *bool
}

func (s *Service) List(ctx context.Context, caller Caller) ([]*models.WebhookEndpoint, error) {
	if err := authorize(caller, scopeRead); err != nil {
		return nil, err
	}
	tenantID, err := tenantScope(caller)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListWebhookEndpoints(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if items == nil {
		items = []*models.WebhookEndpoint{}
	}
	return items, nil
}

func (s *Service) Create(ctx context.Context, caller Caller, req CreateRequest) (*models.WebhookEndpoint, error) {
	if err := authorize(caller, scopeWrite); err != nil {
		return nil, err
	}
	tenantID, err := tenantScope(caller)
	if err != nil {
		return nil, err
	}

	endpointURL, err := validateEndpointURL(req.URL)
	if err != nil {
		return nil, app.BadRequest(err.Error())
	}
	eventTypes, err := sanitizeEventTypes(req.EventTypes)
	if err != nil {
		return nil, app.BadRequest(err.Error())
	}
	secret, err := sanitizeSecret(req.Secret)
	if err != nil {
		return nil, app.BadRequest(err.Error())
	}

	now := time.Now()
	ep := &models.WebhookEndpoint{
		ID:         uuid.New(),
		TenantID:   tenantID,
		URL:        endpointURL,
		EventTypes: eventTypes,
		IsActive:   true,
		CreatedBy:  caller.UserID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	ep.Secret = secret
	if err := s.store.CreateWebhookEndpoint(ctx, ep); err != nil {
		return nil, app.Internal(err)
	}
	return ep, nil
}

func (s *Service) Update(ctx context.Context, caller Caller, id uuid.UUID, req UpdateRequest) (*models.WebhookEndpoint, error) {
	existing, err := s.ownedEndpoint(ctx, caller, id)
	if err != nil {
		return nil, err
	}

	if req.URL != nil {
		endpointURL, err := validateEndpointURL(*req.URL)
		if err != nil {
			return nil, app.BadRequest(err.Error())
		}
		existing.URL = endpointURL
	}
	if req.EventTypes != nil {
		eventTypes, err := sanitizeEventTypes(req.EventTypes)
		if err != nil {
			return nil, app.BadRequest(err.Error())
		}
		existing.EventTypes = eventTypes
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := s.store.UpdateWebhookEndpoint(ctx, existing); err != nil {
		return nil, app.Internal(err)
	}
	return existing, nil
}

func (s *Service) Delete(ctx context.Context, caller Caller, id uuid.UUID) error {
	if _, err := s.ownedEndpoint(ctx, caller, id); err != nil {
		return err
	}
	if err := s.store.DeleteWebhookEndpoint(ctx, id); err != nil {
		return app.Internal(err)
	}
	return nil
}

// ownedEndpoint loads an endpoint the caller may write to. An endpoint in
// another tenant reports as missing rather than forbidden, so the route never
// confirms that an id exists elsewhere.
func (s *Service) ownedEndpoint(ctx context.Context, caller Caller, id uuid.UUID) (*models.WebhookEndpoint, error) {
	if err := authorize(caller, scopeWrite); err != nil {
		return nil, err
	}
	tenantID, err := tenantScope(caller)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetWebhookEndpoint(ctx, id)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing == nil || existing.TenantID != tenantID {
		return nil, app.NotFound("webhook endpoint not found")
	}
	return existing, nil
}

// authorize gates webhook endpoint management: admins (super or tenant) are
// allowed, API key callers must carry the required scope, and plain JWT users
// are refused. That last rule is deliberately stricter than the routes'
// RequireScopes middleware, which lets any logged-in user through.
func authorize(caller Caller, requiredScope string) error {
	switch {
	case caller.Actor.IsSuperAdmin || caller.Actor.IsAdmin:
		return nil
	case caller.Actor.Type == authz.PrincipalAPIKey:
		if caller.HasScope != nil && caller.HasScope(requiredScope) {
			return nil
		}
		return app.Forbidden("insufficient api key scope")
	default:
		return app.Forbidden("webhook endpoint access requires admin or webhook-scoped api key")
	}
}

func tenantScope(caller Caller) (uuid.UUID, error) {
	if caller.Actor.TenantID == uuid.Nil {
		return uuid.Nil, app.Forbidden("tenant required")
	}
	return caller.Actor.TenantID, nil
}

var eventTypeRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)

// TODO: Dispatcher still uses configured static webhook URLs. When tenant
// endpoints are wired, re-validate the resolved destination IP at dispatch time
// to avoid DNS rebinding/private-network SSRF.
func validateEndpointURL(raw string) (string, error) {
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

func sanitizeEventTypes(raw []string) ([]string, error) {
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
		if !eventTypeRE.MatchString(eventType) {
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

func sanitizeSecret(raw string) (*string, error) {
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
