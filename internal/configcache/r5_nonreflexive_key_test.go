package configcache

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestProgressConfigCacheNonreflexiveKeys(t *testing.T) {
	t.Run("float", func(t *testing.T) { progressNonreflexiveKey(t, math.NaN()) })
	t.Run("struct", func(t *testing.T) { progressNonreflexiveKey(t, struct{ N float64 }{math.NaN()}) })
	t.Run("array", func(t *testing.T) { progressNonreflexiveKey(t, [1]float64{math.NaN()}) })
	t.Run("interface", func(t *testing.T) { progressNonreflexiveKey[any](t, math.NaN()) })
}

func progressNonreflexiveKey[K comparable](t *testing.T, key K) {
	t.Helper()
	cause := errors.New("nonreflexive-key loader failed")
	calls := 0
	cache := New(time.Hour, func(_ context.Context, _ K) (int, error) {
		calls++
		if calls == 3 {
			return 99, cause
		}
		return calls, nil
	})
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 2; i++ {
			value, err := cache.Get(context.Background(), key)
			if err != nil || value != i {
				done <- errors.New("nonreflexive key changed uncached loader result")
				return
			}
		}
		if value, err := cache.Get(context.Background(), key); value != 0 || !errors.Is(err, cause) {
			done <- errors.New("nonreflexive key changed loader error semantics")
			return
		}
		if len(cache.entries) != 0 || len(cache.inflight) != 0 {
			done <- errors.New("nonreflexive keys retained unreachable cache records")
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get cannot finish for a comparable NaN key")
	}
}

func TestProgressConfigCacheNilInterfaceKey(t *testing.T) {
	calls := 0
	cache := New(time.Hour, func(_ context.Context, _ any) (int, error) {
		calls++
		return calls, nil
	})
	for i := 0; i < 2; i++ {
		if value, err := cache.Get(context.Background(), nil); err != nil || value != 1 {
			t.Fatalf("nil interface cache hit failed: value=%d err=%v", value, err)
		}
	}
	cache.Invalidate(nil)
	if value, err := cache.Get(context.Background(), nil); err != nil || value != 2 {
		t.Fatalf("nil interface invalidation failed: value=%d err=%v", value, err)
	}
	for i := 0; i < 4096; i++ {
		if _, err := cache.Get(context.Background(), i); err != nil {
			t.Fatal(err)
		}
	}
}
