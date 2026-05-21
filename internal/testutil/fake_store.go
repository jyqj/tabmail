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

const fakeClaimLeaseDuration = 5 * time.Minute

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

	zones      map[uuid.UUID]*models.DomainZone
	routes     map[uuid.UUID]*models.DomainRoute
	mailboxes  map[uuid.UUID]*models.Mailbox
	messages   map[uuid.UUID]*models.Message
	audits     []*models.AuditEntry
	monitor    []*models.MonitorEvent
	outbox     map[uuid.UUID]*models.OutboxEvent
	deliveries map[uuid.UUID]*models.WebhookDelivery
	ingestJobs map[uuid.UUID]*models.IngestJob
	users      map[uuid.UUID]*models.User
	settings   map[string]*models.SystemSetting
}

func NewFakeStore() *FakeStore {
	return &FakeStore{
		plans:      map[uuid.UUID]*models.Plan{},
		tenants:    map[uuid.UUID]*models.Tenant{},
		overrides:  map[uuid.UUID]*models.TenantOverride{},
		apiKeys:    map[uuid.UUID]*models.TenantAPIKey{},
		apiRaw:     map[string]resolvedAPIKey{},
		zones:      map[uuid.UUID]*models.DomainZone{},
		routes:     map[uuid.UUID]*models.DomainRoute{},
		mailboxes:  map[uuid.UUID]*models.Mailbox{},
		messages:   map[uuid.UUID]*models.Message{},
		monitor:    []*models.MonitorEvent{},
		outbox:     map[uuid.UUID]*models.OutboxEvent{},
		deliveries: map[uuid.UUID]*models.WebhookDelivery{},
		ingestJobs: map[uuid.UUID]*models.IngestJob{},
	}
}

func (s *FakeStore) RegisterAPIKey(raw string, tenant *models.Tenant, scopes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiRaw[raw] = resolvedAPIKey{Tenant: cloneTenant(tenant), Scopes: append([]string(nil), scopes...)}
}

func (s *FakeStore) ForceIngestJobUpdatedAt(id uuid.UUID, updatedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.UpdatedAt = updatedAt
	}
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

func (s *FakeStore) ListMailboxesByZones(_ context.Context, tenantID uuid.UUID, zoneIDs []uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := make(map[uuid.UUID]struct{}, len(zoneIDs))
	for _, id := range zoneIDs {
		allowed[id] = struct{}{}
	}
	var list []*models.Mailbox
	for _, m := range s.mailboxes {
		if m.TenantID != tenantID {
			continue
		}
		if _, ok := allowed[m.ZoneID]; !ok {
			continue
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
	if mb, ok := s.mailboxes[cp.MailboxID]; ok {
		mb.MessageCount++
	}
	m.ID = cp.ID
	m.ReceivedAt = cp.ReceivedAt
	return nil
}

func (s *FakeStore) CreateMessageWithQuota(_ context.Context, m *models.Message, maxMessages int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mb, ok := s.mailboxes[m.MailboxID]
	if !ok {
		return false, errors.New("mailbox not found")
	}
	if maxMessages > 0 && int(mb.MessageCount) >= maxMessages {
		return false, nil
	}

	cp := *m
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	s.messages[cp.ID] = &cp
	mb.MessageCount++
	m.ID = cp.ID
	m.ReceivedAt = cp.ReceivedAt
	return true, nil
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
	if msg, ok := s.messages[id]; ok {
		if mb, exists := s.mailboxes[msg.MailboxID]; exists && mb.MessageCount > 0 {
			mb.MessageCount--
		}
		delete(s.messages, id)
	}
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
	if mb, ok := s.mailboxes[mailboxID]; ok {
		mb.MessageCount = 0
	}
	return nil
}

func (s *FakeStore) CountMessages(_ context.Context, mailboxID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mb, ok := s.mailboxes[mailboxID]; ok {
		return int(mb.MessageCount), nil
	}
	return 0, nil
}

func (s *FakeStore) CountMessagesByObjectKey(_ context.Context, objectKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.RawObjectKey == objectKey {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountRawObjectReferences(_ context.Context, objectKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.RawObjectKey == objectKey {
			n++
		}
	}
	for _, job := range s.ingestJobs {
		if job.RawObjectKey == objectKey && (job.State == "pending" || job.State == "retry" || job.State == "processing") {
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
		if mb, ok := s.mailboxes[m.MailboxID]; ok && mb.MessageCount > 0 {
			mb.MessageCount--
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

func (s *FakeStore) DeleteExpiredMessagesReturningKeys(_ context.Context, before time.Time, limit int) (int, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []*models.Message
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
	if len(expired) > limit {
		expired = expired[:limit]
	}

	var keys []string
	for _, m := range expired {
		if mb, ok := s.mailboxes[m.MailboxID]; ok && mb.MessageCount > 0 {
			mb.MessageCount--
		}
		delete(s.messages, m.ID)
		if m.RawObjectKey != "" {
			keys = append(keys, m.RawObjectKey)
		}
	}
	return len(expired), keys, nil
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

func (f *FakeStore) ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error) {
	return nil, 0, nil
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

func (s *FakeStore) CreateOutboxEvent(_ context.Context, e *models.OutboxEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := time.Now().UTC()
	if cp.OccurredAt.IsZero() {
		cp.OccurredAt = now
	}
	if cp.NextAttemptAt.IsZero() {
		cp.NextAttemptAt = now
	}
	if cp.State == "" {
		cp.State = "pending"
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.outbox[cp.ID] = &cp
	e.ID = cp.ID
	return nil
}

func (s *FakeStore) ClaimOutboxEvents(_ context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var list []*models.OutboxEvent
	for _, e := range s.outbox {
		claimable := (e.State == "pending" || e.State == "retry") && !e.NextAttemptAt.After(now)
		expiredLease := e.State == "processing" && (e.LeaseUntil == nil || !e.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *e
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	for _, e := range list {
		stored := s.outbox[e.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.UpdatedAt = now
		e.State = stored.State
		e.Attempts = stored.Attempts
		e.ClaimedAt = stored.ClaimedAt
		e.LeaseUntil = stored.LeaseUntil
		e.UpdatedAt = stored.UpdatedAt
	}
	return list, nil
}

func (s *FakeStore) MarkOutboxEventDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.outbox[id]; ok {
		e.State = "done"
		e.ClaimedAt = nil
		e.LeaseUntil = nil
		e.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) MarkOutboxEventRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.outbox[id]; ok {
		e.State = "retry"
		e.LastError = lastError
		e.NextAttemptAt = nextAttemptAt
		e.ClaimedAt = nil
		e.LeaseUntil = nil
		e.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) CreateWebhookDeliveries(_ context.Context, event *models.OutboxEvent, urls []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, url := range urls {
		exists := false
		for _, d := range s.deliveries {
			if d.EventID == event.ID && d.URL == url {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		d := &models.WebhookDelivery{
			ID:            uuid.New(),
			EventID:       event.ID,
			URL:           url,
			EventType:     event.EventType,
			Payload:       append([]byte(nil), event.Payload...),
			State:         "pending",
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		s.deliveries[d.ID] = d
	}
	return nil
}

func (s *FakeStore) ClaimWebhookDeliveries(_ context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var list []*models.WebhookDelivery
	for _, d := range s.deliveries {
		claimable := (d.State == "pending" || d.State == "retry") && !d.NextAttemptAt.After(now)
		expiredLease := d.State == "processing" && (d.LeaseUntil == nil || !d.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *d
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	for _, d := range list {
		stored := s.deliveries[d.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		triedAt := now
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.LastTriedAt = &triedAt
		stored.UpdatedAt = now
		d.State = stored.State
		d.Attempts = stored.Attempts
		d.ClaimedAt = stored.ClaimedAt
		d.LeaseUntil = stored.LeaseUntil
		d.LastTriedAt = stored.LastTriedAt
		d.UpdatedAt = stored.UpdatedAt
	}
	return list, nil
}

func (s *FakeStore) MarkWebhookDeliveryDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.deliveries[id]; ok {
		now := time.Now().UTC()
		d.State = "delivered"
		d.ClaimedAt = nil
		d.LeaseUntil = nil
		d.DeliveredAt = &now
		d.UpdatedAt = now
	}
	return nil
}

func (s *FakeStore) MarkWebhookDeliveryRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.deliveries[id]; ok {
		d.State = "retry"
		if dead {
			d.State = "dead"
		}
		d.LastError = lastError
		d.NextAttemptAt = nextAttemptAt
		d.ClaimedAt = nil
		d.LeaseUntil = nil
		d.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) ListDeadWebhookDeliveries(_ context.Context, limit int) ([]models.DeadLetter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	var out []models.DeadLetter
	for _, d := range s.deliveries {
		if d.State != "dead" {
			continue
		}
		out = append(out, models.DeadLetter{
			ID:          d.ID.String(),
			URL:         d.URL,
			EventType:   d.EventType,
			Payload:     append([]byte(nil), d.Payload...),
			Attempts:    d.Attempts,
			LastError:   d.LastError,
			CreatedAt:   d.CreatedAt,
			LastTriedAt: derefTime(d.LastTriedAt),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *FakeStore) CountDeadWebhookDeliveries(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, d := range s.deliveries {
		if d.State == "dead" {
			total++
		}
	}
	return total, nil
}

func (s *FakeStore) ListWebhookDeliveries(_ context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.WebhookDelivery
	for _, d := range s.deliveries {
		if state != "" && d.State != state {
			continue
		}
		if eventType != "" && d.EventType != eventType {
			continue
		}
		if url != "" && !strings.Contains(strings.ToLower(d.URL), strings.ToLower(url)) {
			continue
		}
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= total {
		return []*models.WebhookDelivery{}, total, nil
	}
	end := start + pg.PerPage
	if end > total {
		end = total
	}
	return out[start:end], total, nil
}

func (s *FakeStore) CountWebhookDeliveriesByState(_ context.Context, states ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(states) == 0 {
		return len(s.deliveries), nil
	}
	set := make(map[string]struct{}, len(states))
	for _, state := range states {
		set[state] = struct{}{}
	}
	total := 0
	for _, d := range s.deliveries {
		if _, ok := set[d.State]; ok {
			total++
		}
	}
	return total, nil
}

func (s *FakeStore) CreateIngestJob(_ context.Context, job *models.IngestJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *job
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := time.Now().UTC()
	if cp.NextAttemptAt.IsZero() {
		cp.NextAttemptAt = now
	}
	if cp.State == "" {
		cp.State = "pending"
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.ingestJobs[cp.ID] = &cp
	job.ID = cp.ID
	return nil
}

func (s *FakeStore) ClaimIngestJobs(_ context.Context, now time.Time, limit int) ([]*models.IngestJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var jobs []*models.IngestJob
	for _, job := range s.ingestJobs {
		claimable := (job.State == "pending" || job.State == "retry") && !job.NextAttemptAt.After(now)
		expiredLease := job.State == "processing" && (job.LeaseUntil == nil || !job.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *job
			jobs = append(jobs, &cp)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	for _, job := range jobs {
		stored := s.ingestJobs[job.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.UpdatedAt = now
		job.State = stored.State
		job.Attempts = stored.Attempts
		job.ClaimedAt = stored.ClaimedAt
		job.LeaseUntil = stored.LeaseUntil
		job.UpdatedAt = stored.UpdatedAt
	}
	return jobs, nil
}

func (s *FakeStore) MarkIngestJobDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.State = "done"
		job.ClaimedAt = nil
		job.LeaseUntil = nil
		job.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) MarkIngestJobRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.State = "retry"
		if dead {
			job.State = "dead"
		}
		job.LastError = lastError
		job.NextAttemptAt = nextAttemptAt
		job.ClaimedAt = nil
		job.LeaseUntil = nil
		job.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) PurgeOldIngestJobs(_ context.Context, before time.Time, limit int) (int, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var toDelete []uuid.UUID
	var keys []string
	for id, job := range s.ingestJobs {
		if (job.State == "done" || job.State == "dead") && job.UpdatedAt.Before(before) {
			toDelete = append(toDelete, id)
			if job.RawObjectKey != "" {
				keys = append(keys, job.RawObjectKey)
			}
			if len(toDelete) >= limit {
				break
			}
		}
	}
	for _, id := range toDelete {
		delete(s.ingestJobs, id)
	}
	return len(toDelete), keys, nil
}

func (s *FakeStore) ListIngestJobs(_ context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.IngestJob
	for _, job := range s.ingestJobs {
		if state != "" && job.State != state {
			continue
		}
		if source != "" && job.Source != source {
			continue
		}
		if recipient != "" {
			match := false
			for _, rcpt := range job.Recipients {
				if strings.EqualFold(rcpt, recipient) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		cp := *job
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= total {
		return []*models.IngestJob{}, total, nil
	}
	end := start + pg.PerPage
	if end > total {
		end = total
	}
	return out[start:end], total, nil
}

func (s *FakeStore) CountIngestJobsByState(_ context.Context, states ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(states) == 0 {
		return len(s.ingestJobs), nil
	}
	set := make(map[string]struct{}, len(states))
	for _, state := range states {
		set[state] = struct{}{}
	}
	total := 0
	for _, job := range s.ingestJobs {
		if _, ok := set[job.State]; ok {
			total++
		}
	}
	return total, nil
}

// ================================================================
// Users (stub implementations for Store interface)
// ================================================================

func (s *FakeStore) CreateUser(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if s.users == nil {
		s.users = map[uuid.UUID]*models.User{}
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *FakeStore) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, nil
	}
	u, ok := s.users[id]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (s *FakeStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, nil
	}
	lower := strings.ToLower(strings.TrimSpace(email))
	for _, u := range s.users {
		if strings.ToLower(u.Email) == lower {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *FakeStore) ListUsers(_ context.Context, pg models.Page) ([]*models.User, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, 0, nil
	}
	var all []*models.User
	for _, u := range s.users {
		cp := *u
		all = append(all, &cp)
	}
	return all, len(all), nil
}

func (s *FakeStore) UpdateUser(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return errors.New("user not found")
	}
	if _, ok := s.users[u.ID]; !ok {
		return errors.New("user not found")
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *FakeStore) UpdateUserPassword(_ context.Context, id uuid.UUID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return errors.New("user not found")
	}
	u, ok := s.users[id]
	if !ok {
		return errors.New("user not found")
	}
	u.PasswordHash = hash
	return nil
}

func (s *FakeStore) DeleteUser(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users != nil {
		delete(s.users, id)
	}
	return nil
}

func (s *FakeStore) TouchUserLogin(_ context.Context, _ uuid.UUID) error { return nil }

// ================================================================
// Refresh tokens (stubs)
// ================================================================

func (s *FakeStore) CreateRefreshToken(_ context.Context, _ *models.RefreshToken) error { return nil }
func (s *FakeStore) GetRefreshToken(_ context.Context, _ string) (*models.RefreshToken, error) {
	return nil, nil
}
func (s *FakeStore) RevokeRefreshToken(_ context.Context, _ uuid.UUID) error      { return nil }
func (s *FakeStore) RevokeUserRefreshTokens(_ context.Context, _ uuid.UUID) error { return nil }
func (s *FakeStore) DeleteExpiredRefreshTokens(_ context.Context) error           { return nil }

// ================================================================
// Admin invitations (stubs)
// ================================================================

func (s *FakeStore) CreateAdminInvitation(_ context.Context, _ *models.AdminInvitation) error {
	return nil
}
func (s *FakeStore) GetAdminInvitationByCode(_ context.Context, _ string) (*models.AdminInvitation, error) {
	return nil, nil
}
func (s *FakeStore) MarkInvitationAccepted(_ context.Context, _ uuid.UUID) error { return nil }

// ================================================================
// System settings (stubs)
// ================================================================

func (s *FakeStore) GetSetting(_ context.Context, key string) (*models.SystemSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return nil, nil
	}
	ss, ok := s.settings[key]
	if !ok {
		return nil, nil
	}
	cp := *ss
	return &cp, nil
}

func (s *FakeStore) UpsertSetting(_ context.Context, key, value, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		s.settings = map[string]*models.SystemSetting{}
	}
	s.settings[key] = &models.SystemSetting{Key: key, Value: value, Description: description}
	return nil
}

func (s *FakeStore) ListSettings(_ context.Context) ([]*models.SystemSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return nil, nil
	}
	var out []*models.SystemSetting
	for _, ss := range s.settings {
		cp := *ss
		out = append(out, &cp)
	}
	return out, nil
}

// ================================================================
// Permission profiles (stubs)
// ================================================================

func (s *FakeStore) CreatePermissionProfile(_ context.Context, _ *models.PermissionProfile) error {
	return nil
}
func (s *FakeStore) GetPermissionProfile(_ context.Context, _ uuid.UUID) (*models.PermissionProfile, error) {
	return nil, nil
}
func (s *FakeStore) GetPermissionProfileByName(_ context.Context, _ string) (*models.PermissionProfile, error) {
	return nil, nil
}
func (s *FakeStore) ListPermissionProfiles(_ context.Context) ([]*models.PermissionProfile, error) {
	return nil, nil
}
func (s *FakeStore) UpdatePermissionProfile(_ context.Context, _ *models.PermissionProfile) error {
	return nil
}
func (s *FakeStore) DeletePermissionProfile(_ context.Context, _ uuid.UUID) error { return nil }

// ================================================================
// User permission overrides (stubs)
// ================================================================

func (s *FakeStore) UpsertUserPermissionOverride(_ context.Context, _ *models.UserPermissionOverride) error {
	return nil
}
func (s *FakeStore) GetUserPermissionOverride(_ context.Context, _ uuid.UUID) (*models.UserPermissionOverride, error) {
	return nil, nil
}
func (s *FakeStore) DeleteUserPermissionOverride(_ context.Context, _ uuid.UUID) error { return nil }
func (s *FakeStore) EffectivePermission(_ context.Context, _ uuid.UUID) (*models.EffectivePermission, error) {
	return nil, nil
}

// ================================================================
// Outbound jobs (stubs)
// ================================================================

func (s *FakeStore) CreateOutboundJob(_ context.Context, _ *models.OutboundJob) error { return nil }
func (s *FakeStore) GetOutboundJob(_ context.Context, _ uuid.UUID) (*models.OutboundJob, error) {
	return nil, nil
}
func (s *FakeStore) ListOutboundJobs(_ context.Context, _ uuid.UUID, _ models.Page) ([]*models.OutboundJob, int, error) {
	return nil, 0, nil
}
func (s *FakeStore) ClaimOutboundJobs(_ context.Context, _ time.Time, _ int) ([]*models.OutboundJob, error) {
	return nil, nil
}
func (s *FakeStore) MarkOutboundJobSent(_ context.Context, _ uuid.UUID, _ int, _, _ string) error {
	return nil
}
func (s *FakeStore) MarkOutboundJobRetry(_ context.Context, _ uuid.UUID, _ string, _ time.Time) error {
	return nil
}
func (s *FakeStore) MarkOutboundJobFailed(_ context.Context, _ uuid.UUID, _ string, _ bool) error {
	return nil
}
func (s *FakeStore) CountOutboundSince(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

// ================================================================
// Uniqueness checks (stubs)
// ================================================================

func (s *FakeStore) ExistsMailboxByAddress(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *FakeStore) ExistsZoneByDomain(_ context.Context, _ string) (bool, error) {
	return false, nil
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

func (m *MemoryObjectStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok, nil
}

func (m *MemoryObjectStore) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.data)
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

func derefTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
