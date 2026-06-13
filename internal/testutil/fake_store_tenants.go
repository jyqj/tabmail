package testutil

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) RegisterAPIKey(raw string, tenant *models.Tenant, scopes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kid := uuid.New()
	s.apiRaw[raw] = resolvedAPIKey{Tenant: cloneTenant(tenant), Scopes: append([]string(nil), scopes...), KeyID: kid}
	s.apiKeys[kid] = &models.TenantAPIKey{
		ID:       kid,
		TenantID: tenant.ID,
		Label:    "test-key",
		Scopes:   append([]string(nil), scopes...),
	}
}

func (s *FakeStore) CreatePlan(_ context.Context, p *models.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = cp.CreatedAt
	}
	s.plans[cp.ID] = &cp
	p.ID = cp.ID
	p.CreatedAt = cp.CreatedAt
	p.UpdatedAt = cp.UpdatedAt
	return nil
}

func (s *FakeStore) GetPlan(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.plans[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListPlans(_ context.Context) ([]*models.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*models.Plan, 0, len(s.plans))
	for _, p := range s.plans {
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *FakeStore) UpdatePlan(_ context.Context, p *models.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[p.ID]; !ok {
		return errors.New("plan not found")
	}
	cp := *p
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = time.Now()
	}
	s.plans[p.ID] = &cp
	return nil
}

func (s *FakeStore) DeletePlan(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.plans, id)
	return nil
}

func (s *FakeStore) CreateTenant(_ context.Context, t *models.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.tenants[cp.ID] = &cp
	t.ID = cp.ID
	t.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) GetTenant(_ context.Context, id uuid.UUID) (*models.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tenants[id]; ok {
		return cloneTenant(t), nil
	}
	return nil, nil
}

func (s *FakeStore) ListTenants(_ context.Context) ([]*models.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*models.Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, cloneTenant(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *FakeStore) DeleteTenant(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tenants, id)
	delete(s.overrides, id)
	return nil
}

func (s *FakeStore) UpsertOverride(_ context.Context, o *models.TenantOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *o
	if existing, ok := s.overrides[cp.TenantID]; ok {
		cp.ID = existing.ID
	} else if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	cp.UpdatedAt = time.Now()
	s.overrides[cp.TenantID] = &cp
	o.ID = cp.ID
	o.UpdatedAt = cp.UpdatedAt
	return nil
}

func (s *FakeStore) GetOverride(_ context.Context, tenantID uuid.UUID) (*models.TenantOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.overrides[tenantID]; ok {
		cp := *o
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error) {
	t, err := s.GetTenant(ctx, tenantID)
	if err != nil || t == nil {
		return nil, err
	}
	p, err := s.GetPlan(ctx, t.PlanID)
	if err != nil || p == nil {
		return nil, err
	}
	ec := &models.EffectiveConfig{
		MaxDomains:            p.MaxDomains,
		MaxMailboxesPerDomain: p.MaxMailboxesPerDomain,
		MaxMessagesPerMailbox: p.MaxMessagesPerMailbox,
		MaxMessageBytes:       p.MaxMessageBytes,
		RetentionHours:        p.RetentionHours,
		RPMLimit:              p.RPMLimit,
		DailyQuota:            p.DailyQuota,
	}
	o, _ := s.GetOverride(ctx, tenantID)
	if o != nil {
		if o.MaxDomains != nil {
			ec.MaxDomains = *o.MaxDomains
		}
		if o.MaxMailboxesPerDomain != nil {
			ec.MaxMailboxesPerDomain = *o.MaxMailboxesPerDomain
		}
		if o.MaxMessagesPerMailbox != nil {
			ec.MaxMessagesPerMailbox = *o.MaxMessagesPerMailbox
		}
		if o.MaxMessageBytes != nil {
			ec.MaxMessageBytes = *o.MaxMessageBytes
		}
		if o.RetentionHours != nil {
			ec.RetentionHours = *o.RetentionHours
		}
		if o.RPMLimit != nil {
			ec.RPMLimit = *o.RPMLimit
		}
		if o.DailyQuota != nil {
			ec.DailyQuota = *o.DailyQuota
		}
	}
	return ec, nil
}

func (s *FakeStore) CreateAPIKey(_ context.Context, k *models.TenantAPIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *k
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.apiKeys[cp.ID] = &cp
	k.ID = cp.ID
	k.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) GetAPIKey(_ context.Context, id uuid.UUID) (*models.TenantAPIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.apiKeys[id]; ok {
		cp := *k
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListAPIKeys(_ context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.TenantAPIKey
	for _, k := range s.apiKeys {
		if k.TenantID == tenantID {
			cp := *k
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *FakeStore) ListAPIKeysByOwner(_ context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.TenantAPIKey
	for _, k := range s.apiKeys {
		if k.TenantID == tenantID && k.OwnerUserID != nil && *k.OwnerUserID == ownerUserID {
			cp := *k
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *FakeStore) DeleteAPIKey(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apiKeys, id)
	return nil
}

func (s *FakeStore) ResolveAPIKey(_ context.Context, rawKey string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rk, ok := s.apiRaw[rawKey]; ok {
		kid := rk.KeyID
		var allowedZoneIDs []uuid.UUID
		var ownerUserID *uuid.UUID
		if k, exists := s.apiKeys[kid]; exists {
			if len(k.AllowedZoneIDs) > 0 {
				allowedZoneIDs = append([]uuid.UUID(nil), k.AllowedZoneIDs...)
			}
			ownerUserID = k.OwnerUserID
		}
		return cloneTenant(rk.Tenant), &kid, append([]string(nil), rk.Scopes...), allowedZoneIDs, ownerUserID, nil
	}
	return nil, nil, nil, nil, nil, nil
}

func (s *FakeStore) TouchAPIKey(_ context.Context, id uuid.UUID, ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.apiKeys[id]; ok {
		now := time.Now()
		k.LastUsedAt = &now
		if ip != "" {
			k.LastUsedIP = &ip
		}
	}
	return nil
}

func cloneTenant(t *models.Tenant) *models.Tenant {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}
