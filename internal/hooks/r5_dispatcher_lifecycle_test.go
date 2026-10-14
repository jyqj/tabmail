package hooks

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type r5DispatcherLifecycleStore struct {
	*testutil.FakeStore
	claims  chan string
	entered atomic.Int32
	exited  atomic.Int32
}

func (s *r5DispatcherLifecycleStore) claim(ctx context.Context, stage string) error {
	s.entered.Add(1)
	s.claims <- stage
	<-ctx.Done()
	s.exited.Add(1)
	return ctx.Err()
}

func (s *r5DispatcherLifecycleStore) ClaimOutboxEvents(ctx context.Context, _ time.Time, _ int) ([]*models.OutboxEvent, error) {
	return nil, s.claim(ctx, "outbox")
}

func (s *r5DispatcherLifecycleStore) ClaimWebhookDeliveries(ctx context.Context, _ time.Time, _ int) ([]*models.WebhookDelivery, error) {
	return nil, s.claim(ctx, "delivery")
}

// Exercise the public Run boundary, including the lazy pair construction.
// Running with -race detects unsynchronized replacement of either worker.
// Both stages must retain each caller's capacity and context ownership.
func TestR5DispatcherConcurrentRunsJoinAndRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const callersPerGroup = 16
		st := &r5DispatcherLifecycleStore{
			FakeStore: testutil.NewFakeStore(),
			claims:    make(chan string, 4*callersPerGroup),
		}
		d := New(Config{PollInterval: time.Hour}, zerolog.Nop()).BindStore(st)
		start := make(chan struct{})
		launch := func(ctx context.Context, count int) <-chan struct{} {
			var wg sync.WaitGroup
			for i := 0; i < count; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					d.Run(ctx)
				}()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			return done
		}
		ctxA, cancelA := context.WithCancel(context.Background())
		ctxB, cancelB := context.WithCancel(context.Background())
		doneA := launch(ctxA, callersPerGroup)
		doneB := launch(ctxB, callersPerGroup)
		defer func() { cancelA(); cancelB(); <-doneA; <-doneB }()
		close(start)

		awaitClaims := func(perStage int) {
			t.Helper()
			counts := map[string]int{}
			for i := 0; i < 2*perStage; i++ {
				select {
				case stage := <-st.claims:
					counts[stage]++
				case <-time.After(time.Second):
					t.Fatalf("Run callers did not enter both worker stages: %v", counts)
				}
			}
			if counts["outbox"] != perStage || counts["delivery"] != perStage {
				t.Fatalf("Run lost a stage or serialized caller capacity: %v", counts)
			}
		}
		awaitDone := func(done <-chan struct{}) {
			t.Helper()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Run returned without joining, or did not exit after cancellation")
			}
		}
		awaitClaims(2 * callersPerGroup)
		synctest.Wait()
		outbox, delivery := d.outboxWorker, d.deliveryWorker
		if outbox == nil || delivery == nil {
			t.Fatal("Run published an incomplete worker pair")
		}

		cancelA()
		awaitDone(doneA)
		synctest.Wait()
		if st.exited.Load() != 2*callersPerGroup {
			t.Fatalf("one caller group did not join exactly its own claims: %d", st.exited.Load())
		}
		select {
		case <-doneB:
			t.Fatal("canceling one caller group stopped independent Run callers")
		default:
		}
		cancelB()
		awaitDone(doneB)
		if st.entered.Load() != st.exited.Load() {
			t.Fatalf("Run left active claims after returning: entered=%d exited=%d", st.entered.Load(), st.exited.Load())
		}

		// A fully joined pair can start another generation. A canceled entry
		// must then return without adding claims or replacing the shared pair.
		ctxNext, cancelNext := context.WithCancel(context.Background())
		doneNext := launch(ctxNext, 1)
		defer func() { cancelNext(); <-doneNext }()
		awaitClaims(1)
		cancelNext()
		awaitDone(doneNext)
		d.Run(ctxNext)
		if d.outboxWorker != outbox || d.deliveryWorker != delivery {
			t.Error("a later Run replaced the initialized worker pair")
		}
		want := int32(4*callersPerGroup + 2)
		if st.entered.Load() != want || st.exited.Load() != want {
			t.Errorf("restart leaked claims or canceled Run entered storage: entered=%d exited=%d want=%d", st.entered.Load(), st.exited.Load(), want)
		}
	})
}
