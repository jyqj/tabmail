package testutil

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func (s *FakeStore) CreateZone(_ context.Context, z *models.DomainZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *z
	if cp.Visibility == "" {
		cp.Visibility = models.VisibilityPrivate
	}
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.zones[cp.ID] = &cp
	z.ID = cp.ID
	z.Visibility = cp.Visibility
	z.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if z, ok := s.zones[id]; ok {
		cp := *z
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) GetZoneByDomain(_ context.Context, domain string) (*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, z := range s.zones {
		if z.Domain == domain {
			cp := *z
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *FakeStore) ListZones(_ context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.DomainZone
	for _, z := range s.zones {
		if z.TenantID == tenantID {
			cp := *z
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

// ListZonesScoped mirrors the SQL: tenant_id always, plus the zone allowlist
// (ZoneIDs) when AllZones is false, plus owner_user_id when OwnerUserID is set.
func (s *FakeStore) ListZonesScoped(_ context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.AllZones && len(scope.ZoneIDs) == 0 {
		return []*models.DomainZone{}, nil
	}
	allowed := make(map[uuid.UUID]struct{}, len(scope.ZoneIDs))
	for _, id := range scope.ZoneIDs {
		allowed[id] = struct{}{}
	}
	var out []*models.DomainZone
	for _, z := range s.zones {
		if z.TenantID != scope.TenantID {
			continue
		}
		if !scope.AllZones {
			if _, ok := allowed[z.ID]; !ok {
				continue
			}
		}
		if scope.OwnerUserID != nil {
			if z.OwnerUserID == nil || *z.OwnerUserID != *scope.OwnerUserID {
				continue
			}
		}
		cp := *z
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (s *FakeStore) ListAllZones(_ context.Context) ([]*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.DomainZone
	for _, z := range s.zones {
		cp := *z
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (s *FakeStore) ListZonesByVisibilities(_ context.Context, visibilities []models.ResourceVisibility) ([]*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := make(map[models.ResourceVisibility]struct{}, len(visibilities))
	for _, v := range visibilities {
		allowed[v] = struct{}{}
	}
	var out []*models.DomainZone
	for _, z := range s.zones {
		if _, ok := allowed[z.Visibility]; ok {
			cp := *z
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (s *FakeStore) ListPublicZones(_ context.Context) ([]*models.DomainZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.DomainZone
	for _, z := range s.zones {
		if z.Visibility == models.VisibilityPublic {
			cp := *z
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (s *FakeStore) UpdateZone(_ context.Context, z *models.DomainZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *z
	if cp.Visibility == "" {
		cp.Visibility = models.VisibilityPrivate
	}
	s.zones[z.ID] = &cp
	return nil
}

func (s *FakeStore) DeleteZone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.zones, id)
	return nil
}

func (s *FakeStore) CountZones(_ context.Context, tenantID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, z := range s.zones {
		if z.TenantID == tenantID {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountAllZones(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.zones), nil
}

func (s *FakeStore) CreateRoute(_ context.Context, r *models.DomainRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.routes[cp.ID] = &cp
	r.ID = cp.ID
	r.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) GetRoute(_ context.Context, id uuid.UUID) (*models.DomainRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.routes[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListRoutes(_ context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.DomainRoute
	for _, r := range s.routes {
		if r.ZoneID == zoneID {
			cp := *r
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *FakeStore) DeleteRoute(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.routes, id)
	return nil
}

func (s *FakeStore) FindMatchingRoutes(ctx context.Context, domain string, tenantID *uuid.UUID) ([]*models.DomainRoute, error) {
	zone, _ := s.GetZoneByDomain(ctx, domain)
	if zone == nil {
		return nil, nil
	}
	if tenantID != nil && zone.TenantID != *tenantID {
		return nil, nil
	}
	return s.ListRoutes(ctx, zone.ID)
}
