package testutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

const fakeClaimLeaseDuration = 5 * time.Minute

type resolvedAPIKey struct {
	Tenant *models.Tenant
	Scopes []string
	KeyID  uuid.UUID
}

type FakeStore struct {
	mu sync.Mutex

	plans      map[uuid.UUID]*models.Plan
	tenants    map[uuid.UUID]*models.Tenant
	overrides  map[uuid.UUID]*models.TenantOverride
	apiKeys    map[uuid.UUID]*models.TenantAPIKey
	apiRaw     map[string]resolvedAPIKey
	smtpPolicy *models.SMTPPolicy

	zones            map[uuid.UUID]*models.DomainZone
	routes           map[uuid.UUID]*models.DomainRoute
	mailboxes        map[uuid.UUID]*models.Mailbox
	messages         map[uuid.UUID]*models.Message
	audits           []*models.AuditEntry
	monitor          []*models.MonitorEvent
	outbox           map[uuid.UUID]*models.OutboxEvent
	deliveries       map[uuid.UUID]*models.WebhookDelivery
	ingestJobs       map[uuid.UUID]*models.IngestJob
	outboundJobs     map[uuid.UUID]*models.OutboundJob
	outboundAttempts map[uuid.UUID]*models.OutboundAttempt
	suppressions     map[uuid.UUID]*models.SuppressionEntry
	users            map[uuid.UUID]*models.User
	settings         map[string]*models.SystemSetting

	sendIdentities map[uuid.UUID]*models.SendIdentity
}

func NewFakeStore() *FakeStore {
	return &FakeStore{
		plans:            map[uuid.UUID]*models.Plan{},
		tenants:          map[uuid.UUID]*models.Tenant{},
		overrides:        map[uuid.UUID]*models.TenantOverride{},
		apiKeys:          map[uuid.UUID]*models.TenantAPIKey{},
		apiRaw:           map[string]resolvedAPIKey{},
		zones:            map[uuid.UUID]*models.DomainZone{},
		routes:           map[uuid.UUID]*models.DomainRoute{},
		mailboxes:        map[uuid.UUID]*models.Mailbox{},
		messages:         map[uuid.UUID]*models.Message{},
		monitor:          []*models.MonitorEvent{},
		outbox:           map[uuid.UUID]*models.OutboxEvent{},
		deliveries:       map[uuid.UUID]*models.WebhookDelivery{},
		ingestJobs:       map[uuid.UUID]*models.IngestJob{},
		outboundJobs:     map[uuid.UUID]*models.OutboundJob{},
		outboundAttempts: map[uuid.UUID]*models.OutboundAttempt{},
		suppressions:     map[uuid.UUID]*models.SuppressionEntry{},
		sendIdentities:   map[uuid.UUID]*models.SendIdentity{},
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

func (s *FakeStore) Close() error { return nil }

// ForTenant returns a read view whose lookups are filtered to tenantID; rows
// belonging to another tenant read as not found (nil, nil), matching the
// postgres implementation.
func (s *FakeStore) ForTenant(tenantID uuid.UUID) store.TenantScoped {
	return &fakeTenantView{store: s, tenantID: tenantID}
}

type fakeTenantView struct {
	store    *FakeStore
	tenantID uuid.UUID
}

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
