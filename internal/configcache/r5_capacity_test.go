package configcache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// The production default must bound retained results independently of how many
// different domains (including misses) arrive during one process lifetime.
// These tests use only the existing constructor and operations on the baseline.
const progressCacheCapacity = 4096

func TestProgressConfigCacheRetainedCapacity(t *testing.T) {
	for _, negative := range []bool{false, true} {
		t.Run(fmt.Sprintf("negative=%v", negative), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := make(map[string]int)
				cache := New(time.Hour, func(_ context.Context, key string) (int, error) {
					calls[key]++
					if negative {
						return 0, nil
					}
					return calls[key], nil
				}, WithNilCache[string, int](true))
				for i := 0; i <= 2*progressCacheCapacity; i++ {
					key := fmt.Sprintf("domain-%05d.test", i)
					if _, err := cache.Get(context.Background(), key); err != nil {
						t.Fatal(err)
					}
					if len(cache.entries) > progressCacheCapacity {
						t.Fatalf("unique keys retained without a capacity bound: %d", len(cache.entries))
					}
				}
				if len(cache.inflight) != 0 {
					t.Fatalf("completed loads retained active-key history: %d", len(cache.inflight))
				}
				for _, key := range []string{"domain-08192.test", "domain-00000.test"} {
					if _, err := cache.Get(context.Background(), key); err != nil {
						t.Fatal(err)
					}
				}
				if calls["domain-08192.test"] != 1 || calls["domain-00000.test"] != 2 {
					t.Fatalf("recent result or evicted result has wrong lookup behavior: %v", map[string]int{"recent": calls["domain-08192.test"], "evicted": calls["domain-00000.test"]})
				}
			})
		})
	}
}

func TestProgressConfigCacheReclaimsExpiredColdKeys(t *testing.T) {
	for _, mode := range []string{"successful miss", "noncached zero", "loader failure"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := errors.New("controlled lookup failure")
				cache := New(time.Second, func(_ context.Context, key int) (int, error) {
					if key == -1 {
						switch mode {
						case "noncached zero":
							return 0, nil
						case "loader failure":
							return 0, cause
						}
					}
					return 1, nil
				})
				for i := 0; i < 1000; i++ {
					if _, err := cache.Get(context.Background(), i); err != nil {
						t.Fatal(err)
					}
				}
				// Exact TTL equality is already expired, including entries that
				// are never requested again.
				time.Sleep(time.Second)
				_, err := cache.Get(context.Background(), -1)
				if (mode == "loader failure") != errors.Is(err, cause) {
					t.Fatalf("loader error contract changed: %v", err)
				}
				want := 0
				if mode == "successful miss" {
					want = 1
				}
				if len(cache.entries) != want || len(cache.inflight) != 0 {
					t.Fatalf("cold expired entries were retained: entries=%d active=%d want=%d", len(cache.entries), len(cache.inflight), want)
				}
			})
		})
	}
}

func TestProgressConfigCacheNonpositiveTTLRetainsNothing(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			calls := 0
			cache := New(ttl, func(_ context.Context, _ int) (int, error) {
				calls++
				return calls, nil
			})
			for i := 0; i < 3; i++ {
				if value, err := cache.Get(context.Background(), 1); err != nil || value != i+1 {
					t.Fatalf("nonpositive TTL lookup changed: value=%d err=%v", value, err)
				}
			}
			if len(cache.entries) != 0 || len(cache.inflight) != 0 {
				t.Fatalf("disabled cache retained results: entries=%d active=%d", len(cache.entries), len(cache.inflight))
			}
		})
	}
}

func TestProgressConfigCacheInvalidationKeepsCapacityConsistent(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%v", all), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := make(map[string]int)
				cache := New(time.Hour, func(_ context.Context, key string) (int, error) {
					calls[key]++
					return calls[key], nil
				})
				for i := 0; i < 2*progressCacheCapacity; i++ {
					if _, err := cache.Get(context.Background(), "changed"); err != nil {
						t.Fatal(err)
					}
					if all {
						cache.InvalidateAll()
					} else {
						cache.Invalidate("changed")
					}
				}
				for i := 0; i < progressCacheCapacity; i++ {
					if _, err := cache.Get(context.Background(), fmt.Sprintf("fresh-%d", i)); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := cache.Get(context.Background(), "fresh-0"); err != nil || calls["fresh-0"] != 1 {
					t.Fatalf("invalidation left stale eviction metadata: calls=%d err=%v", calls["fresh-0"], err)
				}
				cache.Get(context.Background(), "overflow")
				cache.Get(context.Background(), "fresh-0")
				if calls["fresh-0"] != 2 || len(cache.entries) != progressCacheCapacity {
					t.Fatalf("capacity eviction after invalidation is incorrect: calls=%d entries=%d", calls["fresh-0"], len(cache.entries))
				}
			})
		})
	}
}

func TestProgressConfigCacheConcurrentPublicationBound(t *testing.T) {
	cache := New(time.Hour, func(_ context.Context, key string) (string, error) { return key, nil })
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 512; i++ {
				key := fmt.Sprintf("worker-%d/domain-%d", worker, i)
				if value, err := cache.Get(context.Background(), key); err != nil || value != key {
					t.Errorf("concurrent caller lost its value: value=%q err=%v", value, err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	if len(cache.entries) > progressCacheCapacity || len(cache.inflight) != 0 {
		t.Fatalf("concurrent loads exceeded retention bound: entries=%d active=%d", len(cache.entries), len(cache.inflight))
	}
}
