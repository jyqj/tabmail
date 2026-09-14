package ingest

import (
	"context"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/workqueue"
	"time"
)

type ingestJob struct {
	*models.IngestJob
	claim *store.IngressClaim
}
type ingestStore struct{ store store.IngressLedger }

func newIngestStore(s store.IngressLedger) *ingestStore { return &ingestStore{store: s} }

// Claim one receipt at a time. A batch must not lease later receipts while an
// earlier one waits on storage; processOne is bounded below the database lease.
func (a *ingestStore) Claim(ctx context.Context, _ time.Time, _ int) ([]*workqueue.Job[*ingestJob], error) {
	c, err := a.store.ClaimIngress(ctx)
	if err != nil || c == nil {
		return nil, err
	}
	return []*workqueue.Job[*ingestJob]{{ID: c.Job.ID, Attempts: c.Job.Attempts,
		Payload: &ingestJob{IngestJob: c.Job, claim: c}, Lease: workqueue.Lease{Token: &c.Token}}}, nil
}
func (a *ingestStore) MarkDone(ctx context.Context, j *workqueue.Job[*ingestJob]) error {
	return a.store.FinishIngress(ctx, j.Payload.claim, 1, time.Now().UTC())
}
func (a *ingestStore) MarkRetry(ctx context.Context, j *workqueue.Job[*ingestJob], _ string, next time.Time) error {
	return a.store.FinishIngress(ctx, j.Payload.claim, int(^uint(0)>>1), next)
}
func (a *ingestStore) MarkDead(ctx context.Context, j *workqueue.Job[*ingestJob], _ string) error {
	return a.store.FinishIngress(ctx, j.Payload.claim, 1, time.Now().UTC())
}

// Accepted raw bytes are held by the receipt until safe completed-receipt GC.
// Neither zero deliveries nor retry exhaustion authorizes object deletion.
type ingestHooks struct{ svc *Service }

func (*ingestHooks) OnDone(_ context.Context, _ *workqueue.Job[*ingestJob]) {
	metrics.IngestJobProcessed()
}
func (*ingestHooks) OnRetry(_ context.Context, _ *workqueue.Job[*ingestJob], _ error) {
	metrics.IngestJobRetried()
}
func (*ingestHooks) OnDead(_ context.Context, _ *workqueue.Job[*ingestJob], _ error) {
	metrics.IngestJobDead()
}
