// Package workqueue provides a generic, claim-based worker that drives job
// processing for any payload type. It unifies the three formerly-separate
// worker loops (ingest, hooks outbox/delivery, outbound) behind one Store[T] +
// RetryPolicy[T] + Hooks[T] seam.
//
// The worker preserves the exact retry cadence, dead-letter boundary, and
// lease semantics of each legacy worker: claim SQL, backoff formulas, and
// terminal-state strings remain in their existing Store adapters and policy
// implementations. This package holds only the dispatch loop and the
// policy/hook abstraction.
package workqueue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ErrLeaseLost is returned by a Store adapter when a mark operation fails
// because the lease no longer matches (e.g. an outbound delivery_token
// mismatch — the job was re-claimed by another worker). The Worker treats it
// as a soft skip: log a warning and move on without panicking.
var ErrLeaseLost = errors.New("workqueue: lease lost")

// Lease carries the claim guard a Store needs to validate a mark. For
// outbound it wraps the delivery_token; for the other stores it is unused
// (Token == nil).
type Lease struct {
	Token *uuid.UUID
}

// Job is the in-flight representation of a claimed row. Attempts is the
// post-claim value (the claim SQL has already incremented it), so Dead and
// NextAttempt policy methods see the same number the legacy code did.
type Job[T any] struct {
	ID       uuid.UUID
	Attempts int
	Payload  T
	Lease    Lease
}

// RetryPolicy decides whether a job is terminal and, when it is not, when it
// should be retried. Both methods receive the full Job so that per-row state
// (such as an outbound job's MaxAttempts) can participate in the decision.
// Attempts in the job is the post-claim count.
type RetryPolicy[T any] interface {
	// Dead reports whether the job has exhausted its retries.
	Dead(job *Job[T]) bool
	// NextAttempt returns the delay until the next attempt for a non-dead job.
	NextAttempt(job *Job[T]) time.Duration
}

// ExponentialBackoff implements ingest's backoff: base*2^min(attempts-1, capExp)
// plus uniform jitter in [0, jitter). A job is dead once Attempts >= maxRetries.
type ExponentialBackoff[T any] struct {
	Base    time.Duration
	CapExp  int        // exponent cap (ingest uses 8)
	Jitter  func() time.Duration
	Max     int        // dead boundary in post-claim attempts (>= maxRetries)
}

func (p ExponentialBackoff[T]) Dead(job *Job[T]) bool { return job.Attempts >= p.Max }

func (p ExponentialBackoff[T]) NextAttempt(job *Job[T]) time.Duration {
	exp := job.Attempts - 1
	if exp < 0 {
		exp = 0
	}
	if exp > p.CapExp {
		exp = p.CapExp
	}
	d := p.Base * time.Duration(1<<uint(exp))
	if p.Jitter != nil {
		d += p.Jitter()
	}
	return d
}

// LinearBackoff implements webhook delivery's backoff: base*attempts, no
// jitter. Dead once Attempts >= maxRetries.
type LinearBackoff[T any] struct {
	Base time.Duration
	Max  int
}

func (p LinearBackoff[T]) Dead(job *Job[T]) bool { return job.Attempts >= p.Max }

func (p LinearBackoff[T]) NextAttempt(job *Job[T]) time.Duration {
	return p.Base * time.Duration(job.Attempts)
}

// FixedBackoff implements webhook outbox's backoff: a constant base delay
// regardless of attempt count. The outbox never marks an event dead — it
// retries indefinitely — so Dead is always false.
type FixedBackoff[T any] struct {
	Base time.Duration
}

func (p FixedBackoff[T]) Dead(*Job[T]) bool { return false }

func (p FixedBackoff[T]) NextAttempt(*Job[T]) time.Duration { return p.Base }

// ExponentialCappedBackoff implements outbound's backoff:
// base*2^attempts, capped at Cap. A job is dead once the next attempt would
// exceed MaxAttempts, matching the legacy attempt := job.Attempts+1;
// attempt >= job.MaxAttempts check (the +1 lives here).
//
// MaxAttempts is read from the job because Submit freezes it per row; the
// policy reads it via the maxAttempts func so the concrete payload type stays
// out of this package.
type ExponentialCappedBackoff[T any] struct {
	Base         time.Duration
	Cap          time.Duration
	MaxAttempts  func(job *Job[T]) int
}

func (p ExponentialCappedBackoff[T]) Dead(job *Job[T]) bool {
	attempt := job.Attempts + 1
	return attempt >= p.MaxAttempts(job)
}

func (p ExponentialCappedBackoff[T]) NextAttempt(job *Job[T]) time.Duration {
	attempt := job.Attempts + 1
	d := p.Base * time.Duration(1<<uint(attempt))
	if p.Cap > 0 && d > p.Cap {
		d = p.Cap
	}
	return d
}

// Store is the claim/mark surface a Worker drives. Each implementation wraps
// one legacy claim SQL and its mark SQLs unchanged; only the method names are
// normalized. Terminal-state string differences ("done"/"delivered"/"sent",
// "dead"/"failed") stay private to each adapter.
type Store[T any] interface {
	Claim(ctx context.Context, now time.Time, limit int) ([]*Job[T], error)
	MarkDone(ctx context.Context, job *Job[T]) error
	MarkRetry(ctx context.Context, job *Job[T], lastError string, nextAttemptAt time.Time) error
	MarkDead(ctx context.Context, job *Job[T], lastError string) error
}

// Hooks observes job lifecycle transitions. The zero-value Hooks (nil fields)
// is a no-op; concrete workers plug in orphan cleanup, dead-letter push, and
// metrics here.
type Hooks[T any] interface {
	OnDone(ctx context.Context, job *Job[T])
	OnRetry(ctx context.Context, job *Job[T], err error)
	OnDead(ctx context.Context, job *Job[T], err error)
}

// noopHooks is the default when none are supplied.
type noopHooks[T any] struct{}

func (noopHooks[T]) OnDone(context.Context, *Job[T])        {}
func (noopHooks[T]) OnRetry(context.Context, *Job[T], error) {}
func (noopHooks[T]) OnDead(context.Context, *Job[T], error)  {}

// Handler processes one claimed job. Returning nil marks the job done; a
// non-nil error routes through RetryPolicy (dead → MarkDead, else MarkRetry).
// For adapters whose success write is asymmetric (outbound writes attempt +
// sent inside the handler), the handler returns nil after performing the
// write and the Store's MarkDone is a no-op.
type Handler[T any] func(ctx context.Context, job *Job[T]) error

// Worker drives a claim loop against one Store. Run blocks until ctx is
// cancelled (ingest/hooks shape); Start launches a goroutine and Stop waits
// for the in-flight batch to finish (outbound shape).
type Worker[T any] struct {
	store        Store[T]
	handler      Handler[T]
	policy       RetryPolicy[T]
	hooks        Hooks[T]
	leaseTTL     time.Duration // informational; claim SQL owns the real lease
	pollInterval time.Duration
	batchSize    int
	logger       zerolog.Logger

	wg       sync.WaitGroup
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewWorker constructs a Worker. leaseTTL is kept for readability but the
// actual lease duration is owned by the claim SQL in the Store adapter.
func NewWorker[T any](
	store Store[T],
	handler Handler[T],
	policy RetryPolicy[T],
	hooks Hooks[T],
	leaseTTL, pollInterval time.Duration,
	batchSize int,
	logger zerolog.Logger,
) *Worker[T] {
	if hooks == nil {
		hooks = noopHooks[T]{}
	}
	if leaseTTL <= 0 {
		leaseTTL = 5 * time.Minute
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Worker[T]{
		store:        store,
		handler:      handler,
		policy:       policy,
		hooks:        hooks,
		leaseTTL:     leaseTTL,
		pollInterval: pollInterval,
		batchSize:    batchSize,
		logger:       logger,
	}
}

// Run processes batches until ctx is cancelled. Each iteration claims a batch,
// runs every job through processOne, then waits for either ctx.Done or the
// poll ticker. Matches the legacy ingest/hooks loop shape.
func (w *Worker[T]) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		w.processBatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Start launches Run in a goroutine. Use Stop to wait for the in-flight batch
// to finish. Matches the legacy outbound loop shape.
func (w *Worker[T]) Start(ctx context.Context) {
	w.stopCh = make(chan struct{})
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.processBatch(ctx)
			}
		}
	}()
}

// Stop signals a Start-ed worker to exit and waits for the goroutine to drain.
// In-flight processOne calls finish before the goroutine returns because
// processBatch is synchronous.
func (w *Worker[T]) Stop() {
	if w.stopCh != nil {
		w.stopOnce.Do(func() { close(w.stopCh) })
	}
	w.wg.Wait()
}

// ProcessBatch runs a single claim+process cycle without waiting on the poll
// ticker. It is intended for tests and synchronous drivers; the normal entry
// points are Run (blocking) and Start/Stop (background).
func (w *Worker[T]) ProcessBatch(ctx context.Context) {
	w.processBatch(ctx)
}

func (w *Worker[T]) processBatch(ctx context.Context) {
	jobs, err := w.store.Claim(ctx, time.Now().UTC(), w.batchSize)
	if err != nil {
		w.logger.Warn().Err(err).Msg("workqueue: claim")
		return
	}
	for _, job := range jobs {
		w.processOne(ctx, job)
	}
}

// processOne dispatches a single claimed job: run the handler, then route the
// result through RetryPolicy to MarkDone / MarkRetry / MarkDead, invoking the
// matching Hook. A lease-lost error from a mark is logged and skipped without
// panicking (the job was re-claimed by another worker).
func (w *Worker[T]) processOne(ctx context.Context, job *Job[T]) {
	err := w.handler(ctx, job)
	if err == nil {
		if markErr := w.store.MarkDone(ctx, job); markErr != nil {
			if errors.Is(markErr, ErrLeaseLost) {
				w.logger.Warn().Str("job_id", job.ID.String()).Msg("workqueue: lease lost on mark-done")
				return
			}
			w.logger.Error().Err(markErr).Str("job_id", job.ID.String()).Msg("workqueue: mark done")
			return
		}
		w.hooks.OnDone(ctx, job)
		return
	}
	if w.policy.Dead(job) {
		if markErr := w.store.MarkDead(ctx, job, err.Error()); markErr != nil {
			if errors.Is(markErr, ErrLeaseLost) {
				w.logger.Warn().Str("job_id", job.ID.String()).Msg("workqueue: lease lost on mark-dead")
				return
			}
			w.logger.Error().Err(markErr).Str("job_id", job.ID.String()).Msg("workqueue: mark dead")
			return
		}
		w.hooks.OnDead(ctx, job, err)
		return
	}
	next := time.Now().UTC().Add(w.policy.NextAttempt(job))
	if markErr := w.store.MarkRetry(ctx, job, err.Error(), next); markErr != nil {
		if errors.Is(markErr, ErrLeaseLost) {
			w.logger.Warn().Str("job_id", job.ID.String()).Msg("workqueue: lease lost on mark-retry")
			return
		}
		w.logger.Error().Err(markErr).Str("job_id", job.ID.String()).Msg("workqueue: mark retry")
		return
	}
	w.hooks.OnRetry(ctx, job, err)
}
