package rawobject

import (
	"context"
	"errors"
	"testing"
)

// fakeReaper simulates the store's atomic count-then-delete: it reports a fixed
// reference count and invokes del only when that count is zero.
type fakeReaper struct {
	refs     int
	lockErr  error
	delCalls int
}

func (f *fakeReaper) ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error) {
	if f.lockErr != nil {
		return false, f.lockErr
	}
	if f.refs > 0 {
		return false, nil
	}
	f.delCalls++
	if del != nil {
		if err := del(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

type fakeDeleter struct {
	called bool
	gotKey string
	err    error
}

func (f *fakeDeleter) Delete(_ context.Context, key string) error {
	f.called = true
	f.gotKey = key
	return f.err
}

func TestRelease(t *testing.T) {
	ctx := context.Background()

	t.Run("empty key is a noop", func(t *testing.T) {
		reaper := &fakeReaper{}
		del := &fakeDeleter{}
		out, err := Release(ctx, reaper, del, "   ")
		if out != Noop || err != nil {
			t.Fatalf("got (%v, %v), want (Noop, nil)", out, err)
		}
		if reaper.delCalls != 0 || del.called {
			t.Fatalf("empty key must not touch the reaper/deleter")
		}
	})

	t.Run("nil dependencies are a noop", func(t *testing.T) {
		out, err := Release(ctx, nil, nil, "k")
		if out != Noop || err != nil {
			t.Fatalf("got (%v, %v), want (Noop, nil)", out, err)
		}
	})

	t.Run("still referenced keeps the object", func(t *testing.T) {
		reaper := &fakeReaper{refs: 2}
		del := &fakeDeleter{}
		out, err := Release(ctx, reaper, del, "k")
		if out != StillReferenced || err != nil {
			t.Fatalf("got (%v, %v), want (StillReferenced, nil)", out, err)
		}
		if del.called {
			t.Fatalf("referenced object must not be deleted")
		}
	})

	t.Run("unreferenced object is deleted", func(t *testing.T) {
		reaper := &fakeReaper{refs: 0}
		del := &fakeDeleter{}
		out, err := Release(ctx, reaper, del, "k")
		if out != Deleted || err != nil {
			t.Fatalf("got (%v, %v), want (Deleted, nil)", out, err)
		}
		if !del.called || del.gotKey != "k" {
			t.Fatalf("expected delete of key %q, called=%v key=%q", "k", del.called, del.gotKey)
		}
	})

	t.Run("lock or count failure reports CountFailed without deleting", func(t *testing.T) {
		reaper := &fakeReaper{lockErr: errors.New("db down")}
		del := &fakeDeleter{}
		out, err := Release(ctx, reaper, del, "k")
		if out != CountFailed || err == nil {
			t.Fatalf("got (%v, %v), want (CountFailed, err)", out, err)
		}
		if del.called {
			t.Fatalf("must not delete when the count failed")
		}
	})

	t.Run("delete failure reports DeleteFailed", func(t *testing.T) {
		reaper := &fakeReaper{refs: 0}
		del := &fakeDeleter{err: errors.New("blob gone")}
		out, err := Release(ctx, reaper, del, "k")
		if out != DeleteFailed || err == nil {
			t.Fatalf("got (%v, %v), want (DeleteFailed, err)", out, err)
		}
	})
}
