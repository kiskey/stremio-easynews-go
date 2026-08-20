package addon

import (
	"sync/atomic"

	"github.com/kiskey/stremio-easynews-go/internal/api"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

var (
	cacheStatsLogEvery      = shared.ParseIntEnv("CACHE_STATS_LOG_EVERY", 100)
	staleRevalidateSeconds  = nonNegativeInt(shared.ParseIntEnv("STREMIO_STALE_REVALIDATE_SECONDS", 3600))
	staleIfErrorSeconds     = nonNegativeInt(shared.ParseIntEnv("STREMIO_STALE_ERROR_SECONDS", 86400))
	streamRequestStatsCount atomic.Uint64
)

// CacheStatsSnapshot contains only aggregate counters and never cache keys,
// credentials, titles, queries, URLs, or other user-specific values.
type CacheStatsSnapshot struct {
	Request  shared.CacheStats
	Easynews shared.CacheStats
	Metadata shared.CacheStats
}

func nonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func streamResultWithCachePolicy(streams []Stream, cacheMaxAge int) StreamHandlerResult {
	return StreamHandlerResult{
		Streams:         streams,
		CacheMaxAge:     cacheMaxAge,
		StaleRevalidate: staleRevalidateSeconds,
		StaleError:      staleIfErrorSeconds,
	}
}

func addCacheStats(total *shared.CacheStats, next shared.CacheStats) {
	total.Entries += next.Entries
	total.Capacity += next.Capacity
	total.Hits += next.Hits
	total.Misses += next.Misses
	total.Sets += next.Sets
	total.Expired += next.Expired
	total.Evictions += next.Evictions
}

func metadataCacheStats() shared.CacheStats {
	var total shared.CacheStats
	for _, stats := range []shared.CacheStats{
		imdbToTMDBIDCache.Stats(),
		tmdbAltTitlesCache.Stats(),
		tmdbDetailsCache.Stats(),
		tmdbTransTitleCache.Stats(),
		tmdbSeasonAirDateCache.Stats(),
		metaResponseCache.Stats(),
	} {
		addCacheStats(&total, stats)
	}
	return total
}

// GetCacheStats returns a low-cost operational snapshot suitable for logs and
// future diagnostics. It intentionally exposes no cache keys or payload data.
func GetCacheStats() CacheStatsSnapshot {
	return CacheStatsSnapshot{
		Request:  requestCache.Stats(),
		Easynews: api.SearchCacheStats(),
		Metadata: metadataCacheStats(),
	}
}

func maybeLogCacheStats() {
	if cacheStatsLogEvery <= 0 {
		return
	}
	requestNumber := streamRequestStatsCount.Add(1)
	if requestNumber%uint64(cacheStatsLogEvery) != 0 {
		return
	}

	stats := GetCacheStats()
	addonLogger.Info("Cache stats after %d stream requests: request={%s} easynews={%s} metadata={%s}",
		requestNumber, stats.Request.String(), stats.Easynews.String(), stats.Metadata.String())
}
