package hooks

import (
	"context"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/workqueue"
)

// outboxPayload is the workqueue payload for outbox events. The outbox worker
// fans an event out into one webhook_delivery row per configured URL; it never
// marks an event dead, so the payload carries only the row itself.
type outboxPayload struct {
	*models.OutboxEvent
}

// deliveryPayload is the workqueue payload for webhook deliveries. The
// delivery worker POSTs the payload to the URL and tracks the outcome through
// terminal states "delivered"/"retry"/"dead". now is captured at claim time so
// the dead-letter LastTriedAt matches the legacy behavior.
type deliveryPayload struct {
	*models.WebhookDelivery
	now time.Time
}

// ---------- outbox store adapter ----------

type outboxStore struct {
	store outboxClaimMark
}

type outboxClaimMark interface {
	ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error)
	MarkOutboxEventDone(ctx context.Context, id uuid.UUID) error
	MarkOutboxEventRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error
}

func newOutboxStore(s outboxClaimMark) *outboxStore { return &outboxStore{store: s} }

func (a *outboxStore) Claim(ctx context.Context, now time.Time, limit int) ([]*workqueue.Job[*outboxPayload], error) {
	events, err := a.store.ClaimOutboxEvents(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*workqueue.Job[*outboxPayload], len(events))
	for i, e := range events {
		out[i] = &workqueue.Job[*outboxPayload]{
			ID:       e.ID,
			Attempts: e.Attempts,
			Payload:  &outboxPayload{OutboxEvent: e},
		}
	}
	return out, nil
}

func (a *outboxStore) MarkDone(ctx context.Context, job *workqueue.Job[*outboxPayload]) error {
	return a.store.MarkOutboxEventDone(ctx, job.ID)
}

func (a *outboxStore) MarkRetry(ctx context.Context, job *workqueue.Job[*outboxPayload], lastError string, nextAttemptAt time.Time) error {
	return a.store.MarkOutboxEventRetry(ctx, job.ID, lastError, nextAttemptAt)
}

// MarkDead is unreachable for outbox (FixedBackoff never returns dead). It is
// implemented as a retry to keep the Store contract satisfied.
func (a *outboxStore) MarkDead(ctx context.Context, job *workqueue.Job[*outboxPayload], lastError string) error {
	return a.store.MarkOutboxEventRetry(ctx, job.ID, lastError, time.Now().UTC())
}

// ---------- delivery store adapter ----------

type deliveryStore struct {
	store deliveryClaimMark
}

type deliveryClaimMark interface {
	ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error)
	MarkWebhookDeliveryDone(ctx context.Context, id uuid.UUID) error
	MarkWebhookDeliveryRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
}

func newDeliveryStore(s deliveryClaimMark) *deliveryStore { return &deliveryStore{store: s} }

func (a *deliveryStore) Claim(ctx context.Context, now time.Time, limit int) ([]*workqueue.Job[*deliveryPayload], error) {
	deliveries, err := a.store.ClaimWebhookDeliveries(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*workqueue.Job[*deliveryPayload], len(deliveries))
	for i, d := range deliveries {
		out[i] = &workqueue.Job[*deliveryPayload]{
			ID:       d.ID,
			Attempts: d.Attempts,
			Payload:  &deliveryPayload{WebhookDelivery: d, now: now},
		}
	}
	return out, nil
}

func (a *deliveryStore) MarkDone(ctx context.Context, job *workqueue.Job[*deliveryPayload]) error {
	return a.store.MarkWebhookDeliveryDone(ctx, job.ID)
}

func (a *deliveryStore) MarkRetry(ctx context.Context, job *workqueue.Job[*deliveryPayload], lastError string, nextAttemptAt time.Time) error {
	return a.store.MarkWebhookDeliveryRetry(ctx, job.ID, lastError, nextAttemptAt, false)
}

func (a *deliveryStore) MarkDead(ctx context.Context, job *workqueue.Job[*deliveryPayload], lastError string) error {
	return a.store.MarkWebhookDeliveryRetry(ctx, job.ID, lastError, time.Now().UTC(), true)
}

// ---------- outbox hooks (retry metric only; outbox never dies) ----------

type outboxHooks struct{}

func (outboxHooks) OnDone(_ context.Context, _ *workqueue.Job[*outboxPayload]) {}

func (outboxHooks) OnRetry(_ context.Context, _ *workqueue.Job[*outboxPayload], _ error) {
	metrics.WebhookRetried()
}

func (outboxHooks) OnDead(_ context.Context, _ *workqueue.Job[*outboxPayload], _ error) {}

// ---------- delivery hooks (metrics + dead-letter push) ----------

type deliveryHooks struct {
	dispatcher *Dispatcher
}

func (h *deliveryHooks) OnDone(_ context.Context, _ *workqueue.Job[*deliveryPayload]) {
	metrics.WebhookDelivered()
}

func (h *deliveryHooks) OnRetry(_ context.Context, _ *workqueue.Job[*deliveryPayload], _ error) {
	metrics.WebhookRetried()
}

func (h *deliveryHooks) OnDead(_ context.Context, job *workqueue.Job[*deliveryPayload], err error) {
	if job == nil || job.Payload == nil || job.Payload.WebhookDelivery == nil {
		return
	}
	d := job.Payload.WebhookDelivery
	metrics.WebhookFailed()
	h.dispatcher.pushDeadLetter(models.DeadLetter{
		ID:          d.ID.String(),
		URL:         d.URL,
		EventType:   d.EventType,
		Payload:     append([]byte(nil), d.Payload...),
		Attempts:    d.Attempts,
		LastError:   err.Error(),
		CreatedAt:   d.CreatedAt,
		LastTriedAt: job.Payload.now,
	})
}
