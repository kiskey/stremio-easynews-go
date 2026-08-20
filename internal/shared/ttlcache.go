package shared

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// CacheStats is a point-in-time, non-secret snapshot of one bounded TTL cache.
type CacheStats struct {
	Entries   int
	Capacity  int
	Hits      uint64
	Misses    uint64
	Sets      uint64
	Expired   uint64
	Evictions uint64
}

// Requests returns the number of Get operations observed by the cache.
func (s CacheStats) Requests() uint64 {
	return s.Hits + s.Misses
}

// HitRate returns a value in the range [0,1].
func (s CacheStats) HitRate() float64 {
	requests := s.Requests()
	if requests == 0 {
		return 0
	}
	return float64(s.Hits) / float64(requests)
}

func (s CacheStats) String() string {
	return fmt.Sprintf("entries=%d/%d hits=%d misses=%d hitRate=%.1f%% sets=%d expired=%d evictions=%d",
		s.Entries, s.Capacity, s.Hits, s.Misses, s.HitRate()*100, s.Sets, s.Expired, s.Evictions)
}

type ttlCacheEntry[V any] struct {
	value     V
	expiresAt int64
	element   *list.Element
}

// TTLCache is a bounded, concurrency-safe in-memory TTL cache with deterministic
// LRU eviction. It has no background goroutine: expired entries are removed on
// access and opportunistically before capacity eviction.
type TTLCache[K comparable, V any] struct {
	mu         sync.Mutex
	data       map[K]*ttlCacheEntry[V]
	lru        list.List // front = most recently used; elements contain K
	maxEntries int
	defaultTTL time.Duration

	hits      uint64
	misses    uint64
	sets      uint64
	expired   uint64
	evictions uint64
}

func NewTTLCache[K comparable, V any](maxEntries int, defaultTTL time.Duration) *TTLCache[K, V] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &TTLCache[K, V]{
		data:       make(map[K]*ttlCacheEntry[V], maxEntries),
		maxEntries: maxEntries,
		defaultTTL: defaultTTL,
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	value, _, ok := c.GetWithRemainingTTL(key)
	return value, ok
}

// GetWithRemainingTTL returns the cached value and the remaining lifetime of
// the entry. A zero remaining TTL means the entry has no expiration.
func (c *TTLCache[K, V]) GetWithRemainingTTL(key K) (V, time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.data[key]
	if !ok {
		c.misses++
		var zero V
		return zero, 0, false
	}

	now := time.Now().UnixNano()
	if entry.expiresAt > 0 && now >= entry.expiresAt {
		c.removeLocked(key, entry)
		c.expired++
		c.misses++
		var zero V
		return zero, 0, false
	}

	c.lru.MoveToFront(entry.element)
	c.hits++
	remaining := time.Duration(0)
	if entry.expiresAt > 0 {
		remaining = time.Duration(entry.expiresAt - now)
	}
	return entry.value, remaining, true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

func (c *TTLCache[K, V]) SetWithTTL(key K, value V, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	now := time.Now()
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = now.Add(ttl).UnixNano()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.sets++
	if existing, ok := c.data[key]; ok {
		existing.value = value
		existing.expiresAt = expiresAt
		c.lru.MoveToFront(existing.element)
		return
	}

	element := c.lru.PushFront(key)
	c.data[key] = &ttlCacheEntry[V]{
		value:     value,
		expiresAt: expiresAt,
		element:   element,
	}

	if len(c.data) <= c.maxEntries {
		return
	}

	// Prefer reclaiming expired entries before evicting a live LRU entry.
	c.pruneExpiredLocked(now.UnixNano())
	for len(c.data) > c.maxEntries {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		oldestKey := oldest.Value.(K)
		entry := c.data[oldestKey]
		c.removeLocked(oldestKey, entry)
		c.evictions++
	}
}

func (c *TTLCache[K, V]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.data[key]
	if !ok {
		return false
	}
	c.removeLocked(key, entry)
	return true
}

func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[K]*ttlCacheEntry[V], c.maxEntries)
	c.lru.Init()
	c.hits = 0
	c.misses = 0
	c.sets = 0
	c.expired = 0
	c.evictions = 0
}

func (c *TTLCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.data)
}

func (c *TTLCache[K, V]) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{
		Entries:   len(c.data),
		Capacity:  c.maxEntries,
		Hits:      c.hits,
		Misses:    c.misses,
		Sets:      c.sets,
		Expired:   c.expired,
		Evictions: c.evictions,
	}
}

func (c *TTLCache[K, V]) pruneExpiredLocked(now int64) {
	for key, entry := range c.data {
		if entry.expiresAt > 0 && now >= entry.expiresAt {
			c.removeLocked(key, entry)
			c.expired++
		}
	}
}

func (c *TTLCache[K, V]) removeLocked(key K, entry *ttlCacheEntry[V]) {
	delete(c.data, key)
	if entry != nil && entry.element != nil {
		c.lru.Remove(entry.element)
	}
}
