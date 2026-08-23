package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/workqueue"
)

// outboundJob is the workqueue payload for outbound jobs. It wraps the
// persisted *models.OutboundJob; the wrapper is local so the persisted struct
// stays pure and the delivery_token flows through the Store adapter's Lease.
type outboundJob struct {
	*models.OutboundJob
}

// outboundStore adapts the legacy outbound claim/mark SQLs to the generic
// workqueue.Store surface. The claim SQL (ClaimOutboundJobs) is unchanged; it
// generates a fresh delivery_token per claim, which we surface as the job's
// Lease.Token. Mark methods validate the token and translate a mismatch into
// workqueue.ErrLeaseLost so the worker logs and skips without panicking.
//
// MarkDone is a no-op: outbound's success write (CreateOutboundAttempt +
// MarkOutboundJobSent) is performed inside the handler, which returns nil to
// signal completion. The retry/dead marks still go through the adapter and
// carry the token guard.
type outboundStore struct {
	store outboundClaimMark
}

type outboundClaimMark interface {
	ClaimOutboundJobs(ctx context.Context, now time.Time, limit int) ([]*models.OutboundJob, error)
	MarkOutboundJobRetry(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, nextAttemptAt time.Time) error
	MarkOutboundJobFailed(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, dead bool) error
}

func newOutboundStore(s outboundClaimMark) *outboundStore { return &outboundStore{store: s} }

func (a *outboundStore) Claim(ctx context.Context, now time.Time, limit int) ([]*workqueue.Job[*outboundJob], error) {
	jobs, err := a.store.ClaimOutboundJobs(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*workqueue.Job[*outboundJob], len(jobs))
	for i, j := range jobs {
		out[i] = &workqueue.Job[*outboundJob]{
			ID:       j.ID,
			Attempts: j.Attempts,
			Payload:  &outboundJob{OutboundJob: j},
			Lease:    workqueue.Lease{Token: j.DeliveryToken},
		}
	}
	return out, nil
}

// MarkDone is a no-op: outbound's success write happens inside the handler.
func (a *outboundStore) MarkDone(_ context.Context, _ *workqueue.Job[*outboundJob]) error {
	return nil
}

func (a *outboundStore) MarkRetry(ctx context.Context, job *workqueue.Job[*outboundJob], lastError string, nextAttemptAt time.Time) error {
	if err := a.store.MarkOutboundJobRetry(ctx, job.ID, job.Lease.Token, lastError, nextAttemptAt); err != nil {
		return wrapTokenMismatch(err)
	}
	return nil
}

func (a *outboundStore) MarkDead(ctx context.Context, job *workqueue.Job[*outboundJob], lastError string) error {
	if err := a.store.MarkOutboundJobFailed(ctx, job.ID, job.Lease.Token, lastError, true); err != nil {
		return wrapTokenMismatch(err)
	}
	return nil
}

// wrapTokenMismatch translates the legacy string-based delivery-token-mismatch
// sentinel into workqueue.ErrLeaseLost so the generic worker treats it as a
// soft skip (log + continue) rather than a hard failure.
func wrapTokenMismatch(err error) error {
	if isTokenMismatch(err) {
		return workqueue.ErrLeaseLost
	}
	return err
}
