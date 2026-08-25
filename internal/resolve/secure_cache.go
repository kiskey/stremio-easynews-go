package resolve

import (
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

var (
	secureResolvedURLTTL = time.Duration(shared.ParseIntEnv("RESOLVE_CACHE_TTL_SECONDS", 300)) * time.Second
	secureResolverCache  = shared.NewTTLCache[string, string](shared.ParseIntEnv("RESOLVE_CACHE_ENTRIES", 5000), secureResolvedURLTTL)
)

func GetSecureCachedResolvedURL(payload string) (string, bool) {
	return secureResolverCache.Get(payload)
}

func SetSecureCachedResolvedURL(payload, targetURL string) {
	secureResolverCache.Set(payload, targetURL)
}

func ClearSecureResolvedURLCache() {
	secureResolverCache.Clear()
}

// SecureResolverCacheStats exposes aggregate counters only; no payloads,
// credentials or resolved URLs are returned.
func SecureResolverCacheStats() shared.CacheStats {
	return secureResolverCache.Stats()
}
