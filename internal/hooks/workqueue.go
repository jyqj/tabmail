package hooks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/store"
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
	MarkOutboxEventDoneClaim(ctx context.Context, id uuid.UUID, attempt int) error
	MarkOutboxEventRetryClaim(ctx context.Context, id uuid.UUID, attempt int, lastError string, nextAttemptAt time.Time) error
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
	return claimMarkError(a.store.MarkOutboxEventDoneClaim(ctx, job.ID, job.Attempts))
}

func (a *outboxStore) MarkRetry(ctx context.Context, job *workqueue.Job[*outboxPayload], lastError string, nextAttemptAt time.Time) error {
	return claimMarkError(a.store.MarkOutboxEventRetryClaim(ctx, job.ID, job.Attempts, lastError, nextAttemptAt))
}

// MarkDead documents the adapter protocol split across the two workqueue
// consumers. Outbound (internal/outbound/workqueue.go) is the mirror image:
// its MarkDone is a no-op because the SMTP handler owns the durable success
// write, while its MarkDead terminates the job. The outbox adapter is the
// opposite — MarkDone (MarkOutboxEventDone) is the real completion write, and
// MarkDead is unreachable in practice because FixedBackoff.Dead always
// returns false, so the worker only routes through MarkDone/MarkRetry. Should
// a future policy ever route here, the behavior is a retry
// (MarkOutboxEventRetry with nextAttemptAt=now): an outbox event is never
// dropped or dead-lettered; only webhook deliveries dead-letter.
func (a *outboxStore) MarkDead(ctx context.Context, job *workqueue.Job[*outboxPayload], lastError string) error {
	return claimMarkError(a.store.MarkOutboxEventRetryClaim(ctx, job.ID, job.Attempts, lastError, time.Now().UTC()))
}

// ---------- delivery store adapter ----------

type deliveryStore struct {
	store deliveryClaimMark
}

type deliveryClaimMark interface {
	ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error)
	MarkWebhookDeliveryDoneClaim(ctx context.Context, id uuid.UUID, attempt int) error
	MarkWebhookDeliveryRetryClaim(ctx context.Context, id uuid.UUID, attempt int, lastError string, nextAttemptAt time.Time, dead bool) error
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
	return claimMarkError(a.store.MarkWebhookDeliveryDoneClaim(ctx, job.ID, job.Attempts))
}

func (a *deliveryStore) MarkRetry(ctx context.Context, job *workqueue.Job[*deliveryPayload], lastError string, nextAttemptAt time.Time) error {
	return claimMarkError(a.store.MarkWebhookDeliveryRetryClaim(ctx, job.ID, job.Attempts, lastError, nextAttemptAt, false))
}

func (a *deliveryStore) MarkDead(ctx context.Context, job *workqueue.Job[*deliveryPayload], lastError string) error {
	return claimMarkError(a.store.MarkWebhookDeliveryRetryClaim(ctx, job.ID, job.Attempts, lastError, time.Now().UTC(), true))
}

// Both the generic worker and callers inspecting the store failure retain the
// lease-loss identity. A stale mark must not run success/dead-letter hooks.
func claimMarkError(err error) error {
	if errors.Is(err, store.ErrClaimLeaseLost) {
		return errors.Join(workqueue.ErrLeaseLost, err)
	}
	return err
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
