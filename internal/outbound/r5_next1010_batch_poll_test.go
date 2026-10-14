package outbound

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// Exercise the shipping service constructor, queue adapter, handler and
// recipient checkpoints. The in-memory store has the production one-row
// claim contract; SMTP is an observed adapter, not a second queue worker.
type next1010OutboundBatchStore struct {
	*testutil.FakeStore
	mu     sync.Mutex
	limits []int
	trace  []string
	failAt int
}

func (s *next1010OutboundBatchStore) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, event)
}

func (s *next1010OutboundBatchStore) ClaimOutboundJobs(ctx context.Context, now time.Time, limit int) ([]*models.OutboundJob, error) {
	s.mu.Lock()
	s.limits = append(s.limits, limit)
	s.trace = append(s.trace, "claim")
	fail := len(s.limits) == s.failAt
	s.mu.Unlock()
	if fail {
		return nil, errors.New("synthetic claim unavailable")
	}
	return s.FakeStore.ClaimOutboundJobs(ctx, now, limit)
}

func (s *next1010OutboundBatchStore) MarkOutboundJobSent(ctx context.Context, id uuid.UUID, token *uuid.UUID, code int, reply, messageID string) error {
	err := s.FakeStore.MarkOutboundJobSent(ctx, id, token, code, reply, messageID)
	if err == nil {
		s.record("sent")
	}
	return err
}

func (s *next1010OutboundBatchStore) snapshot() ([]int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.limits...), append([]string(nil), s.trace...)
}

type next1010OutboundBatchAdapter struct {
	st      *next1010OutboundBatchStore
	deliver func(context.Context) error
}

func (*next1010OutboundBatchAdapter) Name() string { return "batch-observer" }
func (a *next1010OutboundBatchAdapter) Deliver(ctx context.Context, _ *models.OutboundJob, _ []byte) (*DeliveryResult, error) {
	a.st.record("deliver")
	if a.deliver != nil {
		return nil, a.deliver(ctx)
	}
	return nil, nil
}

func next1010OutboundBatchFixture(t *testing.T, batch, count int) (*next1010OutboundBatchStore, *Service, []*models.OutboundJob, *next1010OutboundBatchAdapter) {
	t.Helper()
	st := &next1010OutboundBatchStore{FakeStore: testutil.NewFakeStore()}
	svc := NewService(config.Outbound{Enabled: true, BatchSize: batch, PollInterval: time.Hour, RetryDelay: time.Minute, MaxRetries: 5}, st, nopGovernance{}, zerolog.Nop())
	adapter := &next1010OutboundBatchAdapter{st: st}
	svc.adapter = adapter
	req := quotaTestSendRequest(uuid.New(), uuid.New())
	seedQuotaSender(t, st.FakeStore, &req)
	var jobs []*models.OutboundJob
	for i := range count {
		req.Subject = fmt.Sprintf("batch message %d", i)
		job, err := svc.Submit(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	return st, svc, jobs, adapter
}

func next1010OutboundStates(t *testing.T, st *next1010OutboundBatchStore, jobs []*models.OutboundJob) map[models.OutboundState]int {
	t.Helper()
	states := make(map[models.OutboundState]int)
	for _, job := range jobs {
		got, err := st.GetOutboundJob(context.Background(), job.ID)
		if err != nil || got == nil {
			t.Fatalf("read queued job: %v", err)
		}
		states[got.State]++
		if got.State == models.OutboundPending && (got.Attempts != 0 || got.DeliveryToken != nil || got.LeaseUntil != nil) {
			t.Errorf("unstarted row was leased: %+v", got)
		}
		if got.State == models.OutboundSent && got.Attempts != 1 {
			t.Errorf("sent row attempted %d times", got.Attempts)
		}
	}
	return states
}

func TestNext1010OutboundConfiguredBatch(t *testing.T) {
	for _, tc := range []struct {
		name               string
		batch, count, sent int
	}{
		{"configured_three", 3, 5, 3},
		{"one", 1, 3, 1},
		{"default", 0, 4, 4},
		{"empty", 3, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, svc, jobs, _ := next1010OutboundBatchFixture(t, tc.batch, tc.count)
			svc.ensureWorker().ProcessBatch(context.Background())
			states := next1010OutboundStates(t, st, jobs)
			if states[models.OutboundSent] != tc.sent || states[models.OutboundPending] != tc.count-tc.sent {
				t.Errorf("one poll states=%v; want sent=%d pending=%d", states, tc.sent, tc.count-tc.sent)
			}
			limits, trace := st.snapshot()
			wantTrace := make([]string, 0, 3*tc.sent+1)
			for range tc.sent {
				wantTrace = append(wantTrace, "claim", "deliver", "sent")
			}
			if tc.count == tc.sent && (tc.batch <= 0 || tc.sent < tc.batch) {
				wantTrace = append(wantTrace, "claim") // A single empty claim ends the poll.
			}
			if !reflect.DeepEqual(trace, wantTrace) {
				t.Errorf("dispatch order=%v want=%v", trace, wantTrace)
			}
			for _, limit := range limits {
				if limit != 1 {
					t.Errorf("one serial claim requested %d rows", limit)
				}
			}
		})
	}
}

func TestNext1010OutboundBatchNextPoll(t *testing.T) {
	st, svc, jobs, _ := next1010OutboundBatchFixture(t, 3, 5)
	worker := svc.ensureWorker()
	worker.ProcessBatch(context.Background())
	worker.ProcessBatch(context.Background())
	states := next1010OutboundStates(t, st, jobs)
	if states[models.OutboundSent] != 5 {
		t.Errorf("two three-job polls left work: %v", states)
	}
	limits, _ := st.snapshot()
	if len(limits) != 6 {
		t.Errorf("claims=%d want 5 jobs plus final empty claim", len(limits))
	}
}

func TestNext1010OutboundBatchClaimFailure(t *testing.T) {
	st, svc, jobs, _ := next1010OutboundBatchFixture(t, 3, 3)
	st.failAt = 2
	svc.ensureWorker().ProcessBatch(context.Background())
	limits, trace := st.snapshot()
	if len(limits) != 2 || !reflect.DeepEqual(trace, []string{"claim", "deliver", "sent", "claim"}) {
		t.Errorf("claim failure did not end the current poll: %v", trace)
	}
	states := next1010OutboundStates(t, st, jobs)
	if states[models.OutboundSent] != 1 || states[models.OutboundPending] != 2 {
		t.Errorf("claim failure touched unclaimed jobs: %v", states)
	}
}

func TestNext1010OutboundBatchBlockedThenCancelled(t *testing.T) {
	st, svc, jobs, adapter := next1010OutboundBatchFixture(t, 3, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan struct{})
	adapter.deliver = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
	go func() { defer close(done); svc.ensureWorker().ProcessBatch(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("production handler never reached its transport")
	}
	limits, _ := st.snapshot()
	states := next1010OutboundStates(t, st, jobs)
	if !reflect.DeepEqual(limits, []int{1}) || states[models.OutboundProcessing] != 1 || states[models.OutboundPending] != 2 {
		t.Errorf("slow current delivery prefetched later work: limits=%v states=%v", limits, states)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cooperative cancelled delivery did not join")
	}
	limits, trace := st.snapshot()
	states = next1010OutboundStates(t, st, jobs)
	if len(limits) != 1 || states[models.OutboundProcessing] != 1 || states[models.OutboundPending] != 2 || len(trace) != 2 {
		t.Errorf("cancellation dispatched or terminally marked extra work: trace=%v states=%v", trace, states)
	}
}
