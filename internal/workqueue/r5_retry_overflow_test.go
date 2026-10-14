package workqueue

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestR5BackoffDurationOverflowDoesNotWrap(t *testing.T) {
	t.Parallel()
	const maximum = time.Duration(math.MaxInt64)
	maxOf := func(j *Job[fakePayload]) int { return j.Payload.MaxAttempts }
	tests := []struct {
		name     string
		policy   RetryPolicy[fakePayload]
		attempts int
		want     time.Duration
	}{
		{"ingest multiply", ExponentialBackoff[fakePayload]{Base: maximum/2 + 1, CapExp: 8}, 2, maximum},
		{"ingest exponent beyond machine word", ExponentialBackoff[fakePayload]{Base: time.Second, CapExp: 100}, 100, maximum},
		{"ingest jitter", ExponentialBackoff[fakePayload]{Base: maximum - 1, CapExp: 8, Jitter: func() time.Duration { return 2 }}, 1, maximum},
		{"webhook multiply", LinearBackoff[fakePayload]{Base: maximum/2 + 1}, 2, maximum},
		{"webhook large attempts", LinearBackoff[fakePayload]{Base: time.Minute}, math.MaxInt, maximum},
		{"outbound multiply before cap", ExponentialCappedBackoff[fakePayload]{Base: maximum/2 + 1, Cap: time.Hour, MaxAttempts: maxOf}, 0, time.Hour},
		{"outbound exponent before cap", ExponentialCappedBackoff[fakePayload]{Base: time.Second, Cap: time.Hour, MaxAttempts: maxOf}, 63, time.Hour},
		{"outbound max attempts before cap", ExponentialCappedBackoff[fakePayload]{Base: time.Second, Cap: time.Hour, MaxAttempts: maxOf}, math.MaxInt, time.Hour},
		{"outbound no configured cap", ExponentialCappedBackoff[fakePayload]{Base: time.Second, MaxAttempts: maxOf}, 63, maximum},
		{"exact representable boundary", ExponentialBackoff[fakePayload]{Base: maximum / 2, CapExp: 8}, 2, maximum - 1},
		{"zero delay", LinearBackoff[fakePayload]{Base: 0}, math.MaxInt, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.NextAttempt(job(tt.attempts, math.MaxInt)); got != tt.want {
				t.Fatalf("attempts %d: got %v, want %v", tt.attempts, got, tt.want)
			}
		})
	}
}

func TestR5BackoffTerminalAttemptDoesNotOverflow(t *testing.T) {
	t.Parallel()
	p := ExponentialCappedBackoff[fakePayload]{MaxAttempts: func(j *Job[fakePayload]) int { return j.Payload.MaxAttempts }}
	for _, attempts := range []int{math.MaxInt - 1, math.MaxInt} {
		if !p.Dead(job(attempts, math.MaxInt)) {
			t.Errorf("attempts %d with max %d must remain terminal", attempts, math.MaxInt)
		}
	}
	if p.Dead(job(math.MaxInt-2, math.MaxInt)) {
		t.Fatal("last permitted nonterminal attempt was rejected")
	}
}

func TestR5BackoffWorkerPersistsFutureRetry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		policy RetryPolicy[fakePayload]
		job    *Job[fakePayload]
		delay  time.Duration
	}{
		{"capped outbound", ExponentialCappedBackoff[fakePayload]{Base: time.Second, Cap: time.Hour, MaxAttempts: func(j *Job[fakePayload]) int { return j.Payload.MaxAttempts }}, job(63, 100), time.Hour},
		{"saturated webhook", LinearBackoff[fakePayload]{Base: time.Duration(math.MaxInt64/2 + 1), Max: 3}, job(2, 3), time.Duration(math.MaxInt64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memStore{jobs: []*Job[fakePayload]{tt.job}}
			hooks := &captureHooks{}
			w := NewWorker[fakePayload](st, func(context.Context, *Job[fakePayload]) error { return errors.New("retryable failure") }, tt.policy, hooks,
				time.Minute, time.Second, 1, newLogger())
			before := time.Now().UTC()
			w.ProcessBatch(context.Background())
			after := time.Now().UTC()
			if len(st.retry) != 1 || len(st.done) != 0 || len(st.dead) != 0 || hooks.retry != 1 {
				t.Fatalf("unexpected lifecycle: retry=%d done=%d dead=%d retryHook=%d", len(st.retry), len(st.done), len(st.dead), hooks.retry)
			}
			retry := st.retry[0]
			if retry.nextAt.Before(before.Add(tt.delay)) || retry.nextAt.After(after.Add(tt.delay)) {
				t.Fatalf("retry persisted at %v, want delay %v from [%v, %v]", retry.nextAt, tt.delay, before, after)
			}
			if retry.id != tt.job.ID || retry.attempts != tt.job.Attempts || retry.lastErr != "retryable failure" {
				t.Fatalf("retry identity or failure changed: %#v", retry)
			}
		})
	}
}
