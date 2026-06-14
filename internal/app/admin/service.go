package adminapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
)

type Store interface {
	app.AuditStore
	GetSetting(ctx context.Context, key string) (*models.SystemSetting, error)
	UpsertSetting(ctx context.Context, key, value, description string) error
	ListSettings(ctx context.Context) ([]*models.SystemSetting, error)
	GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error)
	CreateTenant(ctx context.Context, t *models.Tenant) error
	ListTenants(ctx context.Context) ([]*models.Tenant, error)
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	UpsertOverride(ctx context.Context, o *models.TenantOverride) error
	DeleteTenant(ctx context.Context, id uuid.UUID) error
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	ListPlans(ctx context.Context) ([]*models.Plan, error)
	CreatePlan(ctx context.Context, p *models.Plan) error
	UpdatePlan(ctx context.Context, p *models.Plan) error
	DeletePlan(ctx context.Context, id uuid.UUID) error
	CreateAPIKey(ctx context.Context, k *models.TenantAPIKey) error
	GetAPIKey(ctx context.Context, id uuid.UUID) (*models.TenantAPIKey, error)
	ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error)
	ListAPIKeysByOwner(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error)
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	CountAllZones(ctx context.Context) (int, error)
	CountAllMailboxes(ctx context.Context) (int, error)
	CountAllMessages(ctx context.Context) (int, error)
	ListAuditEntries(ctx context.Context, limit int) ([]*models.AuditEntry, error)
	ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error)
	GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error)
	UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error
	ListIngestJobs(ctx context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error)
	ListWebhookDeliveries(ctx context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error)
}

type Service struct {
	store         Store
	dispatcher    *hooks.Dispatcher
	defaultPolicy models.SMTPPolicy
	settings      settingsManager
	// policyInvalidator drops any cached SMTP policy after an update so ingest
	// stops deciding accept/store/discard against the stale entry. May be nil
	// (e.g. in tests or when ingest is not wired in); the TTL still bounds drift.
	policyInvalidator policyInvalidator
	logger            zerolog.Logger
}

type settingsManager interface {
	Get(ctx context.Context, key, defaultVal string) string
	GetInt(ctx context.Context, key string, defaultVal int) int
	GetBool(ctx context.Context, key string, defaultVal bool) bool
	Set(ctx context.Context, key, value, description string) error
	All(ctx context.Context) ([]*models.SystemSetting, error)
	Invalidate()
}

// policyInvalidator evicts a cached SMTP policy. Implemented by ingest.Service.
type policyInvalidator interface {
	InvalidateSMTPPolicy()
}

type APIKeyIssueResult struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	KeyPrefix string    `json:"key_prefix"`
	Label     string    `json:"label"`
	Scopes    []string  `json:"scopes"`
	CreatedAt any       `json:"created_at"`
}

var allowedAPIKeyScopes = []string{
	"domains:read",
	"domains:write",
	"routes:read",
	"routes:write",
	"mailboxes:read",
	"mailboxes:write",
	"messages:read",
	"messages:write",
	"send:read",
	"send:write",
	"webhooks:read",
	"webhooks:write",
}

var defaultAPIKeyScopes = []string{
	"domains:read",
	"routes:read",
	"mailboxes:read",
	"messages:read",
}

func normalizeAPIKeyScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return append([]string(nil), defaultAPIKeyScopes...), nil
	}
	allowed := make(map[string]int, len(allowedAPIKeyScopes))
	for i, scope := range allowedAPIKeyScopes {
		allowed[scope] = i
	}
	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "" {
			continue
		}
		if scope == "*" {
			return nil, app.BadRequest("wildcard api key scope is not allowed; select explicit scopes")
		}
		if _, ok := allowed[scope]; !ok {
			return nil, app.BadRequest("unknown api key scope: " + scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return nil, app.BadRequest("at least one api key scope is required")
	}
	sort.Slice(normalized, func(i, j int) bool {
		return allowed[normalized[i]] < allowed[normalized[j]]
	})
	return normalized, nil
}

func NewService(s Store, dispatcher *hooks.Dispatcher, defaultPolicy models.SMTPPolicy, sm settingsManager, pi policyInvalidator, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, defaultPolicy: defaultPolicy, settings: sm, policyInvalidator: pi, logger: logger.With().Str("service", "admin").Logger()}
}

// ListSettings returns all system settings.
func (s *Service) ListSettings(ctx context.Context) ([]*models.SystemSetting, error) {
	items, err := s.settings.All(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	if items == nil {
		items = []*models.SystemSetting{}
	}
	return items, nil
}

// UpdateSetting updates a single system setting.
func (s *Service) UpdateSetting(ctx context.Context, key, value string, actor string) error {
	if key == "" {
		return app.BadRequest("key is required")
	}
	// Validate known keys
	switch key {
	case models.SettingAutoCreateRouteRPM, models.SettingAutoCreateTenantRPM,
		models.SettingMonitorHistory, models.SettingFallbackRetentionH,
		models.SettingPublicIPRPM:
		// Must be a valid int
		if _, err := fmt.Sscanf(value, "%d", new(int)); err != nil {
			return app.BadRequest("value must be an integer for " + key)
		}
	case models.SettingStripPlusTag, models.SettingOpenRegistration:
		// Must be a valid bool
		if value != "true" && value != "false" {
			return app.BadRequest("value must be true or false for " + key)
		}
	case models.SettingMailboxNaming:
		switch value {
		case "full", "local", "domain":
		default:
			return app.BadRequest("value must be full, local, or domain for " + key)
		}
	}

	if err := s.settings.Set(ctx, key, value, ""); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "setting.update",
		ResourceType: "system_setting",
		Actor:        actor,
		Details:      app.MustJSON(map[string]any{"key": key, "value": value}),
	})
	return nil
}

// BulkUpdateSettings updates multiple settings at once.
func (s *Service) BulkUpdateSettings(ctx context.Context, updates map[string]string, actor string) error {
	for key, value := range updates {
		if err := s.UpdateSetting(ctx, key, value, actor); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateTenant(ctx context.Context, name string, planID uuid.UUID, actor string) (*models.Tenant, error) {
	plan, err := s.store.GetPlan(ctx, planID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if plan == nil {
		return nil, app.BadRequest("plan_id does not exist")
	}
	t := &models.Tenant{Name: name, PlanID: planID}
	if err := s.store.CreateTenant(ctx, t); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "tenant.create",
		ResourceType: "tenant",
		ResourceID:   app.UUIDPtr(t.ID),
		Actor:        actor,
		Details:      app.MustJSON(map[string]any{"name": t.Name, "plan_id": t.PlanID}),
	})
	return t, nil
}

func (s *Service) UpdateTenantOverride(ctx context.Context, tenantID uuid.UUID, body models.TenantOverride, actor string) (*models.TenantOverride, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if tenant == nil {
		return nil, app.NotFound("tenant not found")
	}
	body.TenantID = tenantID
	if err := s.store.UpsertOverride(ctx, &body); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(tenantID),
		Action:       "tenant.override.upsert",
		ResourceType: "tenant_override",
		ResourceID:   app.UUIDPtr(body.ID),
		Actor:        actor,
		Details:      app.MustJSON(body),
	})
	return &body, nil
}

func (s *Service) DeleteTenant(ctx context.Context, id uuid.UUID, actor string) error {
	if err := s.store.DeleteTenant(ctx, id); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "tenant.delete",
		ResourceType: "tenant",
		ResourceID:   app.UUIDPtr(id),
		Actor:        actor,
		Details:      app.MustJSON(map[string]any{"tenant_id": id}),
	})
	return nil
}

func (s *Service) CreatePlan(ctx context.Context, p *models.Plan, actor string) (*models.Plan, error) {
	if err := s.store.CreatePlan(ctx, p); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "plan.create",
		ResourceType: "plan",
		ResourceID:   app.UUIDPtr(p.ID),
		Actor:        actor,
		Details:      app.MustJSON(map[string]any{"name": p.Name}),
	})
	return p, nil
}

func (s *Service) UpdatePlan(ctx context.Context, p *models.Plan, actor string) (*models.Plan, error) {
	if err := s.store.UpdatePlan(ctx, p); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "plan.update",
		ResourceType: "plan",
		ResourceID:   app.UUIDPtr(p.ID),
		Actor:        actor,
		Details:      app.MustJSON(p),
	})
	return p, nil
}

func (s *Service) DeletePlan(ctx context.Context, id uuid.UUID, actor string) error {
	if err := s.store.DeletePlan(ctx, id); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "plan.delete",
		ResourceType: "plan",
		ResourceID:   app.UUIDPtr(id),
		Actor:        actor,
	})
	return nil
}

func (s *Service) CreateAPIKey(ctx context.Context, tenantID uuid.UUID, label string, scopes []string, actor string, callerPerm *models.EffectivePermission, callerUserID *uuid.UUID, allowedZoneIDs []uuid.UUID) (*APIKeyIssueResult, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if tenant == nil {
		return nil, app.NotFound("tenant not found")
	}
	scopes, err = normalizeAPIKeyScopes(scopes)
	if err != nil {
		return nil, err
	}

	// Enforce scope restrictions for non-admin callers
	if callerPerm != nil {
		scopeSet := make(map[string]struct{}, len(scopes))
		for _, sc := range scopes {
			scopeSet[sc] = struct{}{}
		}
		if _, ok := scopeSet["send:write"]; ok {
			if !callerPerm.CanSend {
				return nil, app.Forbidden("cannot create api key with send:write scope: sending not allowed")
			}
		}
		if _, ok := scopeSet["send:read"]; ok {
			if !callerPerm.CanSend {
				return nil, app.Forbidden("cannot create api key with send:read scope: sending not allowed")
			}
		}
		if _, ok := scopeSet["domains:write"]; ok {
			if !callerPerm.CanCreateDomains {
				return nil, app.Forbidden("cannot create api key with domains:write scope: domain creation not allowed")
			}
		}
		if _, ok := scopeSet["routes:write"]; ok {
			if !callerPerm.CanCreateRoutes {
				return nil, app.Forbidden("cannot create api key with routes:write scope: route creation not allowed")
			}
		}
		// There is no profile-level mailbox/message write capability today.
		// Do not let a regular user mint broad write credentials that outlive
		// interactive permission checks; tenant/platform admins can still
		// create integration keys from admin endpoints.
		if _, ok := scopeSet["mailboxes:write"]; ok {
			return nil, app.Forbidden("cannot create api key with mailboxes:write scope: admin approval required")
		}
		if _, ok := scopeSet["messages:write"]; ok {
			return nil, app.Forbidden("cannot create api key with messages:write scope: admin approval required")
		}
		if _, ok := scopeSet["webhooks:read"]; ok {
			return nil, app.Forbidden("cannot create api key with webhooks:read scope: admin approval required")
		}
		if _, ok := scopeSet["webhooks:write"]; ok {
			return nil, app.Forbidden("cannot create api key with webhooks:write scope: admin approval required")
		}
	}

	// Validate allowed_zone_ids: each zone must belong to the tenant.
	if len(allowedZoneIDs) > 0 {
		for _, zoneID := range allowedZoneIDs {
			zone, err := s.store.GetZone(ctx, zoneID)
			if err != nil {
				return nil, app.Internal(err)
			}
			if zone == nil {
				return nil, app.BadRequest(fmt.Sprintf("zone %s not found", zoneID))
			}
			if zone.TenantID != tenantID {
				return nil, app.Forbidden(fmt.Sprintf("zone %s does not belong to tenant", zoneID))
			}
		}
		// Subset check: non-admin caller can't exceed their own zone restrictions.
		if callerPerm != nil && len(callerPerm.AllowedZoneIDs) > 0 {
			allowed := make(map[uuid.UUID]struct{}, len(callerPerm.AllowedZoneIDs))
			for _, z := range callerPerm.AllowedZoneIDs {
				allowed[z] = struct{}{}
			}
			for _, z := range allowedZoneIDs {
				if _, ok := allowed[z]; !ok {
					return nil, app.Forbidden(fmt.Sprintf("zone %s is not in your allowed zone list", z))
				}
			}
		}
	}

	raw := generateKey()
	hash := sha256.Sum256([]byte(raw))
	k := &models.TenantAPIKey{
		TenantID:    tenantID,
		KeyHash:     hex.EncodeToString(hash[:]),
		KeyPrefix:   raw[:12],
		Label:       label,
		Scopes:      scopes,
		OwnerUserID: callerUserID,
	}
	// Use explicitly provided zone IDs, or inherit from caller permission.
	if len(allowedZoneIDs) > 0 {
		k.AllowedZoneIDs = append([]uuid.UUID(nil), allowedZoneIDs...)
	} else if callerPerm != nil && len(callerPerm.AllowedZoneIDs) > 0 {
		k.AllowedZoneIDs = append([]uuid.UUID(nil), callerPerm.AllowedZoneIDs...)
	}
	if err := s.store.CreateAPIKey(ctx, k); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(tenantID),
		Action:       "api_key.create",
		ResourceType: "tenant_api_key",
		ResourceID:   app.UUIDPtr(k.ID),
		Actor:        actor,
		Details:      app.MustJSON(map[string]any{"label": k.Label, "key_prefix": k.KeyPrefix, "scopes": k.Scopes, "owner_user_id": callerUserID}),
	})
	return &APIKeyIssueResult{
		ID:        k.ID,
		Key:       raw,
		KeyPrefix: k.KeyPrefix,
		Label:     k.Label,
		Scopes:    k.Scopes,
		CreatedAt: k.CreatedAt,
	}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error) {
	items, err := s.store.ListAPIKeys(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) ListAPIKeysByOwner(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error) {
	items, err := s.store.ListAPIKeysByOwner(ctx, tenantID, ownerUserID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) DeleteAPIKeyForTenant(ctx context.Context, tenantID uuid.UUID, keyID uuid.UUID, actor string, callerUserID *uuid.UUID) error {
	key, err := s.store.GetAPIKey(ctx, keyID)
	if err != nil {
		return app.Internal(err)
	}
	if key == nil {
		return app.NotFound("api key not found")
	}
	if key.TenantID != tenantID {
		return app.Forbidden("api key belongs to another tenant")
	}
	// Non-admin callers can only delete their own keys
	if callerUserID != nil {
		if key.OwnerUserID == nil || *key.OwnerUserID != *callerUserID {
			return app.Forbidden("cannot delete api key owned by another user")
		}
	}
	return s.DeleteAPIKey(ctx, keyID, actor)
}

func (s *Service) DeleteAPIKey(ctx context.Context, keyID uuid.UUID, actor string) error {
	if err := s.store.DeleteAPIKey(ctx, keyID); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "api_key.delete",
		ResourceType: "tenant_api_key",
		ResourceID:   app.UUIDPtr(keyID),
		Actor:        actor,
	})
	return nil
}

func (s *Service) Stats(ctx context.Context) (*models.SystemStats, error) {
	tenants, err := s.store.ListTenants(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	plans, err := s.store.ListPlans(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	domains, err := s.store.CountAllZones(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	mailboxes, err := s.store.CountAllMailboxes(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	messages, err := s.store.CountAllMessages(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	audit, err := s.store.ListAuditEntries(ctx, 12)
	if err != nil {
		return nil, app.Internal(err)
	}
	deadLetters := []models.DeadLetter{}
	deadLetterSize := 0
	webhooksEnabled := false
	if s.dispatcher != nil {
		webhooksEnabled = s.dispatcher.Enabled()
		deadLetters = append(deadLetters, s.dispatcher.DeadLetters(10)...)
		deadLetterSize = s.dispatcher.DeadLetterSize()
	}
	return &models.SystemStats{
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
	}, nil
}

func (s *Service) ListAudit(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error) {
	items, total, err := s.store.ListAuditEntriesPaged(ctx, pg)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	if items == nil {
		items = []*models.AuditEntry{}
	}
	return items, total, nil
}

func (s *Service) ListIngestJobs(ctx context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error) {
	items, total, err := s.store.ListIngestJobs(ctx, pg, state, source, recipient)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	if items == nil {
		items = []*models.IngestJob{}
	}
	return items, total, nil
}

func (s *Service) ListWebhookDeliveries(ctx context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error) {
	items, total, err := s.store.ListWebhookDeliveries(ctx, pg, state, eventType, url)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	if items == nil {
		items = []*models.WebhookDelivery{}
	}
	return items, total, nil
}

func (s *Service) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	p, err := s.store.GetSMTPPolicy(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	if p == nil {
		p = &s.defaultPolicy
	}
	return p, nil
}

func (s *Service) UpdateSMTPPolicy(ctx context.Context, policy *models.SMTPPolicy, actor string) (*models.SMTPPolicy, error) {
	if err := s.store.UpsertSMTPPolicy(ctx, policy); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		Action:       "smtp_policy.update",
		ResourceType: "smtp_policy",
		Actor:        actor,
		Details:      app.MustJSON(policy),
	})
	// Drop the ingest policy cache so the new policy takes effect immediately
	// instead of waiting out the 2s TTL.
	if s.policyInvalidator != nil {
		s.policyInvalidator.InvalidateSMTPPolicy()
	}
	return policy, nil
}

func generateKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return fmt.Sprintf("tb_%s", hex.EncodeToString(b))
}
