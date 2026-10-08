package hooks

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

// The existing in-memory queue supplies the same five-minute claim lease and
// increments attempts just like PostgreSQL. The barriers control delivery and
// fanout, never forge a queue transition or move a claimed row's deadline.
type r5SerialClaimStore struct {
	*testutil.FakeStore
	mu             sync.Mutex
	deliveryLimits []int
	outboxLimits   []int
	fanouts        map[uuid.UUID]int
	blockEvent     uuid.UUID
	entered        chan struct{}
	release        chan struct{}
}

func (s *r5SerialClaimStore) ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error) {
	s.mu.Lock()
	s.deliveryLimits = append(s.deliveryLimits, limit)
	s.mu.Unlock()
	return s.FakeStore.ClaimWebhookDeliveries(ctx, now, limit)
}

func (s *r5SerialClaimStore) ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error) {
	s.mu.Lock()
	s.outboxLimits = append(s.outboxLimits, limit)
	s.mu.Unlock()
	return s.FakeStore.ClaimOutboxEvents(ctx, now, limit)
}

func (s *r5SerialClaimStore) CreateWebhookDeliveries(ctx context.Context, event *models.OutboxEvent, urls []string) error {
	s.mu.Lock()
	s.fanouts[event.ID]++
	block := event.ID == s.blockEvent && s.fanouts[event.ID] == 1
	s.mu.Unlock()
	if block {
		close(s.entered)
		<-s.release
	}
	return s.FakeStore.CreateWebhookDeliveries(ctx, event, urls)
}

type r5SerialTransport func(*http.Request) (*http.Response, error)

func (f r5SerialTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func r5SerialResponse(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
}

func r5NewSerialDispatcher(st *r5SerialClaimStore, batch int) *Dispatcher {
	d := New(Config{URLs: "https://receiver.example.test/hook", Timeout: 10 * time.Minute, BatchSize: batch}, zerolog.Nop()).BindStore(st)
	d.ensureWorkers()
	return d
}

func TestR5SerialWebhookClaimsDoNotSendLaterRowsAfterLeaseTransfer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		st := &r5SerialClaimStore{FakeStore: testutil.NewFakeStore(), fanouts: make(map[uuid.UUID]int)}
		first := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{"type":"message.received"}`)}
		second := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: first.Payload}
		if err := st.FakeStore.CreateWebhookDeliveries(ctx, first, []string{"https://receiver.example.test/first"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Nanosecond) // Distinct persisted ordering, not a scheduling delay.
		if err := st.FakeStore.CreateWebhookDeliveries(ctx, second, []string{"https://receiver.example.test/second"}); err != nil {
			t.Fatal(err)
		}
		d := r5NewSerialDispatcher(st, 2)
		defer d.client.CloseIdleConnections()
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var mu sync.Mutex
		requests := make(map[string]int)
		d.client.Transport = r5SerialTransport(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			requests[req.URL.Path]++
			mu.Unlock()
			if req.URL.Path == "/first" {
				close(entered)
				<-release
			}
			return r5SerialResponse(req)
		})
		go func() { defer close(done); d.deliveryWorker.ProcessBatch(ctx) }()
		<-entered
		// A second process takes over due work after the first request outlives
		// its lease. It successfully sends and acknowledges the later row.
		time.Sleep(6 * time.Minute)
		claimed, err := st.FakeStore.ClaimWebhookDeliveries(ctx, time.Now().UTC(), 100)
		if err != nil {
			t.Fatal(err)
		}
		var later *models.WebhookDelivery
		for _, item := range claimed {
			if item.EventID == second.ID {
				later = item
			}
		}
		if later == nil {
			t.Fatal("competing worker did not obtain the later delivery")
		}
		if err := d.dispatch(ctx, later); err != nil {
			t.Fatal(err)
		}
		if err := st.FakeStore.MarkWebhookDeliveryDone(ctx, later.ID); err != nil {
			t.Fatal(err)
		}
		unblock()
		<-done
		mu.Lock()
		defer mu.Unlock()
		if requests["/first"] != 1 || requests["/second"] != 1 {
			t.Errorf("HTTP effects = %v; later acknowledged row must not be sent by old batch", requests)
		}
		if st.deliveryLimits[0] != 1 {
			t.Errorf("first claim reserved %d rows before one serial HTTP request", st.deliveryLimits[0])
		}
	})
}

func TestR5SerialOutboxClaimsDoNotFanoutLaterRowsAfterLeaseTransfer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		first := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{"type":"message.received"}`)}
		second := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: first.Payload}
		st := &r5SerialClaimStore{FakeStore: testutil.NewFakeStore(), fanouts: make(map[uuid.UUID]int), blockEvent: first.ID, entered: make(chan struct{}), release: make(chan struct{})}
		if err := st.CreateOutboxEvent(ctx, first); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Nanosecond)
		if err := st.CreateOutboxEvent(ctx, second); err != nil {
			t.Fatal(err)
		}
		d := r5NewSerialDispatcher(st, 2)
		defer d.client.CloseIdleConnections()
		done := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(st.release) }) }
		defer unblock()
		go func() { defer close(done); d.outboxWorker.ProcessBatch(ctx) }()
		<-st.entered
		time.Sleep(6 * time.Minute)
		claimed, err := st.FakeStore.ClaimOutboxEvents(ctx, time.Now().UTC(), 100)
		if err != nil {
			t.Fatal(err)
		}
		var later *models.OutboxEvent
		for _, item := range claimed {
			if item.ID == second.ID {
				later = item
			}
		}
		if later == nil {
			t.Fatal("competing worker did not obtain the later event")
		}
		if err := d.processOutbox(ctx, &workqueue.Job[*outboxPayload]{ID: later.ID, Attempts: later.Attempts, Payload: &outboxPayload{OutboxEvent: later}}); err != nil {
			t.Fatal(err)
		}
		if err := st.MarkOutboxEventDone(ctx, later.ID); err != nil {
			t.Fatal(err)
		}
		unblock()
		<-done
		if st.fanouts[first.ID] != 1 || st.fanouts[second.ID] != 1 {
			t.Errorf("fanout effects = %v; old batch repeated a later completed event", st.fanouts)
		}
		if st.outboxLimits[0] != 1 {
			t.Errorf("first claim reserved %d events before one serial fanout", st.outboxLimits[0])
		}
	})
}

func TestR5SerialWebhookClaimsRetainConfiguredWorkPerCycle(t *testing.T) {
	for _, stage := range []string{"outbox", "delivery"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			st := &r5SerialClaimStore{FakeStore: testutil.NewFakeStore(), fanouts: make(map[uuid.UUID]int)}
			d := r5NewSerialDispatcher(st, 3)
			defer d.client.CloseIdleConnections()
			requests := 0
			d.client.Transport = r5SerialTransport(func(req *http.Request) (*http.Response, error) { requests++; return r5SerialResponse(req) })
			for i := 0; i < 4; i++ {
				event := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{"type":"message.received"}`)}
				var err error
				if stage == "outbox" {
					err = st.CreateOutboxEvent(ctx, event)
				} else {
					err = st.FakeStore.CreateWebhookDeliveries(ctx, event, []string{"https://receiver.example.test/hook"})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var limits []int
			var count int
			if stage == "outbox" {
				d.outboxWorker.ProcessBatch(ctx)
				limits, count = st.outboxLimits, len(st.fanouts)
			} else {
				d.deliveryWorker.ProcessBatch(ctx)
				limits, count = st.deliveryLimits, requests
			}
			if count != 3 {
				t.Errorf("one configured three-job cycle processed %d jobs", count)
			}
			for _, limit := range limits {
				if limit != 1 {
					t.Errorf("serial stage claimed %d rows together", limit)
				}
			}
		})
	}
}
