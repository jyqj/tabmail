package configcache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigCacheRecomputesAfterTTLExpiry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New[string, int](20*time.Millisecond, func(ctx context.Context, key string) (int, error) {
		return int(calls.Add(1)), nil
	})

	first, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("expected cached hit within TTL: first=%d second=%d", first, second)
	}

	time.Sleep(25 * time.Millisecond)
	third, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatalf("expected recompute after TTL expiry: third=%d", third)
	}
}

func TestConfigCacheInvalidateDropsSingleKey(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New[string, int](time.Minute, func(ctx context.Context, key string) (int, error) {
		return int(calls.Add(1)), nil
	})

	if _, err := c.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 loader calls, got %d", got)
	}

	c.Invalidate("a")

	if _, err := c.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 loader calls after invalidating a, got %d", got)
	}
	// b must still be cached.
	if _, err := c.Get(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected b to stay cached, got %d loader calls", got)
	}
}

func TestConfigCacheInvalidateAllDropsEveryKey(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New[string, int](time.Minute, func(ctx context.Context, key string) (int, error) {
		return int(calls.Add(1)), nil
	})

	if _, err := c.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}

	c.InvalidateAll()

	if _, err := c.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("expected 4 loader calls after InvalidateAll, got %d", got)
	}
}

func TestConfigCachePropagatesLoaderError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	c := New[string, int](time.Minute, func(ctx context.Context, key string) (int, error) {
		return 0, sentinel
	})
	if _, err := c.Get(context.Background(), "k"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestConfigCacheWithNilCacheCachesZeroValues(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New[string, *int](time.Minute, func(ctx context.Context, key string) (*int, error) {
		calls.Add(1)
		return nil, nil
	}, WithNilCache[string, *int](true))

	for i := 0; i < 3; i++ {
		v, err := c.Get(context.Background(), "k")
		if err != nil {
			t.Fatal(err)
		}
		if v != nil {
			t.Fatalf("expected nil value, got %v", v)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected nil value to be cached (1 call), got %d", got)
	}
}

func TestConfigCacheWithoutNilCacheSkipsZeroValues(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New[string, *int](time.Minute, func(ctx context.Context, key string) (*int, error) {
		calls.Add(1)
		return nil, nil
	})

	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "k"); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected nil value to skip cache (3 calls), got %d", got)
	}
}

func TestConfigCacheConcurrentGetAndInvalidate(t *testing.T) {
	// Run with -race: ensures Get (R-then-upgrade) and Invalidate (W) do not
	// produce a data race on the internal map.
	t.Parallel()
	var calls atomic.Int32
	c := New[string, int](50*time.Millisecond, func(ctx context.Context, key string) (int, error) {
		return int(calls.Add(1)), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := c.Get(context.Background(), "k"); err != nil {
					t.Errorf("get error: %v", err)
					return
				}
				if i%2 == 0 {
					c.Invalidate("k")
				} else {
					c.InvalidateAll()
				}
			}
		}(i)
	}
	wg.Wait()
}
