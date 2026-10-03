package testpg

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Pure lifecycle tests: these are not real PostgreSQL or migration evidence.
func TestPostgresFixtureCleanupPartialInitialization(t *testing.T) {
	for _, tc := range []struct {
		name             string
		admin, db, store bool
		observer         bool
		want             []string
	}{
		{name: "nothing_acquired"},
		{name: "admin_only_create_failed", admin: true, want: []string{"admin"}},
		{name: "owned_database_store_failed", admin: true, db: true, want: []string{"drop", "admin"}},
		{name: "store_acquired_observer_failed", admin: true, db: true, store: true, want: []string{"store", "drop", "admin"}},
		{name: "fully_initialized", admin: true, db: true, store: true, observer: true, want: []string{"observer", "store", "drop", "admin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			r := &postgresFixtureResources{}
			if tc.admin {
				r.closeAdmin = func() { calls = append(calls, "admin") }
			}
			if tc.db {
				r.dropDatabase = func(context.Context) error { calls = append(calls, "drop"); return nil }
			}
			if tc.store {
				r.closeStore = func() { calls = append(calls, "store") }
			}
			if tc.observer {
				r.closeObserver = func() { calls = append(calls, "observer") }
			}
			for i := 0; i < 2; i++ {
				if err := r.close(); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("cleanup order or duplicate close: got %v, want %v", calls, tc.want)
			}
		})
	}
}

func TestPostgresFixtureCleanupDropFailureStillClosesAdmin(t *testing.T) {
	wantErr := errors.New("controlled cleanup failure")
	var calls []string
	var dropCtx context.Context
	var beforeDrop time.Time
	r := &postgresFixtureResources{
		closeObserver: func() { calls = append(calls, "observer") },
		closeStore: func() {
			calls = append(calls, "store")
			beforeDrop = time.Now()
		},
		dropDatabase: func(ctx context.Context) error {
			calls = append(calls, "drop")
			dropCtx = ctx
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Before(beforeDrop.Add(postgresFixtureDropTimeout)) || deadline.After(time.Now().Add(postgresFixtureDropTimeout)) {
				t.Error("DROP did not receive its independent 10-second cleanup budget after store close")
			}
			if ctx.Err() != nil {
				t.Error("DROP received an already cancelled context")
			}
			return wantErr
		},
		closeAdmin: func() { calls = append(calls, "admin") },
	}
	for i := 0; i < 2; i++ {
		if err := r.close(); !errors.Is(err, wantErr) {
			t.Fatalf("DROP failure lost: %v", err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"observer", "store", "drop", "admin"}) {
		t.Fatalf("cleanup failure changed order or repeated cleanup: %v", calls)
	}
	if dropCtx == nil || !errors.Is(dropCtx.Err(), context.Canceled) {
		t.Fatal("cleanup context was not cancelled after DROP returned")
	}
}
