package workqueue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// These barriers control lifecycle interleavings without sleep-based scheduling.
// Timers below are failure watchdogs (or an explicit expired shutdown budget).
type r5ShutdownGate struct {
	ch   chan struct{}
	once sync.Once
}

func r5NewShutdownGate(t *testing.T) *r5ShutdownGate {
	t.Helper()
	g := &r5ShutdownGate{ch: make(chan struct{})}
	t.Cleanup(g.release)
	return g
}
func (g *r5ShutdownGate) release() { g.once.Do(func() { close(g.ch) }) }

func r5ShutdownWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown barrier did not complete")
	}
}

func r5ShutdownWaitError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("bounded shutdown did not return")
		return nil
	}
}

type r5ShutdownStore struct {
	claimFn func(context.Context) ([]*Job[int], error)
	markFn  func(context.Context) error
	claims  atomic.Int32
	marks   atomic.Int32
	limits  atomic.Int32
}

func (s *r5ShutdownStore) Claim(ctx context.Context, _ time.Time, limit int) ([]*Job[int], error) {
	s.claims.Add(1)
	s.limits.Store(int32(limit))
	return s.claimFn(ctx)
}
func (s *r5ShutdownStore) mark(ctx context.Context) error {
	s.marks.Add(1)
	if s.markFn != nil {
		return s.markFn(ctx)
	}
	return nil
}
func (s *r5ShutdownStore) MarkDone(ctx context.Context, _ *Job[int]) error {
	return s.mark(ctx)
}
func (s *r5ShutdownStore) MarkRetry(ctx context.Context, _ *Job[int], _ string, _ time.Time) error {
	return s.mark(ctx)
}
func (s *r5ShutdownStore) MarkDead(ctx context.Context, _ *Job[int], _ string) error {
	return s.mark(ctx)
}

type r5ShutdownHooks struct {
	calls atomic.Int32
	fn    func(context.Context)
}

func (h *r5ShutdownHooks) call(ctx context.Context) {
	h.calls.Add(1)
	if h.fn != nil {
		h.fn(ctx)
	}
}
func (h *r5ShutdownHooks) OnDone(ctx context.Context, _ *Job[int]) { h.call(ctx) }
func (h *r5ShutdownHooks) OnRetry(ctx context.Context, _ *Job[int], _ error) {
	h.call(ctx)
}
func (h *r5ShutdownHooks) OnDead(ctx context.Context, _ *Job[int], _ error) {
	h.call(ctx)
}

func r5ShutdownJobs() []*Job[int] {
	token := uuid.New()
	return []*Job[int]{
		{ID: uuid.New(), Attempts: 1, Payload: 1, Lease: Lease{Token: &token}},
		{ID: uuid.New(), Attempts: 1, Payload: 2, Lease: Lease{Token: &token}},
	}
}

func r5ShutdownWorker(t *testing.T, st *r5ShutdownStore, handler Handler[int], hooks Hooks[int]) *Worker[int] {
	t.Helper()
	w := NewWorker[int](st, handler, FixedBackoff[int]{Base: time.Second}, hooks,
		time.Minute, time.Millisecond, 7, newLogger())
	// Also signal cancellation on assertion failure; gate cleanups release any
	// deliberately uncooperative callback. Successful tests explicitly join.
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = w.StopContext(ctx)
	})
	return w
}

func r5ShutdownGeneration(w *Worker[int]) *workerGeneration {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	return w.generation
}

func r5ShutdownAsync(fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	return done
}

func r5ShutdownBounded(w *Worker[int]) <-chan error {
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result <- w.StopContext(ctx)
	}()
	return result
}

func TestR5ShutdownBoundaryCancelledEntryDoesNotClaim(t *testing.T) {
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return nil, nil }}
	w := r5ShutdownWorker(t, st, func(context.Context, *Job[int]) error { return nil }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx)
	w.Start(ctx)
	w.ProcessBatch(ctx)
	w.processBatch(ctx)
	if err := w.StopContext(ctx); err != nil {
		t.Fatalf("idle stop: %v", err)
	}
	w.Stop()
	if st.claims.Load() != 0 {
		t.Fatal("cancelled entry claimed work")
	}
}

func TestR5ShutdownBoundaryCooperativeCancelLeavesDurableLease(t *testing.T) {
	for _, returnNil := range []bool{false, true} {
		name := "handler_error"
		if returnNil {
			name = "handler_nil_after_cancel"
		}
		t.Run(name, func(t *testing.T) {
			jobs := r5ShutdownJobs()
			originalToken := *jobs[0].Lease.Token
			entered := make(chan struct{})
			var handled atomic.Int32
			st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return jobs, nil }}
			hooks := &r5ShutdownHooks{}
			w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
				if handled.Add(1) == 1 {
					close(entered)
				}
				<-ctx.Done()
				if returnNil {
					return nil
				}
				return ctx.Err()
			}, hooks)
			run := r5ShutdownAsync(func() { w.Run(context.Background()) })
			r5ShutdownWait(t, entered)
			if err := r5ShutdownWaitError(t, r5ShutdownBounded(w)); err != nil {
				t.Fatalf("cooperative drain: %v", err)
			}
			r5ShutdownWait(t, run)
			if handled.Load() != 1 || st.claims.Load() != 1 || st.marks.Load() != 0 || hooks.calls.Load() != 0 {
				t.Fatalf("cancel dispatched or acknowledged jobs: handled=%d claims=%d marks=%d hooks=%d",
					handled.Load(), st.claims.Load(), st.marks.Load(), hooks.calls.Load())
			}
			if st.limits.Load() != 7 || jobs[0].Attempts != 1 || *jobs[0].Lease.Token != originalToken || *jobs[1].Lease.Token != originalToken {
				t.Fatal("worker altered batch contract or durable lease representation")
			}
		})
	}
}

func TestR5ShutdownBoundaryUncooperativeDeadlineRetainsGeneration(t *testing.T) {
	entered := make(chan struct{})
	release := r5NewShutdownGate(t)
	var handled atomic.Int32
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
	w := r5ShutdownWorker(t, st, func(context.Context, *Job[int]) error {
		if handled.Add(1) == 1 {
			close(entered)
		}
		<-release.ch // Deliberately ignores cancellation.
		return nil
	}, nil)
	w.Start(context.Background())
	r5ShutdownWait(t, entered)
	g := r5ShutdownGeneration(w)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for i := 0; i < 3; i++ {
		if err := w.StopContext(expired); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("non-drained generation must report deadline, got %v", err)
		}
		w.Start(context.Background())
		w.Run(context.Background())
		w.ProcessBatch(context.Background())
		if r5ShutdownGeneration(w) != g {
			t.Fatal("timeout detached old generation")
		}
	}
	select {
	case <-g.done:
		t.Fatal("uncooperative handler was falsely joined")
	default:
	}
	legacyStop := r5ShutdownAsync(w.Stop)
	select {
	case <-legacyStop:
		t.Fatal("legacy Stop must still wait for actual exit")
	default:
	}
	release.release()
	r5ShutdownWait(t, legacyStop)
	if err := w.StopContext(expired); err != nil {
		t.Fatalf("joined generation must report success even with expired budget: %v", err)
	}
	if handled.Load() != 1 || st.claims.Load() != 1 || st.marks.Load() != 0 {
		t.Fatal("stopping generation dispatched, reclaimed, or acknowledged unfinished work")
	}
	w.ProcessBatch(context.Background())
	if r5ShutdownGeneration(w) == g || st.claims.Load() != 2 || st.marks.Load() != 2 {
		t.Fatal("drained generation did not allow a fresh complete batch")
	}
}

func TestR5ShutdownBoundaryClaimStopRaceDoesNotDispatch(t *testing.T) {
	entered := make(chan struct{})
	release := r5NewShutdownGate(t)
	var handled atomic.Int32
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) {
		close(entered)
		<-release.ch // Simulate a claim which commits despite cancellation.
		return r5ShutdownJobs(), nil
	}}
	w := r5ShutdownWorker(t, st, func(context.Context, *Job[int]) error { handled.Add(1); return nil }, nil)
	run := r5ShutdownAsync(func() { w.ProcessBatch(context.Background()) })
	r5ShutdownWait(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.StopContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked claim must remain in flight: %v", err)
	}
	release.release()
	r5ShutdownWait(t, run)
	if err := r5ShutdownWaitError(t, r5ShutdownBounded(w)); err != nil {
		t.Fatal(err)
	}
	if handled.Load() != 0 || st.marks.Load() != 0 || st.claims.Load() != 1 {
		t.Fatal("claim result dispatched or marked after shutdown")
	}
}

func TestR5ShutdownBoundaryParentCancellationStopsRemainder(t *testing.T) {
	entered := make(chan struct{})
	var handled atomic.Int32
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
	w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
		if handled.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := r5ShutdownAsync(func() { w.Run(ctx) })
	r5ShutdownWait(t, entered)
	cancel()
	r5ShutdownWait(t, run)
	if handled.Load() != 1 || st.claims.Load() != 1 || st.marks.Load() != 0 {
		t.Fatal("parent cancellation started or marked remaining work")
	}
}

func TestR5ShutdownBoundaryJoinIncludesMarkAndHook(t *testing.T) {
	for _, phase := range []string{"mark", "hook"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{})
			release := r5NewShutdownGate(t)
			var handled atomic.Int32
			block := func(context.Context) { close(entered); <-release.ch }
			st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
			hooks := &r5ShutdownHooks{}
			if phase == "mark" {
				st.markFn = func(ctx context.Context) error { block(ctx); return nil }
			} else {
				hooks.fn = block
			}
			w := r5ShutdownWorker(t, st, func(context.Context, *Job[int]) error { handled.Add(1); return nil }, hooks)
			run := r5ShutdownAsync(func() { w.ProcessBatch(context.Background()) })
			r5ShutdownWait(t, entered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := w.StopContext(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s was detached from join: %v", phase, err)
			}
			release.release()
			r5ShutdownWait(t, run)
			if err := w.StopContext(ctx); err != nil {
				t.Fatal(err)
			}
			if handled.Load() != 1 || st.marks.Load() != 1 || hooks.calls.Load() != 1 {
				t.Fatal("shutdown started remainder or lost successful first-job transition")
			}
		})
	}
}

func TestR5ShutdownBoundaryLegacyStopStillDrainsWholeBatch(t *testing.T) {
	entered := make(chan struct{})
	release := r5NewShutdownGate(t)
	var handled, cancelled atomic.Int32
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
	w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
		if handled.Add(1) == 1 {
			close(entered)
			<-release.ch
		}
		if ctx.Err() != nil {
			cancelled.Add(1)
		}
		return nil
	}, nil)
	w.Start(context.Background())
	r5ShutdownWait(t, entered)
	g := r5ShutdownGeneration(w)
	stop := r5ShutdownAsync(w.Stop)
	r5ShutdownWait(t, g.stopCh) // Stop admission has linearized, handler still blocked.
	select {
	case <-stop:
		t.Fatal("legacy graceful Stop returned before batch drained")
	default:
	}
	release.release()
	r5ShutdownWait(t, stop)
	if handled.Load() != 2 || st.marks.Load() != 2 || st.claims.Load() != 1 || cancelled.Load() != 0 {
		t.Fatal("legacy graceful batch contract changed")
	}
}

func TestR5ShutdownBoundaryConcurrentRunsKeepCapacity(t *testing.T) {
	const runners = 3
	entered := make(chan struct{}, runners)
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs()[:1], nil }}
	w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	var runs []<-chan struct{}
	for i := 0; i < runners; i++ {
		runs = append(runs, r5ShutdownAsync(func() { w.Run(context.Background()) }))
	}
	for i := 0; i < runners; i++ {
		r5ShutdownWait(t, entered)
	}
	if err := r5ShutdownWaitError(t, r5ShutdownBounded(w)); err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		r5ShutdownWait(t, run)
	}
	if st.claims.Load() != runners || st.marks.Load() != 0 {
		t.Fatal("Run concurrency was reduced or cancellation marked unfinished work")
	}
}

func TestR5ShutdownBoundaryRepeatedStartStopGenerations(t *testing.T) {
	entered := make(chan struct{}, 1)
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
	w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	var previous *workerGeneration
	for generation := 0; generation < 4; generation++ {
		w.Start(context.Background())
		r5ShutdownWait(t, entered)
		g := r5ShutdownGeneration(w)
		if g == previous {
			t.Fatal("new Start reused a drained generation")
		}
		for i := 0; i < 5; i++ {
			w.Start(context.Background())
		}
		w.lifecycleMu.Lock()
		runnerCount := len(g.runners)
		w.lifecycleMu.Unlock()
		if runnerCount != 1 {
			t.Fatalf("repeated Start created %d concurrent runners", runnerCount)
		}
		stops := []<-chan error{r5ShutdownBounded(w), r5ShutdownBounded(w), r5ShutdownBounded(w)}
		for _, stop := range stops {
			if err := r5ShutdownWaitError(t, stop); err != nil {
				t.Fatal(err)
			}
		}
		previous = g
	}
	if st.claims.Load() != 4 || st.marks.Load() != 0 {
		t.Fatal("generation restart overlapped claims or falsely marked cancelled work")
	}
}

func TestR5ShutdownBoundaryStopBeforeFirstPollDoesNotClaim(t *testing.T) {
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return nil, nil }}
	w := r5ShutdownWorker(t, st, func(context.Context, *Job[int]) error { return nil }, nil)
	w.pollInterval = time.Hour // Preserve and exercise Start's initial poll delay.
	w.Start(context.Background())
	if err := r5ShutdownWaitError(t, r5ShutdownBounded(w)); err != nil {
		t.Fatal(err)
	}
	if st.claims.Load() != 0 {
		t.Fatal("stopped idle worker claimed work")
	}
}

func TestR5ShutdownBoundaryConcurrentCancelUpgradesGracefulStop(t *testing.T) {
	entered := make(chan struct{})
	var handled atomic.Int32
	st := &r5ShutdownStore{claimFn: func(context.Context) ([]*Job[int], error) { return r5ShutdownJobs(), nil }}
	w := r5ShutdownWorker(t, st, func(ctx context.Context, _ *Job[int]) error {
		if handled.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	w.Start(context.Background())
	r5ShutdownWait(t, entered)
	g := r5ShutdownGeneration(w)
	graceful := r5ShutdownAsync(w.Stop)
	r5ShutdownWait(t, g.stopCh)
	if err := r5ShutdownWaitError(t, r5ShutdownBounded(w)); err != nil {
		t.Fatal(err)
	}
	r5ShutdownWait(t, graceful)
	if handled.Load() != 1 || st.claims.Load() != 1 || st.marks.Load() != 0 {
		t.Fatal("cancel upgrade dispatched or acknowledged remaining batch")
	}
}
