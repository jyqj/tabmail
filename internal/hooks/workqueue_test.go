package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/workqueue"
)

// TestDeliveryHooksOnDeadPushesDeadLetter pins the store-backed delivery
// worker's dead path: OnDead records a dead letter derived from the claimed
// delivery, with LastTriedAt frozen at claim time and a defensive payload copy.
// Refactors that drop pushDeadLetter or change the field mapping break this.
func TestDeliveryHooksOnDeadPushesDeadLetter(t *testing.T) {
	d := New(Config{
		URLs:       "http://127.0.0.1:1",
		Timeout:    100 * time.Millisecond,
		MaxRetries: 3,
		RetryDelay: time.Millisecond,
		DeadLimit:  10,
	}, zerolog.Nop())
	h := &deliveryHooks{dispatcher: d}

	id := uuid.New()
	created := time.Now().Add(-time.Hour).UTC()
	claimTime := time.Now().Add(-time.Minute).UTC()
	job := &workqueue.Job[*deliveryPayload]{
		ID:       id,
		Attempts: 5,
		Payload: &deliveryPayload{
			WebhookDelivery: &models.WebhookDelivery{
				ID:        id,
				URL:       "https://example.test/hook",
				EventType: "message.received",
				Payload:   []byte(`{"hi":true}`),
				Attempts:  5,
				CreatedAt: created,
			},
			now: claimTime,
		},
	}

	h.OnDead(context.Background(), job, errors.New("boom"))

	if got := d.DeadLetterSize(); got != 1 {
		t.Fatalf("expected 1 dead letter, got %d", got)
	}
	dl := d.DeadLetters(10)[0]
	if dl.ID != id.String() || dl.URL != "https://example.test/hook" || dl.EventType != "message.received" {
		t.Fatalf("unexpected dead letter identity: %#v", dl)
	}
	if dl.Attempts != 5 || dl.LastError != "boom" {
		t.Fatalf("unexpected dead letter fields: attempts=%d lastError=%q", dl.Attempts, dl.LastError)
	}
	if dl.LastTriedAt != claimTime {
		t.Fatalf("expected LastTriedAt frozen at claim time %v, got %v", claimTime, dl.LastTriedAt)
	}
	if dl.CreatedAt != created {
		t.Fatalf("expected CreatedAt %v, got %v", created, dl.CreatedAt)
	}
	if string(dl.Payload) != `{"hi":true}` {
		t.Fatalf("expected payload copied, got %q", dl.Payload)
	}
}

// TestDeliveryHooksOnDeadNilSafe ensures a malformed job never panics the worker.
func TestDeliveryHooksOnDeadNilSafe(t *testing.T) {
	d := New(Config{DeadLimit: 10}, zerolog.Nop())
	h := &deliveryHooks{dispatcher: d}

	h.OnDead(context.Background(), nil, errors.New("e"))
	h.OnDead(context.Background(), &workqueue.Job[*deliveryPayload]{}, errors.New("e"))
	h.OnDead(context.Background(), &workqueue.Job[*deliveryPayload]{Payload: &deliveryPayload{}}, errors.New("e"))

	if got := d.DeadLetterSize(); got != 0 {
		t.Fatalf("nil payloads must not push dead letters, got %d", got)
	}
}
