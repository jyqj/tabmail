package outbound

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/workqueue"
)

// This fixture exercises the real Service lifecycle forwarding and real Worker
// join. SMTP outcomes and durable lease recovery require the separate real-PG
// and SMTP acceptance suites; no fake completion here certifies those paths.
type r5OutboundCallerStore struct {
	claims atomic.Int32
	marks  atomic.Int32
}

func (s *r5OutboundCallerStore) Claim(context.Context, time.Time, int) ([]*workqueue.Job[*outboundJob], error) {
	s.claims.Add(1)
	return []*workqueue.Job[*outboundJob]{{ID: uuid.New()}, {ID: uuid.New()}}, nil
}
func (s *r5OutboundCallerStore) MarkDone(context.Context, *workqueue.Job[*outboundJob]) error {
	s.marks.Add(1)
	return nil
}
func (s *r5OutboundCallerStore) MarkRetry(context.Context, *workqueue.Job[*outboundJob], string, time.Time) error {
	s.marks.Add(1)
	return nil
}
func (s *r5OutboundCallerStore) MarkDead(context.Context, *workqueue.Job[*outboundJob], string) error {
	s.marks.Add(1)
	return nil
}

type r5OutboundCallerGate struct {
	ch   chan struct{}
	once sync.Once
}

func r5OutboundCallerNewGate(t *testing.T) *r5OutboundCallerGate {
	t.Helper()
	g := &r5OutboundCallerGate{ch: make(chan struct{})}
	t.Cleanup(g.release)
	return g
}
func (g *r5OutboundCallerGate) release() { g.once.Do(func() { close(g.ch) }) }
func r5OutboundCallerWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("outbound caller barrier did not complete")
	}
}
func r5OutboundCallerFixture(t *testing.T, handler workqueue.Handler[*outboundJob]) (*Service, *r5OutboundCallerStore) {
	t.Helper()
	st := &r5OutboundCallerStore{}
	svc := &Service{cfg: config.Outbound{Enabled: true}, logger: zerolog.Nop()}
	svc.worker = workqueue.NewWorker[*outboundJob](st, handler, workqueue.FixedBackoff[*outboundJob]{Base: time.Second},
		nil, time.Minute, time.Millisecond, 7, zerolog.Nop())
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = svc.StopContext(ctx)
	})
	return svc, st
}

func TestR5OutboundShutdownCallerIdleAndDisabled(t *testing.T) {
	svc := &Service{logger: zerolog.Nop()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.StartWorker(context.Background()) // Disabled: must not lazily create a worker.
	if err := svc.StopContext(ctx); err != nil || svc.worker != nil {
		t.Fatalf("idle shutdown created a worker or returned failure: %v", err)
	}
	svc.Stop()
	svc.Shutdown()
}

func TestR5OutboundShutdownCallerCancellationReachesHandler(t *testing.T) {
	entered := make(chan struct{})
	var handled atomic.Int32
	svc, st := r5OutboundCallerFixture(t, func(ctx context.Context, _ *workqueue.Job[*outboundJob]) error {
		if handled.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	svc.StartWorker(context.Background())
	r5OutboundCallerWait(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.StopContext(ctx); err != nil {
		t.Fatal(err)
	}
	if handled.Load() != 1 || st.claims.Load() != 1 || st.marks.Load() != 0 {
		t.Fatal("service failed to cancel active worker or dispatched/marked remainder")
	}
}

func TestR5OutboundShutdownCallerIncompletePreservesErrorIdentity(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "cancelled"
		if expired {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			release := r5OutboundCallerNewGate(t)
			var handled atomic.Int32
			svc, st := r5OutboundCallerFixture(t, func(context.Context, *workqueue.Job[*outboundJob]) error {
				if handled.Add(1) == 1 {
					close(entered)
				}
				<-release.ch
				return nil
			})
			svc.StartWorker(context.Background())
			r5OutboundCallerWait(t, entered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			want := context.Canceled
			if expired {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				want = context.DeadlineExceeded
			}
			if err := svc.StopContext(ctx); !errors.Is(err, want) {
				t.Fatalf("service erased incomplete drain error: %v", err)
			}
			svc.StartWorker(context.Background()) // Old generation remains owned.
			if st.claims.Load() != 1 || st.marks.Load() != 0 {
				t.Fatal("incomplete stop restarted or acknowledged old work")
			}
			release.release()
			joined, joinCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer joinCancel()
			if err := svc.StopContext(joined); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestR5OutboundShutdownCallerLegacyStopRemainsGraceful(t *testing.T) {
	entered := make(chan struct{})
	release := r5OutboundCallerNewGate(t)
	var handled, cancelled atomic.Int32
	svc, st := r5OutboundCallerFixture(t, func(ctx context.Context, _ *workqueue.Job[*outboundJob]) error {
		if handled.Add(1) == 1 {
			close(entered)
			<-release.ch
		}
		if ctx.Err() != nil {
			cancelled.Add(1)
		}
		return nil
	})
	svc.StartWorker(context.Background())
	r5OutboundCallerWait(t, entered)
	stopStarted, stopped := make(chan struct{}), make(chan struct{})
	go func() { close(stopStarted); svc.Shutdown(); close(stopped) }()
	r5OutboundCallerWait(t, stopStarted)
	select {
	case <-stopped:
		t.Fatal("legacy Shutdown returned while handler was blocked")
	default:
	}
	release.release()
	r5OutboundCallerWait(t, stopped)
	if handled.Load() < 2 || cancelled.Load() != 0 || st.marks.Load() < 2 {
		t.Fatal("legacy Stop/Shutdown was silently changed to cancellation")
	}
}

func TestR5OutboundShutdownCallerSnapshotSharesWorkerMu(t *testing.T) {
	// Pin publication while both stop entrypoints contend for their snapshot.
	// No test performs an unsynchronized assignment to the service worker.
	svc := &Service{logger: zerolog.Nop()}
	svc.workerMu.Lock()
	bounded, legacy := make(chan struct{}), make(chan struct{})
	go func() { _ = svc.StopContext(context.Background()); close(bounded) }()
	go func() { svc.Stop(); close(legacy) }()
	st := &r5OutboundCallerStore{}
	svc.worker = workqueue.NewWorker[*outboundJob](st,
		func(context.Context, *workqueue.Job[*outboundJob]) error { return nil },
		workqueue.FixedBackoff[*outboundJob]{}, nil, time.Minute, time.Hour, 1, zerolog.Nop())
	svc.workerMu.Unlock()
	r5OutboundCallerWait(t, bounded)
	r5OutboundCallerWait(t, legacy)
	if st.claims.Load() != 0 {
		t.Fatal("pointer snapshot started an idle worker")
	}
}
