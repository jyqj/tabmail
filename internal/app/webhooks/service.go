// Package webhooksapp holds the tenant webhook endpoint flows behind the
// /webhook-endpoints routes. The handlers in internal/api/handlers only decode
// requests and shape responses; destination validation is shared with the
// delivery worker through internal/netpolicy so SSRF rules cannot drift.
package webhooksapp

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/netpolicy"
	"tabmail/internal/store"
)

type storeRepo interface {
	store.Transactor
	app.AuditStore
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
	store      storeRepo
	dispatcher *hooks.Dispatcher
	logger     zerolog.Logger
}

func NewService(s storeRepo, dispatcher *hooks.Dispatcher, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, logger: logger.With().Str("service", "webhooks").Logger()}
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
	audit := models.AuditEntry{TenantID: app.UUIDPtr(tenantID), Actor: caller.Actor.AuditLabel(), Action: "webhook_endpoint.create", ResourceType: "webhook_endpoint", ResourceID: app.UUIDPtr(ep.ID), Details: app.MustJSON(map[string]any{"url": ep.URL, "event_types": ep.EventTypes, "is_active": ep.IsActive})}
	event := hooks.Event{Type: "webhook_endpoint.created", TenantID: tenantID.String(), OccurredAt: now.UTC(), Metadata: map[string]any{"webhook_endpoint_id": ep.ID.String(), "url": ep.URL, "event_types": ep.EventTypes, "is_active": ep.IsActive}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.CreateWebhookEndpoint(txCtx, ep)
	}); err != nil {
		return nil, err
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

	audit := models.AuditEntry{TenantID: app.UUIDPtr(existing.TenantID), Actor: caller.Actor.AuditLabel(), Action: "webhook_endpoint.update", ResourceType: "webhook_endpoint", ResourceID: app.UUIDPtr(existing.ID), Details: app.MustJSON(map[string]any{"url": existing.URL, "event_types": existing.EventTypes, "is_active": existing.IsActive})}
	event := hooks.Event{Type: "webhook_endpoint.updated", TenantID: existing.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"webhook_endpoint_id": existing.ID.String(), "url": existing.URL, "event_types": existing.EventTypes, "is_active": existing.IsActive}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.UpdateWebhookEndpoint(txCtx, existing)
	}); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Service) Delete(ctx context.Context, caller Caller, id uuid.UUID) error {
	existing, err := s.ownedEndpoint(ctx, caller, id)
	if err != nil {
		return err
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(existing.TenantID), Actor: caller.Actor.AuditLabel(), Action: "webhook_endpoint.delete", ResourceType: "webhook_endpoint", ResourceID: app.UUIDPtr(existing.ID), Details: app.MustJSON(map[string]any{"url": existing.URL, "event_types": existing.EventTypes, "is_active": existing.IsActive})}
	event := hooks.Event{Type: "webhook_endpoint.deleted", TenantID: existing.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"webhook_endpoint_id": existing.ID.String(), "url": existing.URL}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeleteWebhookEndpoint(txCtx, id)
	})
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

func validateEndpointURL(raw string) (string, error) {
	return netpolicy.NormalizeWebhookURL(raw)
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
