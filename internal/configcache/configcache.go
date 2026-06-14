// Package configcache provides a generic TTL cache with explicit invalidation
// for configuration reads (DNS zones, routes, SMTP policy). It exists so that
// write paths can evict stale entries immediately instead of waiting out the
// TTL — the TTL remains as a crash-consistent fallback.
package configcache

import (
	"context"
	"reflect"
	"sync"
	"time"
)

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
	// nilOK allows caching the zero value / nil pointer (negative caching),
	// e.g. "this domain has no zone" to avoid re-querying the parent chain.
	nilOK bool
}

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
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
		ttl:     ttl,
		loader:  loader,
		entries: make(map[K]cacheEntry[V]),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the cached value if present and unexpired, otherwise invokes the
// loader and caches the result. When nilOK is false, nil/zero loader results
// are returned to the caller but not cached.
func (c *ConfigCache[K, V]) Get(ctx context.Context, key K) (V, error) {
	now := time.Now()

	c.mu.RLock()
	if entry, ok := c.entries[key]; ok && now.Before(entry.expiresAt) {
		v := entry.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	value, err := c.loader(ctx, key)
	if err != nil {
		var zero V
		return zero, err
	}
	if !c.nilOK && isZero(value) {
		return value, nil
	}

	c.mu.Lock()
	c.entries[key] = cacheEntry[V]{value: value, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return value, nil
}

// Invalidate drops a single key. Safe to call for keys that are not present.
func (c *ConfigCache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// InvalidateAll drops every cached entry.
func (c *ConfigCache[K, V]) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[K]cacheEntry[V])
	c.mu.Unlock()
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
