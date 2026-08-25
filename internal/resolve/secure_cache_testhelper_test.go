package resolve

import (
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

func newTestResolverCache(capacity int) *shared.TTLCache[string, string] {
	return shared.NewTTLCache[string, string](capacity, time.Minute)
}
