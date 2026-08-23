package workqueue

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func newLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

type fakePayload struct {
	MaxAttempts int
}

func job(attempts int, maxAttempts int) *Job[fakePayload] {
	return &Job[fakePayload]{ID: uuid.New(), Attempts: attempts, Payload: fakePayload{MaxAttempts: maxAttempts}}
}

// ---------- Backoff formula table tests (lock the legacy formulas) ----------

func TestExponentialBackoff_IngestFormula(t *testing.T) {
	t.Parallel()
	// ingest: retryBackoff(attempts) = 1<<min(max(attempts-1,0),8) seconds + jitter[0,1s).
	// jitter is randomized, so assert the deterministic base and that the
	// total falls in [base, base+1s).
	const jitterMax = time.Second
	p := ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 8, Jitter: func() time.Duration { return 0 }, Max: 5}

	cases := []struct {
		attempts int
		wantBase time.Duration
	}{
		{1, 1 << 0 * time.Second},                  // 2^0 = 1s
		{2, 1 << 1 * time.Second},                  // 2s
		{3, 1 << 2 * time.Second},                  // 4s
		{9, 1 << 8 * time.Second},                  // capped at exp=8 → 256s
		{20, 1 << 8 * time.Second},                 // beyond cap stays 256s
	}
	for _, c := range cases {
		got := p.NextAttempt(job(c.attempts, 0))
		if got != c.wantBase {
			t.Errorf("attempts=%d: want base %v, got %v", c.attempts, c.wantBase, got)
		}
	}

	// Jitter range [0, 1s).
	withJitter := ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 8, Jitter: func() time.Duration { return jitterMax - time.Millisecond }, Max: 5}
	got := withJitter.NextAttempt(job(1, 0))
	if got != time.Second+(jitterMax-time.Millisecond) {
		t.Errorf("jitter: want %v, got %v", time.Second+(jitterMax-time.Millisecond), got)
	}
}

func TestExponentialBackoff_DeadBoundary(t *testing.T) {
	t.Parallel()
	// ingest: dead := job.Attempts >= maxRetries (post-claim attempts).
	p := ExponentialBackoff[fakePayload]{Max: 5}
	if p.Dead(job(4, 0)) {
		t.Error("attempts=4, max=5: must not be dead")
	}
	if !p.Dead(job(5, 0)) {
		t.Error("attempts=5, max=5: must be dead")
	}
	if !p.Dead(job(6, 0)) {
		t.Error("attempts=6, max=5: must be dead")
	}
}

func TestLinearBackoff_WebhookDeliveryFormula(t *testing.T) {
	t.Parallel()
	// webhook delivery: retryDelay * attempts, no jitter.
	p := LinearBackoff[fakePayload]{Base: 2 * time.Second, Max: 3}
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 6 * time.Second},
		{7, 14 * time.Second},
	}
	for _, c := range cases {
		if got := p.NextAttempt(job(c.attempts, 0)); got != c.want {
			t.Errorf("attempts=%d: want %v, got %v", c.attempts, c.want, got)
		}
	}
	if p.Dead(job(2, 0)) {
		t.Error("attempts=2, max=3: must not be dead")
	}
	if !p.Dead(job(3, 0)) {
		t.Error("attempts=3, max=3: must be dead")
	}
}

func TestFixedBackoff_WebhookOutboxFormula(t *testing.T) {
	t.Parallel()
	// webhook outbox: fixed retryDelay, no attempts multiplier, never dead.
	p := FixedBackoff[fakePayload]{Base: 1500 * time.Millisecond}
	for _, attempts := range []int{1, 2, 5, 100} {
		if got := p.NextAttempt(job(attempts, 0)); got != 1500*time.Millisecond {
			t.Errorf("attempts=%d: want 1.5s, got %v", attempts, got)
		}
		if p.Dead(job(attempts, 0)) {
			t.Errorf("attempts=%d: outbox must never be dead", attempts)
		}
	}
}

func TestExponentialCappedBackoff_OutboundFormula(t *testing.T) {
	t.Parallel()
	// outbound: delay = RetryDelay * 2^attempt (attempt = job.Attempts+1), cap 1h.
	// dead := attempt >= MaxAttempts, i.e. job.Attempts+1 >= MaxAttempts.
	maxOf := func(j *Job[fakePayload]) int { return j.Payload.MaxAttempts }
	p := ExponentialCappedBackoff[fakePayload]{Base: time.Second, Cap: time.Hour, MaxAttempts: maxOf}

	cases := []struct {
		attempts    int
		maxAttempts int
		wantDelay   time.Duration
		wantDead    bool
	}{
		{attempts: 1, maxAttempts: 5, wantDelay: 1 << 2 * time.Second, wantDead: false}, // 2^2=4s
		{attempts: 2, maxAttempts: 5, wantDelay: 1 << 3 * time.Second, wantDead: false}, // 8s
		{attempts: 3, maxAttempts: 5, wantDelay: 1 << 4 * time.Second, wantDead: false}, // 16s
		{attempts: 4, maxAttempts: 5, wantDelay: 1 << 5 * time.Second, wantDead: true},  // attempt=5 >= 5 → dead
		{attempts: 5, maxAttempts: 5, wantDelay: 1 << 6 * time.Second, wantDead: true},  // attempt=6 >= 5 → dead
	}
	for _, c := range cases {
		j := job(c.attempts, c.maxAttempts)
		if got := p.NextAttempt(j); got != c.wantDelay {
			t.Errorf("attempts=%d: want delay %v, got %v", c.attempts, c.wantDelay, got)
		}
		if got := p.Dead(j); got != c.wantDead {
			t.Errorf("attempts=%d max=%d: want dead=%v, got %v", c.attempts, c.maxAttempts, c.wantDead, got)
		}
	}

	// Cap at 1h.
	p2 := ExponentialCappedBackoff[fakePayload]{Base: time.Hour, Cap: time.Hour, MaxAttempts: maxOf}
	if got := p2.NextAttempt(job(0, 100)); got != time.Hour {
		t.Errorf("cap: want 1h, got %v", got)
	}
	// Above cap clamps.
	p3 := ExponentialCappedBackoff[fakePayload]{Base: 2 * time.Hour, Cap: time.Hour, MaxAttempts: maxOf}
	if got := p3.NextAttempt(job(0, 100)); got != time.Hour {
		t.Errorf("above cap: want 1h, got %v", got)
	}
}

// ---------- Store/Worker dispatch ----------

type memStore struct {
	mu           sync.Mutex
	jobs         []*Job[fakePayload]
	done         []uuid.UUID
	retry        []retryRecord
	dead         []deadRecord
	markDoneErr  error
	markRetryErr error
	markDeadErr  error
}

type retryRecord struct {
	id       uuid.UUID
	lastErr  string
	nextAt   time.Time
	attempts int
}

type deadRecord struct {
	id      uuid.UUID
	lastErr string
}

func (m *memStore) Claim(_ context.Context, _ time.Time, limit int) ([]*Job[fakePayload], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > len(m.jobs) {
		limit = len(m.jobs)
	}
	out := m.jobs[:limit]
	m.jobs = m.jobs[limit:]
	return out, nil
}
func (m *memStore) MarkDone(_ context.Context, j *Job[fakePayload]) error {
	if m.markDoneErr != nil {
		return m.markDoneErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = append(m.done, j.ID)
	return nil
}
func (m *memStore) MarkRetry(_ context.Context, j *Job[fakePayload], lastErr string, nextAt time.Time) error {
	if m.markRetryErr != nil {
		return m.markRetryErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retry = append(m.retry, retryRecord{id: j.ID, lastErr: lastErr, nextAt: nextAt, attempts: j.Attempts})
	return nil
}
func (m *memStore) MarkDead(_ context.Context, j *Job[fakePayload], lastErr string) error {
	if m.markDeadErr != nil {
		return m.markDeadErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dead = append(m.dead, deadRecord{id: j.ID, lastErr: lastErr})
	return nil
}

type captureHooks struct {
	done  int32
	retry int32
	dead  int32
	last  atomic.Pointer[fakePayload]
}

func (h *captureHooks) OnDone(_ context.Context, j *Job[fakePayload]) {
	atomic.AddInt32(&h.done, 1)
	p := j.Payload
	h.last.Store(&p)
}
func (h *captureHooks) OnRetry(_ context.Context, j *Job[fakePayload], _ error) { atomic.AddInt32(&h.retry, 1) }
func (h *captureHooks) OnDead(_ context.Context, j *Job[fakePayload], _ error)  { atomic.AddInt32(&h.dead, 1) }

func TestProcessOne_DonePath(t *testing.T) {
	t.Parallel()
	st := &memStore{jobs: []*Job[fakePayload]{job(1, 5)}}
	hooks := &captureHooks{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return nil },
		ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 8, Max: 5},
		hooks, 5*time.Minute, time.Second, 10, newLogger())
	w.processBatch(context.Background())

	if len(st.done) != 1 {
		t.Fatalf("want 1 done, got %d", len(st.done))
	}
	if atomic.LoadInt32(&hooks.done) != 1 {
		t.Errorf("want OnDone called once, got %d", hooks.done)
	}
	if len(st.retry) != 0 || len(st.dead) != 0 {
		t.Errorf("retry=%d dead=%d, want both 0", len(st.retry), len(st.dead))
	}
}

func TestProcessOne_RetryPath_UsesNextAttempt(t *testing.T) {
	t.Parallel()
	j := job(2, 5) // attempts=2, not dead
	st := &memStore{jobs: []*Job[fakePayload]{j}}
	var gotNext time.Time
	policy := LinearBackoff[fakePayload]{Base: 2 * time.Second, Max: 5}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return errors.New("boom") },
		policy, nil, 5*time.Minute, time.Second, 10, newLogger())

	// Intercept MarkRetry to capture nextAt relative to now.
	before := time.Now().UTC()
	w.processBatch(context.Background())

	if len(st.retry) != 1 {
		t.Fatalf("want 1 retry, got %d", len(st.retry))
	}
	gotNext = st.retry[0].nextAt
	// LinearBackoff at attempts=2 → 4s. So nextAt ≈ before+4s.
	wantEarliest := before.Add(4*time.Second - 50*time.Millisecond)
	wantLatest := before.Add(4*time.Second + 50*time.Millisecond)
	if gotNext.Before(wantEarliest) || gotNext.After(wantLatest) {
		t.Errorf("nextAt=%v, want ~%v+4s (window %v..%v)", gotNext, before, wantEarliest, wantLatest)
	}
	if len(st.dead) != 0 {
		t.Errorf("want 0 dead, got %d", len(st.dead))
	}
}

func TestProcessOne_DeadPath(t *testing.T) {
	t.Parallel()
	j := job(5, 5) // attempts=5 >= max=5 → dead
	st := &memStore{jobs: []*Job[fakePayload]{j}}
	hooks := &captureHooks{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return errors.New("fatal") },
		ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 8, Max: 5},
		hooks, 5*time.Minute, time.Second, 10, newLogger())
	w.processBatch(context.Background())

	if len(st.dead) != 1 {
		t.Fatalf("want 1 dead, got %d", len(st.dead))
	}
	if st.dead[0].lastErr != "fatal" {
		t.Errorf("lastErr=%q, want fatal", st.dead[0].lastErr)
	}
	if atomic.LoadInt32(&hooks.dead) != 1 {
		t.Errorf("want OnDead once, got %d", hooks.dead)
	}
	if len(st.retry) != 0 {
		t.Errorf("want 0 retry, got %d", len(st.retry))
	}
}

// ---------- Lease-lost is skipped, not fatal ----------

func TestProcessOne_LeaseLostOnMarkRetryIsSkipped(t *testing.T) {
	t.Parallel()
	st := &memStore{
		jobs:         []*Job[fakePayload]{job(1, 5)},
		markRetryErr: ErrLeaseLost,
	}
	hooks := &captureHooks{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return errors.New("x") },
		LinearBackoff[fakePayload]{Base: time.Second, Max: 5},
		hooks, 5*time.Minute, time.Second, 10, newLogger())
	// Must not panic and must not call OnRetry (mark failed).
	w.processBatch(context.Background())
	if atomic.LoadInt32(&hooks.retry) != 0 {
		t.Errorf("OnRetry must not fire when mark fails with lease lost")
	}
}

func TestProcessOne_LeaseLostOnMarkDoneIsSkipped(t *testing.T) {
	t.Parallel()
	st := &memStore{
		jobs:        []*Job[fakePayload]{job(1, 5)},
		markDoneErr: ErrLeaseLost,
	}
	hooks := &captureHooks{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return nil },
		LinearBackoff[fakePayload]{Base: time.Second, Max: 5},
		hooks, 5*time.Minute, time.Second, 10, newLogger())
	w.processBatch(context.Background())
	if atomic.LoadInt32(&hooks.done) != 0 {
		t.Errorf("OnDone must not fire when mark fails with lease lost")
	}
}

// ---------- ctx cancellation exits Run ----------

func TestRun_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()
	st := &memStore{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return nil },
		LinearBackoff[fakePayload]{Base: time.Second, Max: 5},
		nil, 5*time.Minute, 10*time.Millisecond, 10, newLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

// ---------- Start/Stop drains goroutine ----------

func TestStartStop_GoroutineExits(t *testing.T) {
	t.Parallel()
	st := &memStore{}
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return nil },
		LinearBackoff[fakePayload]{Base: time.Second, Max: 5},
		nil, 5*time.Minute, 10*time.Millisecond, 10, newLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	// Stop must return once the goroutine observes stopCh.
	stopDone := make(chan struct{})
	go func() { w.Stop(); close(stopDone) }()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
}

// ---------- Default hooks are no-op ----------

func TestNilHooksAreNoop(t *testing.T) {
	t.Parallel()
	st := &memStore{jobs: []*Job[fakePayload]{job(1, 5)}}
	// Pass nil hooks; must not panic.
	w := NewWorker[fakePayload](st, func(_ context.Context, _ *Job[fakePayload]) error { return nil },
		ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 8, Max: 5},
		nil, 5*time.Minute, time.Second, 10, newLogger())
	w.processBatch(context.Background())
	if len(st.done) != 1 {
		t.Fatalf("want 1 done, got %d", len(st.done))
	}
}
