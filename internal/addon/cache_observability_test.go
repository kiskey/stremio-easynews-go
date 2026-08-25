package addon

import (
	"testing"
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

func TestStreamResultWithCachePolicy(t *testing.T) {
	oldRevalidate := staleRevalidateSeconds
	oldError := staleIfErrorSeconds
	staleRevalidateSeconds = 123
	staleIfErrorSeconds = 456
	defer func() {
		staleRevalidateSeconds = oldRevalidate
		staleIfErrorSeconds = oldError
	}()

	result := streamResultWithCachePolicy([]Stream{{Name: "test"}}, 600)
	if result.CacheMaxAge != 600 || result.StaleRevalidate != 123 || result.StaleError != 456 {
		t.Fatalf("unexpected cache policy: %+v", result)
	}
}

func TestRequestCacheUsesPerEntryTTLAndStats(t *testing.T) {
	oldCache := requestCache
	requestCache = shared.NewTTLCache[string, StreamHandlerResult](4, 0)
	defer func() { requestCache = oldCache }()

	setRequestCache("short", StreamHandlerResult{CacheMaxAge: 60}, 10*time.Millisecond)
	hit, ok := getFromRequestCache("short")
	if !ok {
		t.Fatal("expected immediate request-cache hit")
	}
	if hit.CacheMaxAge >= 60 {
		t.Fatalf("cache hit must advertise remaining TTL, not reset max-age: %d", hit.CacheMaxAge)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := getFromRequestCache("short"); ok {
		t.Fatal("expired request-cache entry should miss")
	}
	stats := requestCache.Stats()
	if stats.Hits != 1 || stats.Misses != 1 || stats.Expired != 1 {
		t.Fatalf("unexpected request-cache stats: %+v", stats)
	}
}
