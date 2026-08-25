package resolve

import (
	"github.com/gin-gonic/gin"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

// CreateResolveHandler remains for source compatibility and now delegates to
// the secure Batch-8 implementation.
func CreateResolveHandler(logger shared.Logger) gin.HandlerFunc {
	return CreateSecureResolveHandler(logger)
}
