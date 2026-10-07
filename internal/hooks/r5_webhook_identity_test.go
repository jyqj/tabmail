package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type r5IdentityRequest struct {
	path   string
	header http.Header
	body   []byte
}

func r5IdentityServer(t *testing.T, requests chan<- r5IdentityRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read webhook body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- r5IdentityRequest{path: r.URL.Path, header: r.Header.Clone(), body: body}
		if r.Header.Get("X-TabMail-Attempt") == "1" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r5IdentityConfig(srv *httptest.Server) Config {
	return Config{
		URLs: srv.URL + "/first," + srv.URL + "/second", AllowedCIDRs: "127.0.0.1/32,::1/128",
		Secret: "test-webhook-identity-secret", Timeout: time.Second, MaxRetries: 2,
		RetryDelay: time.Millisecond, BatchSize: 10,
	}
}

func r5IdentityCollect(t *testing.T, requests <-chan r5IdentityRequest, count int) []r5IdentityRequest {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	got := make([]r5IdentityRequest, 0, count)
	for len(got) < count {
		select {
		case req := <-requests:
			got = append(got, req)
		case <-timer.C:
			t.Fatalf("received %d webhook requests, want %d", len(got), count)
		}
	}
	return got
}

func r5IdentityCheckEnvelope(t *testing.T, got r5IdentityRequest, body []byte, secret string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	eventID, err := uuid.Parse(got.header.Get("X-TabMail-Event-ID"))
	if err != nil || eventID == uuid.Nil {
		t.Fatalf("missing or invalid event identity: %q", got.header.Get("X-TabMail-Event-ID"))
	}
	deliveryID, err := uuid.Parse(got.header.Get("X-TabMail-Delivery-ID"))
	if err != nil || deliveryID == uuid.Nil {
		t.Fatalf("missing or invalid delivery identity: %q", got.header.Get("X-TabMail-Delivery-ID"))
	}
	if !bytes.Equal(got.body, body) || got.header.Get("X-TabMail-Signature") != sign(secret, body) {
		t.Fatal("stable identity changed payload bytes or the existing body-only signature")
	}
	if got.header.Get("Content-Type") != "application/json" || got.header.Get("X-TabMail-Event") != "mailbox.invalidate" {
		t.Fatal("stable identity changed the existing webhook headers")
	}
	return eventID, deliveryID
}

func TestR5WebhookIdentityDirectPublishFanoutAndRetry(t *testing.T) {
	requests := make(chan r5IdentityRequest, 8)
	srv := r5IdentityServer(t, requests)
	cfg := r5IdentityConfig(srv)
	d := New(cfg, zerolog.Nop())
	t.Cleanup(d.client.CloseIdleConnections)
	event := Event{Type: "mailbox.invalidate", Mailbox: "synthetic@example.test", OccurredAt: time.Unix(1234, 0).UTC()}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	// Identical payloads still represent two distinct publications. Each one
	// fans out to two destinations, and each destination fails once then succeeds.
	d.Publish(event)
	d.Publish(event)
	type deliverySeen struct {
		eventID  uuid.UUID
		path     string
		attempts map[string]bool
	}
	deliveries := make(map[uuid.UUID]*deliverySeen)
	events := make(map[uuid.UUID]map[uuid.UUID]bool)
	for _, req := range r5IdentityCollect(t, requests, 8) {
		eventID, deliveryID := r5IdentityCheckEnvelope(t, req, body, cfg.Secret)
		if events[eventID] == nil {
			events[eventID] = make(map[uuid.UUID]bool)
		}
		events[eventID][deliveryID] = true
		seen := deliveries[deliveryID]
		if seen == nil {
			seen = &deliverySeen{eventID: eventID, path: req.path, attempts: make(map[string]bool)}
			deliveries[deliveryID] = seen
		}
		attempt := req.header.Get("X-TabMail-Attempt")
		if seen.eventID != eventID || seen.path != req.path || seen.attempts[attempt] {
			t.Fatal("delivery identity changed event/destination or repeated an attempt")
		}
		seen.attempts[attempt] = true
	}
	if len(events) != 2 || len(deliveries) != 4 {
		t.Fatalf("got %d event IDs and %d delivery IDs, want 2 and 4", len(events), len(deliveries))
	}
	for _, ids := range events {
		if len(ids) != 2 {
			t.Fatal("one publication did not preserve a shared event ID across fanout")
		}
	}
	for _, seen := range deliveries {
		if len(seen.attempts) != 2 || !seen.attempts["1"] || !seen.attempts["2"] {
			t.Fatalf("retry did not retain its delivery ID and attempt count: %#v", seen)
		}
	}
}

type r5IdentityStore struct {
	*testutil.FakeStore
	events   []models.OutboxEvent
	claimNow time.Time
}

func (s *r5IdentityStore) CreateOutboxEvent(ctx context.Context, event *models.OutboxEvent) error {
	if err := s.FakeStore.CreateOutboxEvent(ctx, event); err != nil {
		return err
	}
	s.events = append(s.events, *event)
	return nil
}

func (s *r5IdentityStore) ClaimWebhookDeliveries(ctx context.Context, _ time.Time, limit int) ([]*models.WebhookDelivery, error) {
	// Advance only the fake store's due-time clock; no sleeps or production
	// retry overrides are needed to exercise both real worker attempts.
	return s.FakeStore.ClaimWebhookDeliveries(ctx, s.claimNow, limit)
}

func TestR5WebhookIdentityStoredFanoutAndRestartedWorker(t *testing.T) {
	ctx := context.Background()
	requests := make(chan r5IdentityRequest, 4)
	srv := r5IdentityServer(t, requests)
	cfg := r5IdentityConfig(srv)
	st := &r5IdentityStore{FakeStore: testutil.NewFakeStore(), claimNow: time.Now().Add(time.Hour)}
	d := New(cfg, zerolog.Nop()).BindStore(st)
	t.Cleanup(d.client.CloseIdleConnections)
	d.Publish(Event{Type: "mailbox.invalidate", Mailbox: "synthetic@example.test", OccurredAt: time.Unix(1234, 0).UTC()})
	if len(st.events) != 1 || st.events[0].ID == uuid.Nil {
		t.Fatalf("publication did not create one immutable outbox identity: %#v", st.events)
	}
	event := st.events[0]
	d.ensureWorkers()
	d.outboxWorker.ProcessBatch(ctx)
	before, total, err := st.ListWebhookDeliveries(ctx, models.Page{PerPage: 10}, "", "", "")
	if err != nil || total != 2 || len(before) != 2 {
		t.Fatalf("outbox did not create two deliveries: total=%d err=%v", total, err)
	}
	expected := make(map[uuid.UUID]string)
	for _, delivery := range before {
		if delivery.ID == uuid.Nil || delivery.EventID != event.ID || expected[delivery.ID] != "" {
			t.Fatal("stored fanout lost distinct delivery IDs or the persisted event ID")
		}
		expected[delivery.ID] = delivery.URL
	}
	// Re-execute the outbox after an unobserved acknowledgement. The existing
	// store fanout contract must preserve both already-created delivery IDs.
	if err := st.MarkOutboxEventRetry(ctx, event.ID, "acknowledgement unknown", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	d.outboxWorker.ProcessBatch(ctx)
	d.deliveryWorker.ProcessBatch(ctx)
	// A fresh dispatcher has no in-memory ID state to reuse. Its retry must
	// transmit the event and delivery identities from the claimed rows.
	restarted := New(cfg, zerolog.Nop()).BindStore(st)
	t.Cleanup(restarted.client.CloseIdleConnections)
	restarted.ensureWorkers()
	restarted.deliveryWorker.ProcessBatch(ctx)
	seen := make(map[uuid.UUID]map[int]bool)
	for _, req := range r5IdentityCollect(t, requests, 4) {
		eventID, deliveryID := r5IdentityCheckEnvelope(t, req, event.Payload, cfg.Secret)
		if eventID != event.ID || expected[deliveryID] != srv.URL+req.path {
			t.Fatal("wire identity did not match the original persisted row and destination")
		}
		attempt, err := strconv.Atoi(req.header.Get("X-TabMail-Attempt"))
		if err != nil || attempt < 1 || attempt > 2 {
			t.Fatalf("invalid existing attempt header: %q", req.header.Get("X-TabMail-Attempt"))
		}
		if seen[deliveryID] == nil {
			seen[deliveryID] = make(map[int]bool)
		}
		if seen[deliveryID][attempt] {
			t.Fatal("same delivery attempt was emitted twice")
		}
		seen[deliveryID][attempt] = true
	}
	after, total, err := st.ListWebhookDeliveries(ctx, models.Page{PerPage: 10}, "", "", "")
	if err != nil || total != 2 || len(after) != 2 || len(seen) != 2 {
		t.Fatalf("fanout replay changed delivery count: total=%d err=%v", total, err)
	}
	for _, delivery := range after {
		if delivery.State != "delivered" || delivery.Attempts != 2 || len(seen[delivery.ID]) != 2 || delivery.EventID != event.ID {
			t.Fatalf("worker retry changed the original lifecycle or identity: %#v", delivery)
		}
	}
}

func TestR5WebhookIdentityMissingIDsAreNotInvented(t *testing.T) {
	for _, tc := range []struct {
		name     string
		eventID  uuid.UUID
		delivery uuid.UUID
	}{
		{name: "both missing"},
		{name: "event missing", delivery: uuid.New()},
		{name: "delivery missing", eventID: uuid.New()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan r5IdentityRequest, 1)
			srv := r5IdentityServer(t, requests)
			d := New(r5IdentityConfig(srv), zerolog.Nop())
			t.Cleanup(d.client.CloseIdleConnections)
			delivery := &models.WebhookDelivery{ID: tc.delivery, EventID: tc.eventID, URL: srv.URL, Attempts: 2, Payload: []byte(`{}`)}
			if err := d.dispatch(context.Background(), delivery); err != nil {
				t.Fatal(err)
			}
			req := r5IdentityCollect(t, requests, 1)[0]
			for header, id := range map[string]uuid.UUID{"X-TabMail-Event-ID": tc.eventID, "X-TabMail-Delivery-ID": tc.delivery} {
				want := ""
				if id != uuid.Nil {
					want = id.String()
				}
				if got := req.header.Get(header); got != want {
					t.Errorf("%s: got %q, want %q", header, got, want)
				}
			}
			if delivery.ID != tc.delivery || delivery.EventID != tc.eventID {
				t.Fatal("dispatch mutated caller-owned identity")
			}
		})
	}
}
