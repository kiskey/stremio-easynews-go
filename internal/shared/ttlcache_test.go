package shared

import (
	"sync"
	"testing"
	"time"
)

func TestTTLCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := NewTTLCache[string, int](2, time.Minute)
	cache.Set("a", 1)
	cache.Set("b", 2)
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected a cache hit")
	}
	cache.Set("c", 3)

	if _, ok := cache.Get("b"); ok {
		t.Fatal("least recently used entry b should have been evicted")
	}
	if got, ok := cache.Get("a"); !ok || got != 1 {
		t.Fatalf("recently used a should remain, got=%d ok=%v", got, ok)
	}
	if stats := cache.Stats(); stats.Evictions != 1 {
		t.Fatalf("expected one LRU eviction, got %+v", stats)
	}
}

func TestTTLCachePrefersExpiredEntriesBeforeLiveEviction(t *testing.T) {
	cache := NewTTLCache[string, int](2, time.Minute)
	cache.SetWithTTL("expired", 1, time.Millisecond)
	cache.Set("live", 2)
	time.Sleep(5 * time.Millisecond)
	cache.Set("new", 3)

	if _, ok := cache.Get("expired"); ok {
		t.Fatal("expired entry should not survive capacity pruning")
	}
	if _, ok := cache.Get("live"); !ok {
		t.Fatal("live entry should remain when an expired entry can be reclaimed")
	}
	stats := cache.Stats()
	if stats.Expired == 0 {
		t.Fatalf("expected expired accounting, got %+v", stats)
	}
	if stats.Evictions != 0 {
		t.Fatalf("live LRU eviction should not be needed, got %+v", stats)
	}
}

func TestTTLCacheStatsAndClear(t *testing.T) {
	cache := NewTTLCache[string, int](2, time.Minute)
	cache.Set("a", 1)
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected hit")
	}
	if _, ok := cache.Get("missing"); ok {
		t.Fatal("unexpected hit")
	}
	stats := cache.Stats()
	if stats.Hits != 1 || stats.Misses != 1 || stats.Sets != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.HitRate() != 0.5 {
		t.Fatalf("unexpected hit rate: %v", stats.HitRate())
	}

	cache.Clear()
	stats = cache.Stats()
	if stats.Entries != 0 || stats.Hits != 0 || stats.Misses != 0 || stats.Sets != 0 {
		t.Fatalf("Clear should reset entries and counters: %+v", stats)
	}
}

func TestTTLCacheConcurrentAccess(t *testing.T) {
	cache := NewTTLCache[int, int](64, time.Minute)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := (base + i) % 128
				cache.Set(key, i)
				_, _ = cache.Get(key)
			}
		}(g)
	}
	wg.Wait()
	if cache.Len() > 64 {
		t.Fatalf("cache exceeded capacity: %d", cache.Len())
	}
}

func TestTTLCacheRemainingTTLDecreases(t *testing.T) {
	cache := NewTTLCache[string, int](2, time.Second)
	cache.SetWithTTL("a", 1, 200*time.Millisecond)
	_, first, ok := cache.GetWithRemainingTTL("a")
	if !ok || first <= 0 || first > 200*time.Millisecond {
		t.Fatalf("unexpected initial remaining TTL: %v ok=%v", first, ok)
	}
	time.Sleep(10 * time.Millisecond)
	_, second, ok := cache.GetWithRemainingTTL("a")
	if !ok || second <= 0 || second >= first {
		t.Fatalf("remaining TTL should decrease: first=%v second=%v ok=%v", first, second, ok)
	}
}
