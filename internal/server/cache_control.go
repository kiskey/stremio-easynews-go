package server

import (
	"fmt"
	"strings"

	"github.com/kiskey/stremio-easynews-go/internal/addon"
)

func streamCacheControlValue(result addon.StreamHandlerResult) string {
	if result.CacheMaxAge <= 0 {
		return ""
	}

	directives := []string{fmt.Sprintf("max-age=%d", result.CacheMaxAge), "public"}
	if result.StaleRevalidate > 0 {
		directives = append(directives, fmt.Sprintf("stale-while-revalidate=%d", result.StaleRevalidate))
	}
	if result.StaleError > 0 {
		directives = append(directives, fmt.Sprintf("stale-if-error=%d", result.StaleError))
	}
	return strings.Join(directives, ", ")
}
