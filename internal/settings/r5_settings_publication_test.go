package settings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

// The store owns commit order independently of the order in which a caller
// receives its successful response, as a real database connection can do.
type publicationStore struct {
	mu            sync.Mutex
	values        map[string]string
	lists         int
	listErr       error
	writeErr      error
	afterCommit   func(string, string)
	afterSnapshot func()
}

func (s *publicationStore) GetSetting(_ context.Context, key string) (*models.SystemSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return &models.SystemSetting{Key: key, Value: value}, nil
}

func (s *publicationStore) UpsertSetting(_ context.Context, key, value, _ string) error {
	s.mu.Lock()
	err := s.writeErr
	if err == nil {
		s.values[key] = value
	}
	after := s.afterCommit
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if after != nil {
		after(key, value)
	}
	return nil
}

func (s *publicationStore) SeedSetting(ctx context.Context, key, value, _ string) (bool, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return false, err
	}
	if _, exists := s.values[key]; exists {
		s.mu.Unlock()
		return false, nil
	}
	if s.writeErr != nil {
		err := s.writeErr
		s.mu.Unlock()
		return false, err
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	after := s.afterCommit
	s.mu.Unlock()
	if after != nil {
		after(key, value)
	}
	return true, nil
}

func (s *publicationStore) ListSettings(context.Context) ([]*models.SystemSetting, error) {
	s.mu.Lock()
	s.lists++
	err, after := s.listErr, s.afterSnapshot
	rows := make([]*models.SystemSetting, 0, len(s.values))
	for key, value := range s.values {
		rows = append(rows, &models.SystemSetting{Key: key, Value: value})
	}
	s.mu.Unlock()
	if after != nil {
		after()
	}
	return rows, err
}

func publicationFixture(key, value string) (*Manager, *publicationStore) {
	st := &publicationStore{values: map[string]string{key: value}}
	return NewManager(st, zerolog.Nop()), st
}

func TestR5SettingsDelayedWriteResponseCannotRevertNewerCommit(t *testing.T) {
	for _, tc := range []struct {
		name, key, before, first, last string
		kind                           string
	}{
		{"registration_disabled", models.SettingOpenRegistration, "true", "true", "false", "bool"},
		{"registration_enabled", models.SettingOpenRegistration, "false", "false", "true", "bool"},
		{"integer_quota", models.SettingPublicIPRPM, "10", "20", "30", "int"},
		{"integer_zero", models.SettingPublicIPRPM, "10", "20", "0", "int"},
		{"integer_negative_preserved", models.SettingPublicIPRPM, "10", "20", "-1", "int"},
		{"string", models.SettingMailboxNaming, "full", "local", "domain", "string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager, st := publicationFixture(tc.key, tc.before)
				ctx := t.Context()
				if got := manager.Get(ctx, tc.key, "missing"); got != tc.before {
					t.Fatalf("initial warm cache = %q, want %q", got, tc.before)
				}
				committed, returnFirst := make(chan struct{}), make(chan struct{})
				st.afterCommit = func(key, value string) {
					if key == tc.key && value == tc.first {
						close(committed)
						<-returnFirst
					}
				}
				firstDone := make(chan error, 1)
				go func() { firstDone <- manager.Set(ctx, tc.key, tc.first, "") }()
				<-committed
				// A is committed but its database response is still withheld.
				if err := manager.Set(ctx, tc.key, tc.last, ""); err != nil {
					t.Fatal(err)
				}
				if got := manager.Get(ctx, tc.key, "missing"); got != tc.last {
					t.Errorf("newer acknowledged value = %q, want %q", got, tc.last)
				}
				close(returnFirst)
				if err := <-firstDone; err != nil {
					t.Fatal(err)
				}
				persisted, _ := st.GetSetting(ctx, tc.key)
				if got := manager.Get(ctx, tc.key, "missing"); got != persisted.Value || got != tc.last {
					t.Errorf("late response reverted cache: cache=%q persisted=%q want=%q", got, persisted.Value, tc.last)
				}
				switch tc.kind {
				case "bool":
					if got := manager.GetBool(ctx, tc.key, tc.last != "true"); got != (tc.last == "true") {
						t.Errorf("GetBool returned old policy: %v", got)
					}
				case "int":
					want := map[string]int{"30": 30, "0": 0, "-1": -1}[tc.last]
					if got := manager.GetInt(ctx, tc.key, 999); got != want {
						t.Errorf("GetInt returned %d, want %d", got, want)
					}
				}
			})
		})
	}
}

func TestR5SettingsReadFailureKeepsLastObservedSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager, st := publicationFixture("key", "initial")
		ctx := t.Context()
		_ = manager.Get(ctx, "key", "missing")
		committed, release := make(chan struct{}), make(chan struct{})
		st.afterCommit = func(_, value string) {
			if value == "earlier" {
				close(committed)
				<-release
			}
		}
		done := make(chan error, 1)
		go func() { done <- manager.Set(ctx, "key", "earlier", "") }()
		<-committed
		if err := manager.Set(ctx, "key", "later", ""); err != nil {
			t.Fatal(err)
		}
		if got := manager.Get(ctx, "key", "missing"); got != "later" {
			t.Fatalf("observed snapshot = %q", got)
		}
		st.mu.Lock()
		st.listErr = errors.New("temporary read failure")
		st.mu.Unlock()
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := manager.Get(ctx, "key", "fallback"); got != "later" {
			t.Errorf("failed refresh replaced last observed snapshot: %q", got)
		}
		st.mu.Lock()
		reads := st.lists
		st.listErr = nil
		st.values["key"] = "recovered"
		st.mu.Unlock()
		if got := manager.Get(ctx, "key", "fallback"); got != "later" {
			t.Errorf("one-second stale policy changed: %q", got)
		}
		st.mu.Lock()
		if st.lists != reads {
			t.Errorf("failed refresh backoff made another read: %d -> %d", reads, st.lists)
		}
		st.mu.Unlock()
		time.Sleep(time.Second)
		if got := manager.Get(ctx, "key", "fallback"); got != "recovered" {
			t.Errorf("one-second retry did not reload: %q", got)
		}
	})
}

func TestR5SettingsFailedWritePreservesCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager, st := publicationFixture("key", "original")
		ctx := t.Context()
		_ = manager.Get(ctx, "key", "missing")
		cause := errors.New("write failed")
		st.writeErr = cause
		if err := manager.Set(ctx, "key", "rejected", ""); !errors.Is(err, cause) {
			t.Fatalf("Set error = %v, want original cause", err)
		}
		if got := manager.Get(ctx, "key", "missing"); got != "original" {
			t.Errorf("failed write changed cache: %q", got)
		}
		if st.lists != 1 || st.values["key"] != "original" {
			t.Errorf("failed write effects: lists=%d persisted=%q", st.lists, st.values["key"])
		}
	})
}

func TestR5SettingsRefreshAndInvalidationKeepCommittedValues(t *testing.T) {
	for _, operation := range []string{"set", "invalidate"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager, st := publicationFixture("key", "before")
				ctx := t.Context()
				_ = manager.Get(ctx, "key", "missing")
				time.Sleep(manager.cacheTTL)
				captured, release := make(chan struct{}), make(chan struct{})
				st.afterSnapshot = func() { close(captured); <-release }
				readDone := make(chan string, 1)
				go func() { readDone <- manager.Get(ctx, "key", "missing") }()
				<-captured
				written, writeDone := make(chan struct{}), make(chan error, 1)
				st.afterCommit = func(_, _ string) { close(written) }
				if operation == "set" {
					go func() { writeDone <- manager.Set(ctx, "key", "after", "") }()
					<-written
				} else {
					st.mu.Lock()
					st.values["key"] = "after"
					st.mu.Unlock()
					go func() { manager.Invalidate(); writeDone <- nil }()
				}
				st.mu.Lock()
				st.afterSnapshot = nil
				st.mu.Unlock()
				close(release)
				<-readDone // A read already in progress may return its earlier snapshot.
				if err := <-writeDone; err != nil {
					t.Fatal(err)
				}
				if got := manager.Get(ctx, "key", "missing"); got != "after" {
					t.Errorf("completed %s left the old refresh current: %q", operation, got)
				}
			})
		})
	}
}

func TestR5SettingsWarmCacheAndDistinctWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager, st := publicationFixture("first", "one")
		ctx := t.Context()
		if got := manager.Get(ctx, "first", "missing"); got != "one" {
			t.Fatal(got)
		}
		if got := manager.Get(ctx, "absent", "default"); got != "default" || st.lists != 1 {
			t.Fatalf("warm cache/default changed: value=%q reads=%d", got, st.lists)
		}
		if err := manager.Set(ctx, "first", "updated", ""); err != nil {
			t.Fatal(err)
		}
		if err := manager.Set(ctx, "second", "two", ""); err != nil {
			t.Fatal(err)
		}
		if got := manager.Get(ctx, "first", "missing"); got != "updated" {
			t.Error(got)
		}
		if got := manager.Get(ctx, "second", "missing"); got != "two" {
			t.Error(got)
		}
		st.mu.Lock()
		st.values["first"] = "external"
		st.mu.Unlock()
		time.Sleep(manager.cacheTTL)
		if got := manager.Get(ctx, "first", "missing"); got != "external" {
			t.Errorf("normal TTL did not reload: %q", got)
		}
	})
}
