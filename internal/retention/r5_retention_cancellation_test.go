package retention

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/rawobject"
)

type retentionContextKey struct{}

type retentionCancelStore struct {
	rawobject.ReferenceStore
	rawobject.BlobStore
	calls         []string
	cancelAt      string
	cancel        context.CancelFunc
	pending       map[string]bool
	expiredKeys   []string
	purgedKeys    []string
	enqueueErr    error
	waitEnqueue   bool
	badRecovery   []string
	originalCtx   context.Context
	releaseErrKey string
	purgeErr      error
}

func (s *retentionCancelStore) record(ctx context.Context, call string) {
	s.calls = append(s.calls, call)
	if call == s.cancelAt {
		s.cancel()
	}
}

func (s *retentionCancelStore) SweepCompanyMetadata(ctx context.Context) error {
	s.record(ctx, "metadata")
	return nil
}
func (s *retentionCancelStore) ListPendingOrphanRetries(ctx context.Context, _ int) ([]string, error) {
	s.record(ctx, "list")
	var out []string
	for _, key := range []string{"retry-a", "retry-b"} {
		if s.pending[key] {
			out = append(out, key)
		}
	}
	return out, nil
}
func (s *retentionCancelStore) ReapExhaustedOrphanRetries(ctx context.Context) (int, error) {
	s.record(ctx, "reap")
	return 0, nil
}
func (s *retentionCancelStore) DeleteExpiredMessagesReturningKeys(ctx context.Context, _ time.Time, _ int) (int, []string, error) {
	s.record(ctx, "expired")
	keys := s.expiredKeys
	s.expiredKeys = nil
	return len(keys), keys, nil
}
func (s *retentionCancelStore) PurgeOldIngestJobs(ctx context.Context, _ time.Time, _ int) (int, []string, error) {
	s.record(ctx, "purge")
	keys := s.purgedKeys
	s.purgedKeys = nil
	return len(keys), keys, s.purgeErr
}
func (s *retentionCancelStore) ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error) {
	s.record(ctx, "release:"+key)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if key == s.releaseErrKey {
		return false, errors.New("synthetic reference count failed")
	}
	if err := del(ctx); err != nil {
		return false, err
	}
	return true, nil
}
func (s *retentionCancelStore) Delete(ctx context.Context, key string) error {
	s.record(ctx, "delete:"+key)
	return ctx.Err()
}
func (s *retentionCancelStore) EnqueueOrphanRetry(ctx context.Context, key string) error {
	deadline, bounded := ctx.Deadline()
	if ctx.Err() != nil || ctx != s.originalCtx && (!bounded || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || ctx.Value(retentionContextKey{}) != "retention-context-value") {
		s.badRecovery = append(s.badRecovery, key)
	}
	s.record(ctx, "enqueue:"+key)
	if s.waitEnqueue {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.enqueueErr != nil {
		return s.enqueueErr
	}
	s.pending[key] = true
	return nil
}
func (s *retentionCancelStore) ClearOrphanRetry(ctx context.Context, key string) error {
	s.record(ctx, "clear:"+key)
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(s.pending, key)
	return nil
}

func retentionCancellationFixture(cancelAt string) (*Scanner, *retentionCancelStore, context.Context, *bytes.Buffer) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), retentionContextKey{}, "retention-context-value"))
	st := &retentionCancelStore{cancelAt: cancelAt, cancel: cancel, pending: make(map[string]bool), originalCtx: ctx}
	logs := &bytes.Buffer{}
	sc := New(rawobject.NewStore(st, st), st, config.Storage{RetentionBatchSize: 3, RetentionScanInterval: time.Second}, zerolog.New(logs))
	return sc, st, ctx, logs
}

func TestR5RetentionCancellationStopsNewWork(t *testing.T) {
	for _, tc := range []struct {
		name, cancelAt  string
		pending         bool
		expired, purged []string
		want            []string
	}{
		{"pre-cancelled", "before", false, nil, nil, nil},
		{"metadata phase", "metadata", false, nil, nil, []string{"metadata"}},
		{"retry list", "list", true, nil, nil, []string{"metadata", "list"}},
		{"retry release", "release:retry-a", true, nil, nil, []string{"metadata", "list", "release:retry-a"}},
		{"retry delete", "delete:retry-a", true, nil, nil, []string{"metadata", "list", "release:retry-a", "delete:retry-a"}},
		{"reap phase", "reap", false, nil, nil, []string{"metadata", "list", "reap"}},
		{"empty expired phase", "expired", false, nil, nil, []string{"metadata", "list", "reap", "expired"}},
		{"committed message keys", "expired", false, []string{"new-a", "new-b"}, nil, []string{"metadata", "list", "reap", "expired", "enqueue:new-a", "enqueue:new-b"}},
		{"message release", "release:new-a", false, []string{"new-a", "new-b"}, nil, []string{"metadata", "list", "reap", "expired", "release:new-a", "enqueue:new-a", "enqueue:new-b"}},
		{"message delete", "delete:new-a", false, []string{"new-a", "new-b"}, nil, []string{"metadata", "list", "reap", "expired", "release:new-a", "delete:new-a", "enqueue:new-a", "enqueue:new-b"}},
		{"committed ingest keys", "purge", false, nil, []string{"new-a", "new-b"}, []string{"metadata", "list", "reap", "expired", "purge", "enqueue:new-a", "enqueue:new-b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, st, ctx, _ := retentionCancellationFixture(tc.cancelAt)
			defer st.cancel()
			st.expiredKeys, st.purgedKeys = tc.expired, tc.purged
			if tc.pending {
				st.pending["retry-a"], st.pending["retry-b"] = true, true
			}
			if tc.cancelAt == "before" {
				st.cancel()
			}
			sc.sweep(ctx)
			if !reflect.DeepEqual(st.calls, tc.want) {
				t.Errorf("cancelled sweep admitted unexpected work:\n got %v\nwant %v", st.calls, tc.want)
			}
			if tc.pending && (!st.pending["retry-a"] || !st.pending["retry-b"]) {
				t.Error("cancelled retry removed durable pending work")
			}
			if len(tc.expired)+len(tc.purged) > 0 {
				if !st.pending["new-a"] || !st.pending["new-b"] {
					t.Error("committed keys were not handed back for recovery")
				}
				if len(st.badRecovery) > 0 {
					t.Errorf("recovery used cancelled, unbounded or detached-value context: %v", st.badRecovery)
				}
			}
		})
	}
}

func TestR5RetentionCancellationReportsRecoveryFailure(t *testing.T) {
	sc, st, ctx, logs := retentionCancellationFixture("expired")
	defer st.cancel()
	st.expiredKeys = []string{"new-a", "new-b"}
	st.enqueueErr = errors.New("synthetic orphan handoff write failed")
	sc.sweep(ctx)
	if !strings.Contains(logs.String(), st.enqueueErr.Error()) {
		t.Fatalf("handoff failure disappeared from diagnostics: %s", logs.String())
	}
	if len(st.pending) != 0 {
		t.Error("failed persistence was presented as tracked")
	}
	if len(st.badRecovery) != 0 {
		t.Errorf("wrong cleanup context: %v", st.badRecovery)
	}
	if want := []string{"metadata", "list", "reap", "expired", "enqueue:new-a", "enqueue:new-b"}; !reflect.DeepEqual(st.calls, want) {
		t.Errorf("failure changed phase boundaries: got %v want %v", st.calls, want)
	}
}

func TestR5RetentionCancellationUsesOneBoundedRecoveryBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc, st, ctx, logs := retentionCancellationFixture("expired")
		defer st.cancel()
		st.expiredKeys = []string{"new-a", "new-b"}
		st.waitEnqueue = true
		start := time.Now()
		sc.sweep(ctx)
		elapsed := time.Since(start)
		if elapsed != 5*time.Second {
			t.Errorf("cleanup must have one shared five-second budget: %v", elapsed)
		}
		if len(st.badRecovery) != 0 {
			t.Errorf("wrong cleanup context: %v", st.badRecovery)
		}
		if want := []string{"metadata", "list", "reap", "expired", "enqueue:new-a"}; !reflect.DeepEqual(st.calls, want) {
			t.Errorf("cleanup restarted after budget ended: got %v want %v", st.calls, want)
		}
		if !strings.Contains(logs.String(), context.DeadlineExceeded.Error()) {
			t.Fatalf("exhausted handoff budget not reported: %s", logs.String())
		}
		if !strings.Contains(logs.String(), `"remaining":1`) {
			t.Fatalf("remaining handoff work not reported after budget: %s", logs.String())
		}
	})
}

func TestR5RetentionCancellationDuringOrdinaryRetryRetainsCurrentKey(t *testing.T) {
	sc, st, ctx, _ := retentionCancellationFixture("enqueue:new-b")
	defer st.cancel()
	// This is a full batch. Completing its first key must not make the next
	// key disappear when cancellation occurs inside the normal retry write.
	st.expiredKeys = []string{"new-a", "new-b", "new-c"}
	st.releaseErrKey = "new-b"
	sc.sweep(ctx)
	want := []string{"metadata", "list", "reap", "expired", "release:new-a", "delete:new-a", "clear:new-a", "release:new-b", "enqueue:new-b", "enqueue:new-b", "enqueue:new-c"}
	if !reflect.DeepEqual(st.calls, want) {
		t.Errorf("full batch did not stop at current untracked key: got %v want %v", st.calls, want)
	}
	if st.pending["new-a"] || !st.pending["new-b"] || !st.pending["new-c"] {
		t.Errorf("completed prefix or pending suffix lost: %v", st.pending)
	}
	if len(st.badRecovery) != 0 {
		t.Errorf("wrong cleanup context: %v", st.badRecovery)
	}
}

func TestR5RetentionCancellationRetainsKnownPurgeKeysWithError(t *testing.T) {
	sc, st, ctx, logs := retentionCancellationFixture("purge")
	defer st.cancel()
	st.purgedKeys = []string{"new-a", "new-b"}
	st.purgeErr = errors.New("synthetic purge rows interrupted")
	sc.sweep(ctx)
	want := []string{"metadata", "list", "reap", "expired", "purge", "enqueue:new-a", "enqueue:new-b"}
	if !reflect.DeepEqual(st.calls, want) {
		t.Errorf("known candidates were discarded or deleted: got %v want %v", st.calls, want)
	}
	if !st.pending["new-a"] || !st.pending["new-b"] || len(st.badRecovery) != 0 {
		t.Errorf("known candidates were not handed off safely: pending=%v contexts=%v", st.pending, st.badRecovery)
	}
	if !strings.Contains(logs.String(), st.purgeErr.Error()) || strings.Contains(logs.String(), "old ingest jobs cleaned up") {
		t.Fatalf("error count was reported as success or original error lost: %s", logs.String())
	}
}
