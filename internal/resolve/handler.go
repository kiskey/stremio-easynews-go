package resolve

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

// CreateResolveHandler builds the Gin handler for GET /resolve/:payload/:filename
func CreateResolveHandler(logger shared.Logger) gin.HandlerFunc {
	timeoutMs := shared.ParseIntEnv("RESOLVE_TIMEOUT_MS", 20000)

	// Share a single highly optimized client instance to reuse TCP/TLS connection pool across all resolves
	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Prevent the client from following redirects and downloading video content
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return func(c *gin.Context) {
		payload := c.Param("payload")

		// 1. Return cached CDN location if still fresh and valid
		if cachedUrl, ok := GetCachedResolvedUrl(payload); ok {
			c.Redirect(http.StatusTemporaryRedirect, cachedUrl)
			return
		}

		// 2. Parse and validate the incoming base64 payload parameters
		target, err := ParseResolvePayload(payload)
		if err != nil {
			if re, ok := err.(*ResolveError); ok {
				c.String(re.Status, re.Message)
			} else {
				c.String(http.StatusBadRequest, "Invalid request")
			}
			return
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), "GET", target.CleanUrl, nil)
		if err != nil {
			logger.Error("Failed to build resolve HTTP request: %v", err)
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}

		req.Header.Set("Authorization", target.AuthHeader)
		// Request 1 byte range to keep the network payload near zero in case the server ignores redirects
		req.Header.Set("Range", "bytes=0-0")

		resp, err := client.Do(req)
		if err != nil {
			logger.Error("Network error resolving stream destination '%s': %v", target.CleanUrl, err)
			c.String(http.StatusBadGateway, "Error resolving stream")
			return
		}
		defer func() {
			// Drain remaining bytes to allow socket recycling
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		// 4. Intercept the geography-aware CDN redirect URL from the response location header
		location := resp.Header.Get("Location")
		finalUrl := location
		if finalUrl == "" {
			finalUrl = target.CleanUrl
		}

		// Cache only if a redirected URL was returned
		if location != "" {
			SetCachedResolvedUrl(payload, location)
		}

		c.Redirect(http.StatusTemporaryRedirect, finalUrl)
	}
}
