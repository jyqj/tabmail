package ingest

import (
	"context"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/workqueue"
)

// ingestJob is the workqueue payload for ingest jobs. It wraps the persisted
// *models.IngestJob with a transient delivered count that the handler writes
// and the OnDone hook reads to decide whether the raw object is now orphaned.
// The wrapper is local to this package so models.IngestJob stays a pure
// persistence struct.
type ingestJob struct {
	*models.IngestJob
	delivered int
}

// ingestStore adapts the legacy ingest claim/mark methods to the generic
// workqueue.Store surface. The underlying SQL (ClaimIngestJobs /
// MarkIngestJobDone / MarkIngestJobRetry) is unchanged; this wrapper only
// normalizes method names and boxes the row into an ingestJob payload. The
// terminal-state strings ("done"/"retry"/"dead") stay inside MarkIngestJobRetry.
type ingestStore struct {
	store claimMarkIngest
}

type claimMarkIngest interface {
	ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error)
	MarkIngestJobDone(ctx context.Context, id uuid.UUID) error
	MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
}

func newIngestStore(s claimMarkIngest) *ingestStore { return &ingestStore{store: s} }

func (a *ingestStore) Claim(ctx context.Context, now time.Time, limit int) ([]*workqueue.Job[*ingestJob], error) {
	jobs, err := a.store.ClaimIngestJobs(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*workqueue.Job[*ingestJob], len(jobs))
	for i, j := range jobs {
		out[i] = &workqueue.Job[*ingestJob]{
			ID:       j.ID,
			Attempts: j.Attempts,
			Payload:  &ingestJob{IngestJob: j},
		}
	}
	return out, nil
}

func (a *ingestStore) MarkDone(ctx context.Context, job *workqueue.Job[*ingestJob]) error {
	return a.store.MarkIngestJobDone(ctx, job.ID)
}

func (a *ingestStore) MarkRetry(ctx context.Context, job *workqueue.Job[*ingestJob], lastError string, nextAttemptAt time.Time) error {
	return a.store.MarkIngestJobRetry(ctx, job.ID, lastError, nextAttemptAt, false)
}

func (a *ingestStore) MarkDead(ctx context.Context, job *workqueue.Job[*ingestJob], lastError string) error {
	// nextAttemptAt is unused for dead rows (the claim SQL never selects state
	// 'dead'); pass now() to mirror the legacy write shape.
	return a.store.MarkIngestJobRetry(ctx, job.ID, lastError, time.Now().UTC(), true)
}

// ingestHooks wires the legacy metrics and orphan-raw-object cleanup into the
// workqueue.Hooks surface. OnDone releases the raw object when nothing was
// delivered and emits the processed/latency metrics; OnRetry/OnDead emit the
// retry/dead metrics, and OnDead also releases the raw object (the job is
// terminal).
type ingestHooks struct {
	svc *Service
}

func (h *ingestHooks) OnDone(ctx context.Context, job *workqueue.Job[*ingestJob]) {
	if job == nil || job.Payload == nil || job.Payload.IngestJob == nil {
		return
	}
	m := job.Payload.IngestJob
	if job.Payload.delivered == 0 {
		h.svc.deleteRawObjectIfOrphaned(ctx, m.RawObjectKey, "zero_delivery_ingest_job")
	}
	metrics.IngestJobProcessed()
	metrics.ObserveIngestJobLatency(time.Since(m.CreatedAt))
}

func (h *ingestHooks) OnRetry(_ context.Context, _ *workqueue.Job[*ingestJob], _ error) {
	metrics.IngestJobRetried()
}

func (h *ingestHooks) OnDead(ctx context.Context, job *workqueue.Job[*ingestJob], _ error) {
	if job == nil || job.Payload == nil || job.Payload.IngestJob == nil {
		return
	}
	m := job.Payload.IngestJob
	metrics.IngestJobDead()
	metrics.ObserveIngestJobLatency(time.Since(m.CreatedAt))
	h.svc.deleteRawObjectIfOrphaned(ctx, m.RawObjectKey, "dead_ingest_job")
}
