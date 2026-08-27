package hooks

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

func TestTenantEndpointsParticipateInOutboxFanout(t *testing.T) {
	tenantID := uuid.New()
	secret := "tenant-secret"
	store := newTenantEndpointStore()
	store.endpoints[tenantID] = []*models.WebhookEndpoint{
		{ID: uuid.New(), TenantID: tenantID, URL: "https://hooks.example.test/messages", Secret: &secret, EventTypes: []string{"message.received"}, IsActive: true},
		{ID: uuid.New(), TenantID: tenantID, URL: "https://hooks.example.test/domains", EventTypes: []string{"domain.created"}, IsActive: true},
		{ID: uuid.New(), TenantID: tenantID, URL: "https://hooks.example.test/disabled", IsActive: false},
	}
	d := New(Config{}, zerolog.Nop()).BindStore(store)
	if !d.Enabled() {
		t.Fatal("a store-bound dispatcher must be enabled without static URLs")
	}
	d.Publish(Event{Type: "message.received", TenantID: tenantID.String(), Mailbox: "a@example.test"})
	if err := d.processBatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	if len(store.deliveries) != 1 {
		store.mu.Unlock()
		t.Fatalf("expected one matching delivery, got %d", len(store.deliveries))
	}
	var delivery *models.WebhookDelivery
	for _, item := range store.deliveries {
		cp := *item
		delivery = &cp
	}
	store.mu.Unlock()
	if delivery.URL != "https://hooks.example.test/messages" {
		t.Fatalf("unexpected endpoint: %s", delivery.URL)
	}
	target, err := d.resolveDeliveryTarget(context.Background(), delivery)
	if err != nil {
		t.Fatal(err)
	}
	if !target.tenantManaged || target.secret != secret {
		t.Fatalf("unexpected resolved target: %#v", target)
	}

	store.endpoints[tenantID][0].IsActive = false
	if _, err := d.resolveDeliveryTarget(context.Background(), delivery); err == nil {
		t.Fatal("queued delivery must fail closed after endpoint disable")
	}
}

type tenantEndpointStore struct {
	mu         sync.Mutex
	outbox     map[uuid.UUID]*models.OutboxEvent
	deliveries map[uuid.UUID]*models.WebhookDelivery
	endpoints  map[uuid.UUID][]*models.WebhookEndpoint
}

func newTenantEndpointStore() *tenantEndpointStore {
	return &tenantEndpointStore{outbox: map[uuid.UUID]*models.OutboxEvent{}, deliveries: map[uuid.UUID]*models.WebhookDelivery{}, endpoints: map[uuid.UUID][]*models.WebhookEndpoint{}}
}

func (s *tenantEndpointStore) CreateOutboxEvent(_ context.Context, event *models.OutboxEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *event
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	cp.NextAttemptAt = time.Now().Add(-time.Second)
	s.outbox[cp.ID] = &cp
	event.ID = cp.ID
	return nil
}
func (s *tenantEndpointStore) ClaimOutboxEvents(context.Context, time.Time, int) ([]*models.OutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.OutboxEvent
	for _, event := range s.outbox {
		if event.State != "pending" && event.State != "retry" {
			continue
		}
		cp := *event
		cp.Attempts++
		out = append(out, &cp)
		event.State = "processing"
		event.Attempts = cp.Attempts
	}
	return out, nil
}
func (s *tenantEndpointStore) MarkOutboxEventDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.outbox[id]; e != nil {
		e.State = "done"
	}
	return nil
}
func (s *tenantEndpointStore) MarkOutboxEventRetry(_ context.Context, id uuid.UUID, msg string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.outbox[id]; e != nil {
		e.State = "retry"
		e.LastError = msg
		e.NextAttemptAt = next
	}
	return nil
}
func (s *tenantEndpointStore) CreateWebhookDeliveries(_ context.Context, event *models.OutboxEvent, urls []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range urls {
		d := &models.WebhookDelivery{ID: uuid.New(), EventID: event.ID, URL: u, EventType: event.EventType, Payload: append([]byte(nil), event.Payload...), State: "pending", CreatedAt: time.Now()}
		s.deliveries[d.ID] = d
	}
	return nil
}
func (s *tenantEndpointStore) ListWebhookEndpoints(_ context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*models.WebhookEndpoint(nil), s.endpoints[tenantID]...), nil
}
func (s *tenantEndpointStore) ClaimWebhookDeliveries(context.Context, time.Time, int) ([]*models.WebhookDelivery, error) {
	return nil, nil
}
func (s *tenantEndpointStore) MarkWebhookDeliveryDone(context.Context, uuid.UUID) error { return nil }
func (s *tenantEndpointStore) MarkWebhookDeliveryRetry(context.Context, uuid.UUID, string, time.Time, bool) error {
	return nil
}
func (s *tenantEndpointStore) ListDeadWebhookDeliveries(context.Context, int) ([]models.DeadLetter, error) {
	return nil, nil
}
func (s *tenantEndpointStore) CountDeadWebhookDeliveries(context.Context) (int, error) { return 0, nil }
