package testutil

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func (s *FakeStore) CreateMailbox(_ context.Context, m *models.Mailbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.mailboxes {
		if existing.FullAddress == m.FullAddress {
			return errors.New("duplicate mailbox")
		}
	}
	cp := *m
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	if cp.MessageCount < 0 {
		cp.MessageCount = 0
	}
	s.mailboxes[cp.ID] = &cp
	m.ID = cp.ID
	m.CreatedAt = cp.CreatedAt
	m.MessageCount = cp.MessageCount
	return nil
}

func (s *FakeStore) GetMailbox(_ context.Context, id uuid.UUID) (*models.Mailbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.mailboxes[id]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) GetMailboxByAddress(_ context.Context, address string) (*models.Mailbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address = strings.ToLower(strings.TrimSpace(address))
	for _, m := range s.mailboxes {
		if m.FullAddress == address {
			cp := *m
			return &cp, nil
		}
	}
	return nil, nil
}

func (v *fakeTenantView) GetMailbox(_ context.Context, id uuid.UUID) (*models.Mailbox, error) {
	v.store.mu.Lock()
	defer v.store.mu.Unlock()
	if m, ok := v.store.mailboxes[id]; ok && m.TenantID == v.tenantID {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (v *fakeTenantView) GetMailboxByAddress(_ context.Context, addr string) (*models.Mailbox, error) {
	v.store.mu.Lock()
	defer v.store.mu.Unlock()
	addr = strings.ToLower(strings.TrimSpace(addr))
	for _, m := range v.store.mailboxes {
		if m.FullAddress == addr && m.TenantID == v.tenantID {
			cp := *m
			return &cp, nil
		}
	}
	return nil, nil
}

// ListMailboxesScoped mirrors the SQL: tenant_id always, plus zone allowlist
// (ZoneIDs) when AllZones is false, plus owner_user_id (via zone lookup) when
// OwnerUserID is set. The owner dimension reads zone.owner_user_id exactly like
// the SQL subquery JOIN to domain_zones.
func (s *FakeStore) ListMailboxesScoped(_ context.Context, scope authz.ZoneListFilter, pg models.Page) ([]*models.Mailbox, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.AllZones && len(scope.ZoneIDs) == 0 {
		return []*models.Mailbox{}, 0, nil
	}
	allowed := make(map[uuid.UUID]struct{}, len(scope.ZoneIDs))
	for _, id := range scope.ZoneIDs {
		allowed[id] = struct{}{}
	}
	// Owner dimension: resolve which zones this user owns, then restrict
	// membership to those zones (matches the SQL subquery).
	var ownerZones map[uuid.UUID]struct{}
	if scope.OwnerUserID != nil {
		ownerZones = map[uuid.UUID]struct{}{}
		for _, z := range s.zones {
			if z.OwnerUserID != nil && *z.OwnerUserID == *scope.OwnerUserID {
				ownerZones[z.ID] = struct{}{}
			}
		}
	}
	var list []*models.Mailbox
	for _, m := range s.mailboxes {
		if m.TenantID != scope.TenantID {
			continue
		}
		if !scope.AllZones {
			if _, ok := allowed[m.ZoneID]; !ok {
				continue
			}
		}
		if ownerZones != nil {
			if _, ok := ownerZones[m.ZoneID]; !ok {
				continue
			}
		}
		cp := *m
		list = append(list, &cp)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return paginateMailboxes(list, pg), len(list), nil
}

func (s *FakeStore) DeleteMailbox(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mailboxes, id)
	for mid, m := range s.messages {
		if m.MailboxID == id {
			delete(s.messages, mid)
		}
	}
	return nil
}

func (s *FakeStore) CountMailboxes(_ context.Context, zoneID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.mailboxes {
		if m.ZoneID == zoneID {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountAllMailboxes(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mailboxes), nil
}

func (s *FakeStore) ListMailboxObjectKeys(_ context.Context, mailboxID uuid.UUID) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.messages {
		if m.MailboxID == mailboxID && m.RawObjectKey != "" {
			out = append(out, m.RawObjectKey)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *FakeStore) ListZoneObjectKeys(_ context.Context, zoneID uuid.UUID) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{})
	var out []string
	for _, m := range s.messages {
		if m.RawObjectKey == "" {
			continue
		}
		mb, ok := s.mailboxes[m.MailboxID]
		if !ok || mb.ZoneID != zoneID {
			continue
		}
		if _, dup := seen[m.RawObjectKey]; dup {
			continue
		}
		seen[m.RawObjectKey] = struct{}{}
		out = append(out, m.RawObjectKey)
	}
	sort.Strings(out)
	return out, nil
}

func paginateMailboxes(list []*models.Mailbox, pg models.Page) []*models.Mailbox {
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= len(list) {
		return []*models.Mailbox{}
	}
	end := start + pg.PerPage
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}
