package testutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

type resolvedAPIKey struct {
	Tenant *models.Tenant
	Scopes []string
}

type FakeStore struct {
	mu sync.Mutex

	plans      map[uuid.UUID]*models.Plan
	tenants    map[uuid.UUID]*models.Tenant
	overrides  map[uuid.UUID]*models.TenantOverride
	apiKeys    map[uuid.UUID]*models.TenantAPIKey
	apiRaw     map[string]resolvedAPIKey
	smtpPolicy *models.SMTPPolicy

	zones     map[uuid.UUID]*models.DomainZone
	routes    map[uuid.UUID]*models.DomainRoute
	mailboxes map[uuid.UUID]*models.Mailbox
	messages  map[uuid.UUID]*models.Message
	audits    []*models.AuditEntry
	monitor   []*models.MonitorEvent
}

func NewFakeStore() *FakeStore {
	return &FakeStore{
		plans:     map[uuid.UUID]*models.Plan{},
		tenants:   map[uuid.UUID]*models.Tenant{},
		overrides: map[uuid.UUID]*models.TenantOverride{},
		apiKeys:   map[uuid.UUID]*models.TenantAPIKey{},
		apiRaw:    map[string]resolvedAPIKey{},
		zones:     map[uuid.UUID]*models.DomainZone{},
		routes:    map[uuid.UUID]*models.DomainRoute{},
		mailboxes: map[uuid.UUID]*models.Mailbox{},
		messages:  map[uuid.UUID]*models.Message{},
		monitor:   []*models.MonitorEvent{},
	}
}

func (s *FakeStore) RegisterAPIKey(raw string, tenant *models.Tenant, scopes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiRaw[raw] = resolvedAPIKey{Tenant: cloneTenant(tenant), Scopes: append([]string(nil), scopes...)}
}

func (s *FakeStore) SeedPlan(p *models.Plan) {
	_ = s.CreatePlan(context.Background(), p)
}

func (s *FakeStore) SeedTenant(t *models.Tenant) {
	_ = s.CreateTenant(context.Background(), t)
}

func (s *FakeStore) SeedZone(z *models.DomainZone) {
	_ = s.CreateZone(context.Background(), z)
}

func (s *FakeStore) SeedRoute(r *models.DomainRoute) {
	_ = s.CreateRoute(context.Background(), r)
}

func (s *FakeStore) SeedMailbox(m *models.Mailbox) {
	_ = s.CreateMailbox(context.Background(), m)
}

func (s *FakeStore) SeedMessage(m *models.Message) {
	_ = s.CreateMessage(context.Background(), m)
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
	if cp.ID == uuid.Nil {
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

func (s *FakeStore) DeleteAPIKey(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apiKeys, id)
	return nil
}

func (s *FakeStore) ResolveAPIKey(_ context.Context, rawKey string) (*models.Tenant, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rk, ok := s.apiRaw[rawKey]; ok {
		return cloneTenant(rk.Tenant), append([]string(nil), rk.Scopes...), nil
	}
	return nil, nil, nil
}

func (s *FakeStore) TouchAPIKey(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.apiKeys[id]; ok {
		now := time.Now()
		k.LastUsedAt = &now
	}
	return nil
}

func (s *FakeStore) CreateZone(_ context.Context, z *models.DomainZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *z
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.zones[cp.ID] = &cp
	z.ID = cp.ID
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

func (s *FakeStore) UpdateZone(_ context.Context, z *models.DomainZone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *z
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

func (s *FakeStore) FindMatchingRoutes(ctx context.Context, domain string) ([]*models.DomainRoute, error) {
	zone, _ := s.GetZoneByDomain(ctx, domain)
	if zone == nil {
		return nil, nil
	}
	return s.ListRoutes(ctx, zone.ID)
}

func (s *FakeStore) GetSMTPPolicy(_ context.Context) (*models.SMTPPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.smtpPolicy == nil {
		return nil, nil
	}
	cp := *s.smtpPolicy
	cp.AcceptDomains = append([]string(nil), cp.AcceptDomains...)
	cp.RejectDomains = append([]string(nil), cp.RejectDomains...)
	cp.StoreDomains = append([]string(nil), cp.StoreDomains...)
	cp.DiscardDomains = append([]string(nil), cp.DiscardDomains...)
	cp.RejectOriginDomains = append([]string(nil), cp.RejectOriginDomains...)
	return &cp, nil
}

func (s *FakeStore) UpsertSMTPPolicy(_ context.Context, p *models.SMTPPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	cp.UpdatedAt = time.Now().UTC()
	cp.AcceptDomains = append([]string(nil), cp.AcceptDomains...)
	cp.RejectDomains = append([]string(nil), cp.RejectDomains...)
	cp.StoreDomains = append([]string(nil), cp.StoreDomains...)
	cp.DiscardDomains = append([]string(nil), cp.DiscardDomains...)
	cp.RejectOriginDomains = append([]string(nil), cp.RejectOriginDomains...)
	s.smtpPolicy = &cp
	p.UpdatedAt = cp.UpdatedAt
	return nil
}

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
	s.mailboxes[cp.ID] = &cp
	m.ID = cp.ID
	m.CreatedAt = cp.CreatedAt
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

func (s *FakeStore) ListMailboxes(_ context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*models.Mailbox
	for _, m := range s.mailboxes {
		if m.TenantID == tenantID {
			cp := *m
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return paginateMailboxes(list, pg), len(list), nil
}

func (s *FakeStore) ListMailboxesByZone(_ context.Context, zoneID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*models.Mailbox
	for _, m := range s.mailboxes {
		if m.ZoneID == zoneID {
			cp := *m
			list = append(list, &cp)
		}
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

func (s *FakeStore) CreateMessage(_ context.Context, m *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	s.messages[cp.ID] = &cp
	m.ID = cp.ID
	m.ReceivedAt = cp.ReceivedAt
	return nil
}

func (s *FakeStore) GetMessage(_ context.Context, id uuid.UUID) (*models.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.messages[id]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListMessages(_ context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*models.Message
	for _, m := range s.messages {
		if m.MailboxID == mailboxID {
			cp := *m
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ReceivedAt.After(list[j].ReceivedAt) })
	return paginateMessages(list, pg), len(list), nil
}

func (s *FakeStore) MarkSeen(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.messages[id]; ok {
		m.Seen = true
	}
	return nil
}

func (s *FakeStore) DeleteMessage(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messages, id)
	return nil
}

func (s *FakeStore) PurgeMailbox(_ context.Context, mailboxID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, m := range s.messages {
		if m.MailboxID == mailboxID {
			delete(s.messages, id)
		}
	}
	return nil
}

func (s *FakeStore) CountMessages(_ context.Context, mailboxID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.MailboxID == mailboxID {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountTenantMessagesSince(_ context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.TenantID == tenantID && !m.ReceivedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountAllMessages(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages), nil
}

func (s *FakeStore) DeleteExpiredMessages(_ context.Context, before time.Time, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := make([]*models.Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.ExpiresAt.Before(before) {
			cp := *m
			expired = append(expired, &cp)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].ExpiresAt.Equal(expired[j].ExpiresAt) {
			return expired[i].ID.String() < expired[j].ID.String()
		}
		return expired[i].ExpiresAt.Before(expired[j].ExpiresAt)
	})

	n := 0
	for _, m := range expired {
		if n >= limit {
			break
		}
		delete(s.messages, m.ID)
		n++
	}
	return n, nil
}

func (s *FakeStore) ListExpiredObjectKeys(_ context.Context, before time.Time, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := make([]*models.Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.ExpiresAt.Before(before) && m.RawObjectKey != "" {
			cp := *m
			expired = append(expired, &cp)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].ExpiresAt.Equal(expired[j].ExpiresAt) {
			return expired[i].ID.String() < expired[j].ID.String()
		}
		return expired[i].ExpiresAt.Before(expired[j].ExpiresAt)
	})

	var out []string
	for _, m := range expired {
		if len(out) >= limit {
			break
		}
		out = append(out, m.RawObjectKey)
	}
	return out, nil
}

func (s *FakeStore) InsertAudit(_ context.Context, e *models.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.audits = append(s.audits, &cp)
	return nil
}

func (s *FakeStore) ListAuditEntries(_ context.Context, limit int) ([]*models.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.audits) {
		limit = len(s.audits)
	}
	out := make([]*models.AuditEntry, 0, limit)
	for i := len(s.audits) - 1; i >= 0 && len(out) < limit; i-- {
		cp := *s.audits[i]
		out = append(out, &cp)
	}
	return out, nil
}

func (s *FakeStore) CreateMonitorEvent(_ context.Context, e *models.MonitorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.At.IsZero() {
		cp.At = time.Now().UTC()
	}
	s.monitor = append(s.monitor, &cp)
	return nil
}

func (s *FakeStore) ListMonitorEvents(_ context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var filtered []*models.MonitorEvent
	for _, e := range s.monitor {
		if eventType != "" && e.Type != eventType {
			continue
		}
		if mailbox != "" && !strings.Contains(strings.ToLower(e.Mailbox), strings.ToLower(mailbox)) {
			continue
		}
		if sender != "" && !strings.Contains(strings.ToLower(e.Sender), strings.ToLower(sender)) {
			continue
		}
		cp := *e
		filtered = append(filtered, &cp)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].At.After(filtered[j].At) })
	total := len(filtered)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= len(filtered) {
		return []*models.MonitorEvent{}, total, nil
	}
	end := start + pg.PerPage
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}

func (s *FakeStore) Close() error { return nil }

type MemoryObjectStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{data: map[string][]byte{}}
}

func (m *MemoryObjectStore) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.data[key] = b
	return nil
}

func (m *MemoryObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *MemoryObjectStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func cloneTenant(t *models.Tenant) *models.Tenant {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
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

func paginateMessages(list []*models.Message, pg models.Page) []*models.Message {
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= len(list) {
		return []*models.Message{}
	}
	end := start + pg.PerPage
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}
