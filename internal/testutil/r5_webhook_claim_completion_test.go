package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func TestFakeWebhookClaimCompletionMatchesGenerationProtocol(t *testing.T) {
	for _, op := range []string{"outbox-done", "outbox-retry", "webhook-done", "webhook-retry", "webhook-dead"} {
		for _, boundary := range []string{"current", "reclaimed", "expired", "completed", "missing", "cancelled"} {
			t.Run(op+"/"+boundary, func(t *testing.T) {
				st := NewFakeStore()
				ctx := context.Background()
				event := &models.OutboxEvent{EventType: "r5.claim", Payload: []byte(`{}`), NextAttemptAt: time.Now().Add(-time.Hour)}
				if err := st.CreateOutboxEvent(ctx, event); err != nil {
					t.Fatal(err)
				}
				id, attempt := event.ID, 0
				var snapshot func() any
				var expire func()
				var reclaim func()
				var mark func(context.Context, uuid.UUID, int) error
				if op == "outbox-done" || op == "outbox-retry" {
					rows, err := st.ClaimOutboxEvents(ctx, time.Now(), 1)
					if err != nil || len(rows) != 1 {
						t.Fatalf("outbox claim: %v %v", rows, err)
					}
					attempt = rows[0].Attempts
					snapshot = func() any { return st.outbox[id] }
					expire = func() {
						past := time.Now().Add(-time.Minute)
						st.outbox[id].LeaseUntil = &past
					}
					reclaim = func() { _, _ = st.ClaimOutboxEvents(ctx, time.Now(), 1) }
					if op == "outbox-done" {
						mark = st.MarkOutboxEventDoneClaim
					} else {
						mark = func(ctx context.Context, id uuid.UUID, attempt int) error {
							return st.MarkOutboxEventRetryClaim(ctx, id, attempt, "retry", time.Now().Add(time.Hour))
						}
					}
				} else {
					if err := st.CreateWebhookDeliveries(ctx, event, []string{"http://127.0.0.1:1/never-dispatched"}); err != nil {
						t.Fatal(err)
					}
					rows, err := st.ClaimWebhookDeliveries(ctx, time.Now(), 1)
					if err != nil || len(rows) != 1 {
						t.Fatalf("delivery claim: %v %v", rows, err)
					}
					id, attempt = rows[0].ID, rows[0].Attempts
					snapshot = func() any { return st.deliveries[id] }
					expire = func() {
						past := time.Now().Add(-time.Minute)
						st.deliveries[id].LeaseUntil = &past
					}
					reclaim = func() { _, _ = st.ClaimWebhookDeliveries(ctx, time.Now(), 1) }
					if op == "webhook-done" {
						mark = st.MarkWebhookDeliveryDoneClaim
					} else {
						mark = func(ctx context.Context, id uuid.UUID, attempt int) error {
							return st.MarkWebhookDeliveryRetryClaim(ctx, id, attempt, "retry", time.Now().Add(time.Hour), op == "webhook-dead")
						}
					}
				}
				markID := id
				switch boundary {
				case "reclaimed":
					expire()
					reclaim()
				case "expired":
					expire()
				case "completed":
					if err := mark(ctx, id, attempt); err != nil {
						t.Fatal(err)
					}
				case "missing":
					markID = uuid.New()
				case "cancelled":
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}
				before, err := json.Marshal(snapshot())
				if err != nil {
					t.Fatal(err)
				}
				err = mark(ctx, markID, attempt)
				if boundary == "current" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				want := store.ErrClaimLeaseLost
				if boundary == "cancelled" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Errorf("got %v, want %v", err, want)
				}
				after, err := json.Marshal(snapshot())
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Error("non-owner completion changed fake queue state")
				}
			})
		}
	}
}
