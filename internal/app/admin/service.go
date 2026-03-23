package adminapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
)

type Store interface {
	app.AuditStore
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
	ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error)
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
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
	logger        zerolog.Logger
}

type APIKeyIssueResult struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	KeyPrefix string    `json:"key_prefix"`
	Label     string    `json:"label"`
	Scopes    []string  `json:"scopes"`
	CreatedAt any       `json:"created_at"`
}

func NewService(s Store, dispatcher *hooks.Dispatcher, defaultPolicy models.SMTPPolicy, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, defaultPolicy: defaultPolicy, logger: logger.With().Str("service", "admin").Logger()}
}

func (s *Service) ListTenants(ctx context.Context) ([]*models.Tenant, error) {
	items, err := s.store.ListTenants(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
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

func (s *Service) GetEffectiveConfig(ctx context.Context, id uuid.UUID) (*models.EffectiveConfig, error) {
	cfg, err := s.store.EffectiveConfig(ctx, id)
	if err != nil {
		return nil, app.Internal(err)
	}
	return cfg, nil
}

func (s *Service) ListPlans(ctx context.Context) ([]*models.Plan, error) {
	items, err := s.store.ListPlans(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
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

func (s *Service) CreateAPIKey(ctx context.Context, tenantID uuid.UUID, label string, scopes []string, actor string) (*APIKeyIssueResult, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if tenant == nil {
		return nil, app.NotFound("tenant not found")
	}
	raw := generateKey()
	hash := sha256.Sum256([]byte(raw))
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}
	k := &models.TenantAPIKey{
		TenantID:  tenantID,
		KeyHash:   hex.EncodeToString(hash[:]),
		KeyPrefix: raw[:12],
		Label:     label,
		Scopes:    scopes,
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
		Details:      app.MustJSON(map[string]any{"label": k.Label, "key_prefix": k.KeyPrefix, "scopes": k.Scopes}),
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
	return policy, nil
}

func generateKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return fmt.Sprintf("tb_%s", hex.EncodeToString(b))
}
