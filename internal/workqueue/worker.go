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
	Base   time.Duration
	CapExp int // exponent cap (ingest uses 8)
	Jitter func() time.Duration
	Max    int // dead boundary in post-claim attempts (>= maxRetries)
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
	Base        time.Duration
	Cap         time.Duration
	MaxAttempts func(job *Job[T]) int
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

func (noopHooks[T]) OnDone(context.Context, *Job[T])         {}
func (noopHooks[T]) OnRetry(context.Context, *Job[T], error) {}
func (noopHooks[T]) OnDead(context.Context, *Job[T], error)  {}

// Handler processes one claimed job. Returning nil marks the job done; a
// non-nil error routes through RetryPolicy (dead → MarkDead, else MarkRetry).
// For adapters whose success write is asymmetric (outbound writes attempt +
// sent inside the handler), the handler returns nil after performing the
// write and the Store's MarkDone is a no-op.
type Handler[T any] func(ctx context.Context, job *Job[T]) error

// Worker drives a claim loop against one Store. Run blocks until ctx is
// cancelled (ingest/hooks shape); Start launches a goroutine. Stop preserves
// the legacy graceful batch join; StopContext cancels and bounds the join.
type Worker[T any] struct {
	store        Store[T]
	handler      Handler[T]
	policy       RetryPolicy[T]
	hooks        Hooks[T]
	leaseTTL     time.Duration // informational; claim SQL owns the real lease
	pollInterval time.Duration
	batchSize    int
	logger       zerolog.Logger

	// lifecycleMu protects generation membership and admission, never I/O.
	lifecycleMu sync.Mutex
	generation  *workerGeneration
}

// A timed-out shutdown retains its generation until every registered runner
// actually exits. In particular, timeout is not permission to start a replacement.
// Multiple Run callers share the generation without serializing their work.
type workerGeneration struct {
	stopCh    chan struct{}
	done      chan struct{}
	stopping  bool
	cancelled bool
	runners   map[*workerRun]struct{}
}

type workerRun struct {
	ctx    context.Context
	cancel context.CancelFunc
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

// Run processes immediately, then polls until its context or the generation is
// stopped. Concurrent Run callers retain their independent processing capacity.
func (w *Worker[T]) Run(ctx context.Context) {
	g, r := w.register(ctx, false)
	if r == nil {
		return
	}
	w.run(g, r, true)
}

// Start launches one background runner, preserving the initial poll delay.
// Repeated Start calls while any runner is active are no-ops, including during a
// timed-out shutdown. A new generation can start only after the old one exits.
func (w *Worker[T]) Start(ctx context.Context) {
	g, r := w.register(ctx, true)
	if r != nil {
		go w.run(g, r, false)
	}
}

func (w *Worker[T]) register(ctx context.Context, start bool) (*workerGeneration, *workerRun) {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if ctx.Err() != nil {
		return nil, nil
	}
	g := w.generation
	if g != nil && len(g.runners) != 0 {
		if g.stopping || start {
			return nil, nil
		}
	} else {
		g = &workerGeneration{stopCh: make(chan struct{}), done: make(chan struct{}), runners: make(map[*workerRun]struct{})}
		w.generation = g
	}
	runCtx, cancel := context.WithCancel(ctx)
	r := &workerRun{ctx: runCtx, cancel: cancel}
	g.runners[r] = struct{}{}
	return g, r
}

func (w *Worker[T]) finish(g *workerGeneration, r *workerRun) {
	r.cancel()
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	delete(g.runners, r)
	if len(g.runners) == 0 {
		close(g.done)
	}
}

func (w *Worker[T]) run(g *workerGeneration, r *workerRun, immediate bool) {
	defer w.finish(g, r)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	if immediate {
		w.processManagedBatch(r.ctx, g)
	}
	for {
		select {
		case <-g.stopCh:
			return
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			w.processManagedBatch(r.ctx, g)
		}
	}
}

// stopGeneration linearizes shutdown with claim/handler admission. Calls that
// were admitted before shutdown are in flight and receive cancellation; no
// lock is held across Store, Handler or Hooks code that might ignore context.
func (w *Worker[T]) stopGeneration(cancel bool) *workerGeneration {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	g := w.generation
	if g == nil {
		return nil
	}
	if !g.stopping {
		g.stopping = true
		close(g.stopCh)
	}
	if cancel && !g.cancelled {
		g.cancelled = true
		for r := range g.runners {
			r.cancel()
		}
	}
	return g
}

// Stop preserves the legacy graceful contract: stop new claims, finish the
// already-claimed batch and wait for all registered runners, without cancelling
// their contexts. It is intentionally unbounded. A concurrent StopContext may
// upgrade that same generation to cancellation. Production callers that need a
// deadline must explicitly migrate to StopContext and handle its error.
func (w *Worker[T]) Stop() {
	if g := w.stopGeneration(false); g != nil {
		<-g.done
	}
}

// StopContext stops new claims and job dispatch, cancels in-flight contexts and
// waits for actual runner exit. nil means all runners (including marks/hooks)
// have exited, NOT that all durable jobs completed successfully. ctx.Err() means
// drain was not observed by the deadline/cancellation; the generation remains
// owned and cannot overlap a replacement. Retrying StopContext waits on it again.
// Even an already-cancelled ctx signals shutdown. No goroutine is detached to
// fake a successful join. The caller must supply a deadline for a bounded wait.
// Unfinished/unstarted jobs retain their existing durable lease for recovery;
// this method does not acknowledge, requeue, or release them.
func (w *Worker[T]) StopContext(ctx context.Context) error {
	g := w.stopGeneration(true)
	if g == nil {
		return nil
	}
	select {
	case <-g.done:
		return nil
	default:
	}
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		// Prefer an already-completed join over a simultaneously expired budget.
		select {
		case <-g.done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

// ProcessBatch registers one synchronous claim+process cycle in the current
// generation. It can run concurrently with Run; shutdown joins it as well.
func (w *Worker[T]) ProcessBatch(ctx context.Context) {
	g, r := w.register(ctx, false)
	if r == nil {
		return
	}
	defer w.finish(g, r)
	w.processManagedBatch(r.ctx, g)
}

func (w *Worker[T]) processBatch(ctx context.Context) {
	w.processManagedBatch(ctx, nil)
}

func (w *Worker[T]) admit(ctx context.Context, g *workerGeneration, claim bool) bool {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	return ctx.Err() == nil && (g == nil || (!g.cancelled && (!claim || !g.stopping)))
}

func (w *Worker[T]) processManagedBatch(ctx context.Context, g *workerGeneration) {
	if !w.admit(ctx, g, true) {
		return
	}
	jobs, err := w.store.Claim(ctx, time.Now().UTC(), w.batchSize)
	if err != nil {
		w.logger.Warn().Err(err).Msg("workqueue: claim")
		return
	}
	for _, job := range jobs {
		if !w.admit(ctx, g, false) {
			return
		}
		w.processOne(ctx, job)
	}
}

// processOne dispatches a single claimed job: run the handler, then route the
// result through RetryPolicy to MarkDone / MarkRetry / MarkDead, invoking the
// matching Hook. A lease-lost error from a mark is logged and skipped without
// panicking (the job was re-claimed by another worker).
func (w *Worker[T]) processOne(ctx context.Context, job *Job[T]) {
	err := w.handler(ctx, job)
	// Cancellation is not a terminal job outcome. A handler may have returned
	// nil despite cancellation, or may have performed its own durable checkpoint.
	// Do not invent a completion/retry/dead mark; leave recovery to the lease owner.
	if ctx.Err() != nil {
		return
	}
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
