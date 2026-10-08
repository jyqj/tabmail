package hooks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/workqueue"
)

type r5ClaimMarkCall struct {
	id      uuid.UUID
	attempt int
	state   string
	reason  string
	next    time.Time
}

// Both old and guarded ports are present so this exact test can expose the
// existing production adapter's selection of an unconditional ID-only write.
type r5ClaimMarkStore struct {
	unsafe int
	calls  []r5ClaimMarkCall
	result error
}

func (*r5ClaimMarkStore) ClaimOutboxEvents(context.Context, time.Time, int) ([]*models.OutboxEvent, error) {
	return nil, nil
}
func (*r5ClaimMarkStore) ClaimWebhookDeliveries(context.Context, time.Time, int) ([]*models.WebhookDelivery, error) {
	return nil, nil
}
func (s *r5ClaimMarkStore) MarkOutboxEventDone(context.Context, uuid.UUID) error {
	s.unsafe++
	return nil
}
func (s *r5ClaimMarkStore) MarkOutboxEventRetry(context.Context, uuid.UUID, string, time.Time) error {
	s.unsafe++
	return nil
}
func (s *r5ClaimMarkStore) MarkWebhookDeliveryDone(context.Context, uuid.UUID) error {
	s.unsafe++
	return nil
}
func (s *r5ClaimMarkStore) MarkWebhookDeliveryRetry(context.Context, uuid.UUID, string, time.Time, bool) error {
	s.unsafe++
	return nil
}
func (s *r5ClaimMarkStore) MarkOutboxEventDoneClaim(_ context.Context, id uuid.UUID, attempt int) error {
	s.calls = append(s.calls, r5ClaimMarkCall{id: id, attempt: attempt, state: "done"})
	return s.result
}
func (s *r5ClaimMarkStore) MarkOutboxEventRetryClaim(_ context.Context, id uuid.UUID, attempt int, reason string, next time.Time) error {
	s.calls = append(s.calls, r5ClaimMarkCall{id: id, attempt: attempt, state: "retry", reason: reason, next: next})
	return s.result
}
func (s *r5ClaimMarkStore) MarkWebhookDeliveryDoneClaim(_ context.Context, id uuid.UUID, attempt int) error {
	s.calls = append(s.calls, r5ClaimMarkCall{id: id, attempt: attempt, state: "delivered"})
	return s.result
}
func (s *r5ClaimMarkStore) MarkWebhookDeliveryRetryClaim(_ context.Context, id uuid.UUID, attempt int, reason string, next time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	s.calls = append(s.calls, r5ClaimMarkCall{id: id, attempt: attempt, state: state, reason: reason, next: next})
	return s.result
}

func TestR5WebhookClaimMarksUseCurrentAttemptAndPreserveLeaseLoss(t *testing.T) {
	for _, stage := range []string{"outbox", "delivery"} {
		for _, outcome := range []string{"done", "retry", "dead"} {
			for _, result := range []string{"accepted", "lease-lost", "database-error"} {
				t.Run(stage+"/"+outcome+"/"+result, func(t *testing.T) {
					ctx := context.Background()
					id, attempt := uuid.New(), 7
					st := &r5ClaimMarkStore{}
					if result == "lease-lost" {
						st.result = fmt.Errorf("observation no longer current: %w", store.ErrClaimLeaseLost)
					} else if result == "database-error" {
						st.result = errors.New("synthetic storage failure")
					}
					next := time.Now().UTC().Add(time.Hour)
					before := time.Now().UTC()
					var err error
					wantState := outcome
					if stage == "outbox" {
						adapter := newOutboxStore(st)
						job := &workqueue.Job[*outboxPayload]{ID: id, Attempts: attempt}
						switch outcome {
						case "done":
							err = adapter.MarkDone(ctx, job)
						case "retry":
							err = adapter.MarkRetry(ctx, job, "synthetic failure", next)
						case "dead":
							wantState = "retry" // Outbox events are never dropped.
							err = adapter.MarkDead(ctx, job, "synthetic failure")
						}
					} else {
						adapter := newDeliveryStore(st)
						job := &workqueue.Job[*deliveryPayload]{ID: id, Attempts: attempt}
						switch outcome {
						case "done":
							wantState = "delivered"
							err = adapter.MarkDone(ctx, job)
						case "retry":
							err = adapter.MarkRetry(ctx, job, "synthetic failure", next)
						case "dead":
							err = adapter.MarkDead(ctx, job, "synthetic failure")
						}
					}
					if st.unsafe != 0 || len(st.calls) != 1 {
						t.Fatalf("unconditional writes=%d guarded writes=%d, want only one guarded write", st.unsafe, len(st.calls))
					}
					call := st.calls[0]
					if call.id != id || call.attempt != attempt || call.state != wantState {
						t.Errorf("claim identity or state changed: %+v", call)
					}
					if outcome != "done" && call.reason != "synthetic failure" {
						t.Error("retry/dead error was not preserved")
					}
					if outcome == "retry" && !call.next.Equal(next) {
						t.Error("retry cadence changed")
					}
					if outcome == "dead" && (call.next.Before(before) || call.next.After(time.Now().UTC())) {
						t.Error("terminal/fallback mark did not retain its current timestamp")
					}
					if !errors.Is(err, st.result) {
						t.Errorf("storage outcome lost: got %v want %v", err, st.result)
					}
					if errors.Is(err, workqueue.ErrLeaseLost) != (result == "lease-lost") {
						t.Errorf("worker lease-loss classification incorrect: %v", err)
					}
				})
			}
		}
	}
}
