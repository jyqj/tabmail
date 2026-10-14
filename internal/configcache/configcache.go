// Package configcache provides a generic TTL cache with explicit invalidation
// for configuration reads (DNS zones, routes, SMTP policy). It exists so that
// write paths can evict stale entries immediately instead of waiting out the
// TTL — the TTL remains as a crash-consistent fallback.
package configcache

import (
	"container/list"
	"context"
	"reflect"
	"sync"
	"time"
)

// Bound retained positive and negative results even when their keys are drawn
// from untrusted SMTP recipient domains. Values are evicted in publication
// order; a hit keeps its original TTL and the existing read-lock fast path.
const maxEntries = 4096

// Loader fetches the value for a key on cache miss.
type Loader[K comparable, V any] func(ctx context.Context, key K) (V, error)

// ConfigCache is a TTL cache keyed by K. Values returned from Get should be
// treated as read-only by callers; loaders are responsible for returning copies
// where mutation isolation matters.
type ConfigCache[K comparable, V any] struct {
	mu      sync.RWMutex
	ttl     time.Duration
	loader  Loader[K, V]
	entries map[K]cacheEntry[V]
	order   *list.List
	// A token belongs only to currently running loads. Invalidation detaches
	// it so a late result cannot restore an evicted value or replace a new one.
	inflight map[K]*loadGeneration
	// nilOK allows caching the zero value / nil pointer (negative caching),
	// e.g. "this domain has no zone" to avoid re-querying the parent chain.
	nilOK bool
}

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
	order     *list.Element
}

// Wrapping the key also permits nil interface keys without a nil type assertion.
type orderedKey[K comparable] struct{ key K }

type loadGeneration struct {
	active int
}

// Option configures a ConfigCache.
type Option[K comparable, V any] func(*ConfigCache[K, V])

// WithNilCache enables caching of nil/zero values (negative caching). Off by
// default so transient empty reads are not pinned for the full TTL.
func WithNilCache[K comparable, V any](b bool) Option[K, V] {
	return func(c *ConfigCache[K, V]) { c.nilOK = b }
}

// New constructs a ConfigCache with the given TTL and loader.
func New[K comparable, V any](ttl time.Duration, loader Loader[K, V], opts ...Option[K, V]) *ConfigCache[K, V] {
	c := &ConfigCache[K, V]{
		ttl:      ttl,
		loader:   loader,
		entries:  make(map[K]cacheEntry[V]),
		order:    list.New(),
		inflight: make(map[K]*loadGeneration),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the cached value if present and unexpired, otherwise invokes the
// loader and caches the result. When nilOK is false, nil/zero loader results
// are returned to the caller but not cached. A load already in progress when
// invalidated may finish for its caller, but its result cannot enter the cache.
// Cache misses reclaim cold expired entries; idle caches retain at most 4096
// entries. Nonpositive TTLs retain no results. No background worker is required.
func (c *ConfigCache[K, V]) Get(ctx context.Context, key K) (V, error) {
	// Comparable keys can still be nonreflexive (NaN, including inside arrays
	// or structs). Such map records can never be retrieved or deleted by key;
	// preserve the uncached result without retaining an unreachable load token.
	if key != key {
		value, err := c.loader(ctx, key)
		if err != nil {
			var zero V
			return zero, err
		}
		return value, nil
	}
	now := time.Now()

	c.mu.RLock()
	if entry, ok := c.entries[key]; ok && now.Before(entry.expiresAt) {
		v := entry.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	c.pruneExpiredLocked(time.Now())
	// Another load may have filled the cache while this miss acquired the lock.
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry.value, nil
	}
	generation := c.inflight[key]
	if generation == nil {
		generation = &loadGeneration{}
		c.inflight[key] = generation
	}
	generation.active++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.inflight[key] == generation {
			generation.active--
			if generation.active == 0 {
				delete(c.inflight, key)
			}
		}
		c.mu.Unlock()
	}()

	value, err := c.loader(ctx, key)
	cacheable := err == nil && c.ttl > 0 && (c.nilOK || !isZero(value))

	c.mu.Lock()
	if c.inflight[key] == generation && cacheable {
		now := time.Now()
		c.pruneExpiredLocked(now)
		// Concurrent loads of this generation may publish the same key more
		// than once. Its previous queue node must not consume capacity later.
		c.removeEntryLocked(key)
		for len(c.entries) >= maxEntries {
			c.removeEntryLocked(c.order.Front().Value.(orderedKey[K]).key)
		}
		c.entries[key] = cacheEntry[V]{value: value, expiresAt: now.Add(c.ttl), order: c.order.PushBack(orderedKey[K]{key: key})}
	}
	c.mu.Unlock()
	if err != nil {
		var zero V
		return zero, err
	}
	return value, nil
}

// Invalidate drops a single key. Safe to call for keys that are not present.
func (c *ConfigCache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	c.removeEntryLocked(key)
	delete(c.inflight, key)
	c.mu.Unlock()
}

// InvalidateAll drops every cached entry.
func (c *ConfigCache[K, V]) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[K]cacheEntry[V])
	c.order.Init()
	c.inflight = make(map[K]*loadGeneration)
	c.mu.Unlock()
}

func (c *ConfigCache[K, V]) removeEntryLocked(key K) {
	if entry, ok := c.entries[key]; ok {
		c.order.Remove(entry.order)
		delete(c.entries, key)
	}
}

func (c *ConfigCache[K, V]) pruneExpiredLocked(now time.Time) {
	// One fixed TTL means publication order is also expiration order. Each
	// entry is removed once, so a miss never scans the entire live cache.
	for oldest := c.order.Front(); oldest != nil; oldest = c.order.Front() {
		key := oldest.Value.(orderedKey[K]).key
		if now.Before(c.entries[key].expiresAt) {
			return
		}
		c.removeEntryLocked(key)
	}
}

// isZero reports whether v is the zero value for its type. It handles
// non-comparable value types (slices, maps, structs containing slices) that
// would panic under a naive == comparison: nil-able kinds (pointer, slice,
// map, chan, func, interface) are tested for nil via reflect, and all other
// kinds use reflect's IsZero.
func isZero[V any](v V) bool {
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return rv.IsNil()
	case reflect.Interface:
		// A nil interface (no concrete type) is zero; a non-nil interface
		// wrapping a nil pointer/slice/etc. is treated as non-zero so that
		// typed-nil wrappers are still cacheable.
		return !rv.IsValid() || rv.IsNil()
	default:
		return rv.IsZero()
	}
}
