package hooks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

func TestR5ConfiguredWebhookFanout(t *testing.T) {
	t.Run("preserves distinct destination identity and first-seen order", func(t *testing.T) {
		d := New(Config{URLs: " , https://receiver.example.test/Hook?a=1, https://receiver.example.test/Hook?a=1 ,https://receiver.example.test/hook?a=1,https://receiver.example.test/Hook?a=2, "}, zerolog.Nop())
		t.Cleanup(d.client.CloseIdleConnections)
		want := []string{"https://receiver.example.test/Hook?a=1", "https://receiver.example.test/hook?a=1", "https://receiver.example.test/Hook?a=2"}
		if !reflect.DeepEqual(d.urls, want) {
			t.Fatalf("configured destinations = %q, want %q", d.urls, want)
		}
	})

	t.Run("direct publications send once per unique destination", func(t *testing.T) {
		requests := make(chan r5IdentityRequest, 8)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read synthetic webhook body: %v", err)
			}
			requests <- r5IdentityRequest{path: r.URL.RequestURI(), header: r.Header.Clone(), body: body}
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(srv.Close)
		d := New(Config{URLs: srv.URL + "/hook?a=1, " + srv.URL + "/hook?a=1 ," + srv.URL + "/hook?a=2", AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: time.Second, MaxRetries: 1}, zerolog.Nop())
		t.Cleanup(d.client.CloseIdleConnections)
		event := Event{Type: "mailbox.invalidate", Mailbox: "synthetic@example.test", OccurredAt: time.Unix(1234, 0).UTC()}
		d.Publish(event)
		d.Publish(event)
		// Await every configured direct dispatch, including duplicates on the
		// baseline, so the negative assertion cannot race a late request.
		got := r5IdentityCollect(t, requests, 2*len(d.urls))
		if len(got) != 4 {
			t.Errorf("two publications sent %d requests, want four", len(got))
		}
		events := make(map[uuid.UUID]map[string]uuid.UUID)
		deliveries := make(map[uuid.UUID]bool)
		for _, req := range got {
			eventID, err := uuid.Parse(req.header.Get("X-TabMail-Event-ID"))
			if err != nil || eventID == uuid.Nil {
				t.Fatal("missing event identity")
			}
			deliveryID, err := uuid.Parse(req.header.Get("X-TabMail-Delivery-ID"))
			if err != nil || deliveryID == uuid.Nil || deliveries[deliveryID] {
				t.Fatal("missing or reused delivery identity")
			}
			deliveries[deliveryID] = true
			if events[eventID] == nil {
				events[eventID] = make(map[string]uuid.UUID)
			}
			if events[eventID][req.path] != uuid.Nil {
				t.Errorf("one publication sent duplicate destination %q", req.path)
			}
			events[eventID][req.path] = deliveryID
		}
		if len(events) != 2 {
			t.Fatalf("distinct publications collapsed to %d event identities", len(events))
		}
		for _, paths := range events {
			if len(paths) != 2 || paths["/hook?a=1"] == uuid.Nil || paths["/hook?a=2"] == uuid.Nil {
				t.Fatal("distinct query destinations were lost")
			}
		}
	})

	t.Run("stored outbox merges global and tenant destinations once", func(t *testing.T) {
		tenant := uuid.New()
		st := &destinationFanoutStore{FakeStore: testutil.NewFakeStore(), endpoints: []*models.WebhookEndpoint{
			{URL: " https://receiver.example.test/global ", IsActive: true},
			{URL: "https://receiver.example.test/tenant", IsActive: true},
			{URL: "https://receiver.example.test/tenant", IsActive: true},
			{URL: "https://receiver.example.test/inactive", IsActive: false},
		}}
		d := New(Config{URLs: "https://receiver.example.test/global, https://receiver.example.test/global "}, zerolog.Nop()).BindStore(st)
		t.Cleanup(d.client.CloseIdleConnections)
		event := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{"tenant_id":"` + tenant.String() + `"}`)}
		if err := d.processOutbox(context.Background(), &workqueue.Job[*outboxPayload]{Payload: &outboxPayload{OutboxEvent: event}}); err != nil {
			t.Fatal(err)
		}
		want := []string{"https://receiver.example.test/global", "https://receiver.example.test/tenant"}
		if !reflect.DeepEqual(st.urls, want) {
			t.Fatalf("stored outbox destinations = %q, want %q", st.urls, want)
		}
	})
}
