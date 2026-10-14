package ingest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// The production ingress service and queue adapter share the existing
// in-memory receipt ledger. Observers only expose claim/finish ordering and
// cooperative I/O failures; they do not implement another delivery state machine.
type next1010IngestBatchStore struct {
	*testutil.FakeStore
	mu      sync.Mutex
	trace   []string
	claims  int
	failAt  int
	deliver func(context.Context) error
}

func (s *next1010IngestBatchStore) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, event)
}

func (s *next1010IngestBatchStore) ClaimIngress(ctx context.Context) (*store.IngressClaim, error) {
	s.mu.Lock()
	s.claims++
	s.trace = append(s.trace, "claim")
	fail := s.claims == s.failAt
	s.mu.Unlock()
	if fail {
		return nil, errors.New("synthetic ingress claim unavailable")
	}
	return s.FakeStore.ClaimIngress(ctx)
}

func (s *next1010IngestBatchStore) DeliverIngress(ctx context.Context, claim *store.IngressClaim, m *models.Message, mailboxLimit, dailyLimit int) (bool, error) {
	s.record("deliver")
	if s.deliver != nil {
		if err := s.deliver(ctx); err != nil {
			return false, err
		}
	}
	return s.FakeStore.DeliverIngress(ctx, claim, m, mailboxLimit, dailyLimit)
}

func (s *next1010IngestBatchStore) FinishIngress(ctx context.Context, claim *store.IngressClaim, maxAttempts int, next time.Time) error {
	err := s.FakeStore.FinishIngress(ctx, claim, maxAttempts, next)
	if err == nil {
		s.record("finish")
	}
	return err
}

func (s *next1010IngestBatchStore) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.trace...)
}

func next1010IngestBatchFixture(t *testing.T, batch, count int) (*next1010IngestBatchStore, *Service) {
	t.Helper()
	base, objects, _ := newDurableCleanupService(t, 4096, true)
	st := &next1010IngestBatchStore{FakeStore: base}
	svc := NewService(st, objects, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: true, BatchSize: batch, PollInterval: time.Hour, MaxRetries: 5}, zerolog.Nop())
	for i := range count {
		result, err := svc.Accept(context.Background(), Envelope{Source: "batch-fixture", MailFrom: "sender@example.test", Recipients: []string{"reader@mail.test"}}, []byte(fmt.Sprintf("Subject: batch %d\r\n\r\nmessage %d\r\n", i, i)))
		if err != nil || !result.Queued {
			t.Fatalf("accept receipt %d: result=%v err=%v", i, result, err)
		}
	}
	return st, svc
}

func next1010IngestStates(t *testing.T, st *next1010IngestBatchStore, count int) map[string]int {
	t.Helper()
	jobs, total, err := st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 100}, "", "", "")
	if err != nil || total != count || len(jobs) != count {
		t.Fatalf("read receipt ledger: total=%d rows=%d error=%v", total, len(jobs), err)
	}
	states := make(map[string]int)
	for _, job := range jobs {
		states[job.State]++
		if job.State == "pending" && (job.Attempts != 0 || job.ClaimedAt != nil || job.LeaseUntil != nil) {
			t.Errorf("unstarted receipt was leased: %+v", job)
		}
		if job.State == "done" && job.Attempts != 1 {
			t.Errorf("completed receipt attempted %d times", job.Attempts)
		}
	}
	return states
}

func TestNext1010IngestConfiguredBatch(t *testing.T) {
	for _, tc := range []struct {
		name               string
		batch, count, done int
	}{
		{"configured_three", 3, 5, 3},
		{"one", 1, 3, 1},
		{"default", 0, 4, 4},
		{"empty", 3, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, svc := next1010IngestBatchFixture(t, tc.batch, tc.count)
			svc.ProcessBatch(context.Background())
			states := next1010IngestStates(t, st, tc.count)
			if states["done"] != tc.done || states["pending"] != tc.count-tc.done {
				t.Errorf("one poll states=%v; want done=%d pending=%d", states, tc.done, tc.count-tc.done)
			}
			wantTrace := make([]string, 0, 3*tc.done+1)
			for range tc.done {
				wantTrace = append(wantTrace, "claim", "deliver", "finish")
			}
			if tc.count == tc.done && (tc.batch <= 0 || tc.done < tc.batch) {
				wantTrace = append(wantTrace, "claim")
			}
			if got := st.snapshot(); !reflect.DeepEqual(got, wantTrace) {
				t.Errorf("dispatch order=%v want=%v", got, wantTrace)
			}
		})
	}
}

func TestNext1010IngestBatchNextPoll(t *testing.T) {
	st, svc := next1010IngestBatchFixture(t, 3, 5)
	svc.ProcessBatch(context.Background())
	svc.ProcessBatch(context.Background())
	if states := next1010IngestStates(t, st, 5); states["done"] != 5 {
		t.Errorf("two three-job polls left work: %v", states)
	}
	if got := st.snapshot(); len(got) != 16 {
		t.Errorf("want 5 claim/deliver/finish groups plus final empty claim: %v", got)
	}
}

func TestNext1010IngestBatchClaimFailure(t *testing.T) {
	st, svc := next1010IngestBatchFixture(t, 3, 3)
	st.failAt = 2
	svc.ProcessBatch(context.Background())
	if got := st.snapshot(); !reflect.DeepEqual(got, []string{"claim", "deliver", "finish", "claim"}) {
		t.Errorf("claim failure did not end the current poll: %v", got)
	}
	if states := next1010IngestStates(t, st, 3); states["done"] != 1 || states["pending"] != 2 {
		t.Errorf("claim failure touched unclaimed receipts: %v", states)
	}
}

func TestNext1010IngestBatchBlockedThenCancelled(t *testing.T) {
	st, svc := next1010IngestBatchFixture(t, 3, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, done := make(chan struct{}), make(chan struct{})
	st.deliver = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
	go func() { defer close(done); svc.ProcessBatch(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("production handler never reached its delivery transaction")
	}
	states := next1010IngestStates(t, st, 3)
	if trace := st.snapshot(); !reflect.DeepEqual(trace, []string{"claim", "deliver"}) || states["processing"] != 1 || states["pending"] != 2 {
		t.Errorf("slow current transaction prefetched later work: trace=%v states=%v", trace, states)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cooperative cancelled receipt did not join")
	}
	states = next1010IngestStates(t, st, 3)
	if trace := st.snapshot(); !reflect.DeepEqual(trace, []string{"claim", "deliver"}) || states["processing"] != 1 || states["pending"] != 2 {
		t.Errorf("cancellation dispatched or marked extra receipts: trace=%v states=%v", trace, states)
	}
}
