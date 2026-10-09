package configcache_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/configcache"
)

type cacheReadResult struct {
	value int
	err   error
}

func receiveCacheResult[T any](t *testing.T, ctx context.Context, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-ctx.Done():
		t.Fatal("controlled cache read did not reach its barrier:", ctx.Err())
		var zero T
		return zero
	}
}

func TestR5ConfigInvalidationFencesInflightResults(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, negative := range []bool{false, true} {
			for _, newerFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("all=%v/negative=%v/newer-first=%v", all, negative, newerFirst), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					started, release := make(chan struct{}), make(chan struct{})
					var calls, current atomic.Int32
					old := 11
					if negative {
						old = 0
					}
					current.Store(int32(old))
					cache := configcache.New(time.Minute, func(ctx context.Context, _ string) (int, error) {
						value := int(current.Load())
						if calls.Add(1) == 1 {
							close(started)
							select {
							case <-release:
							case <-ctx.Done():
								return 0, ctx.Err()
							}
						}
						return value, nil
					}, configcache.WithNilCache[string, int](true))
					oldResult := make(chan cacheReadResult, 1)
					go func() {
						value, err := cache.Get(ctx, "policy")
						oldResult <- cacheReadResult{value, err}
					}()
					receiveCacheResult(t, ctx, started)
					current.Store(22)
					if all {
						cache.InvalidateAll()
					} else {
						cache.Invalidate("policy")
					}
					if newerFirst {
						if value, err := cache.Get(ctx, "policy"); err != nil || value != 22 {
							t.Fatalf("fresh generation failed: %d, %v", value, err)
						}
					}
					close(release)
					// An already-started caller may finish with its own old snapshot.
					if result := receiveCacheResult(t, ctx, oldResult); result.err != nil || result.value != old {
						t.Fatalf("in-flight caller result changed: %+v", result)
					}
					for i := 0; i < 2; i++ {
						if value, err := cache.Get(ctx, "policy"); err != nil || value != 22 {
							t.Fatalf("late read restored invalidated config: %d, %v", value, err)
						}
					}
					if calls.Load() != 2 {
						t.Fatalf("fresh result was not cached once: loader calls=%d", calls.Load())
					}
				})
			}
		}
	}
}

func TestR5ConfigInvalidationKeepsUnrelatedInflightKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	cache := configcache.New(time.Minute, func(ctx context.Context, key string) (int, error) {
		if key == "unrelated" && calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return 5, nil
	})
	result := make(chan cacheReadResult, 1)
	go func() {
		value, err := cache.Get(ctx, "unrelated")
		result <- cacheReadResult{value, err}
	}()
	receiveCacheResult(t, ctx, started)
	cache.Invalidate("changed")
	close(release)
	if read := receiveCacheResult(t, ctx, result); read.err != nil || read.value != 5 {
		t.Fatalf("unrelated in-flight read changed: %+v", read)
	}
	if value, err := cache.Get(ctx, "unrelated"); err != nil || value != 5 || calls.Load() != 1 {
		t.Fatalf("single-key invalidation discarded unrelated load: %d, %v, calls=%d", value, err, calls.Load())
	}
}

func TestR5ConfigInvalidatedFailureCannotEvictFreshResult(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%v", all), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			cause := errors.New("old loader failed")
			cache := configcache.New(time.Minute, func(ctx context.Context, _ string) (int, error) {
				if calls.Add(1) == 1 {
					close(started)
					select {
					case <-release:
						return 9, cause
					case <-ctx.Done():
						return 0, ctx.Err()
					}
				}
				return 12, nil
			})
			result := make(chan cacheReadResult, 1)
			go func() {
				value, err := cache.Get(ctx, "key")
				result <- cacheReadResult{value, err}
			}()
			receiveCacheResult(t, ctx, started)
			if all {
				cache.InvalidateAll()
			} else {
				cache.Invalidate("key")
			}
			if value, err := cache.Get(ctx, "key"); err != nil || value != 12 {
				t.Fatalf("fresh load failed: %d, %v", value, err)
			}
			close(release)
			if old := receiveCacheResult(t, ctx, result); old.value != 0 || !errors.Is(old.err, cause) {
				t.Fatalf("loader error contract changed: %+v", old)
			}
			if value, err := cache.Get(ctx, "key"); err != nil || value != 12 || calls.Load() != 2 {
				t.Fatalf("old failure evicted fresh value: %d, %v, calls=%d", value, err, calls.Load())
			}
		})
	}
}
