package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/kiskey/stremio-easynews-go/internal/addon"
	"github.com/kiskey/stremio-easynews-go/internal/i18n"
	"github.com/kiskey/stremio-easynews-go/internal/resolve"
	"github.com/kiskey/stremio-easynews-go/internal/seal"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

var serverLogger = shared.CreateLogger("Server", "")

// ServeHTTP initializes and boots the high-performance HTTP gateway.
func ServeHTTP(port int) {
	if port == 0 {
		port = shared.ParseIntEnv("PORT", 1337)
	}

	if os.Getenv("EASYNEWS_LOG_LEVEL") != "debug" && os.Getenv("EASYNEWS_LOG_LEVEL") != "silly" {
		gin.SetMode(gin.ReleaseMode)
	}

	if err := seal.Init(); err != nil {
		serverLogger.Warn("Seal initialization skipped: %v (falling back to legacy config mode)", err)
	} else {
		serverLogger.Info("Config encryption enabled (AES-256-GCM)")
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	r.Use(corsMiddleware())
	r.Use(requestLogger())

	cacheMaxAge := shared.ParseIntEnv("CACHE_MAX_AGE", 0)
	if cacheMaxAge > 0 {
		r.Use(func(c *gin.Context) {
			if c.Writer.Header().Get("Cache-Control") == "" {
				c.Header("Cache-Control", fmt.Sprintf("max-age=%d, public", cacheMaxAge))
			}
			c.Next()
		})
	}

	// -----------------------------------------------------------------------
	// Stremio Protocol Gateway Routes
	// -----------------------------------------------------------------------

	// Called once by the configure page at install time to generate the
	// encrypted URL slug. The response is secret-bearing and must never be
	// cached by browsers or intermediary proxies.
	r.POST("/api/config/seal", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")

		if !seal.Enabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Encryption is not enabled on the server"})
			return
		}

		var cfg addon.AddonConfig
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config payload"})
			return
		}
		if cfg.Username == "" || cfg.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password are required"})
			return
		}

		tok, err := seal.SealConfig(cfg)
		if err != nil {
			serverLogger.Error("Failed to seal config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt configuration"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": tok})
	})

	r.GET("/manifest.json", func(c *gin.Context) {
		m := addon.BuildManifest()
		c.JSON(http.StatusOK, m)
	})

	r.GET("/:config/manifest.json", func(c *gin.Context) {
		configStr := c.Param("config")
		config := addon.ParseConfig(configStr)
		m := addon.BuildManifest()

		m.BehaviorHints.ConfigurationRequired = false

		for i, field := range m.Config {
			switch field.Key {
			case "username":
				m.Config[i].Default = config.Username
			case "password":
				// Never decrypt and echo the Easynews password into a manifest.
				// The configured URL is already a bearer secret; returning the
				// plaintext password would unnecessarily widen exposure.
				m.Config[i].Default = ""
			case "strictTitleMatching":
				m.Config[i].Default = config.StrictTitleMatching
			case "enableAltTitles":
				m.Config[i].Default = config.EnableAltTitles
			case "altTitleCountry":
				m.Config[i].Default = config.AltTitleCountry
			case "preferredLanguage":
				m.Config[i].Default = config.PreferredLanguage
			case "sortingPreference":
				m.Config[i].Default = config.SortingPreference
			case "showQualities":
				m.Config[i].Default = config.ShowQualities
			case "maxResultsPerQuality":
				m.Config[i].Default = config.MaxResultsPerQuality
			case "maxFileSize":
				m.Config[i].Default = config.MaxFileSize
			case "uiLanguage":
				if config.UILanguage != "" {
					m.Config[i].Default = config.UILanguage
				}
			}
		}

		c.Header("Cache-Control", "private, no-store")
		c.JSON(http.StatusOK, m)
	})

	streamRouteHandler := func(c *gin.Context, hasConfig bool) {
		contentType := c.Param("type")
		idParam := c.Param("id")
		id := strings.TrimSuffix(idParam, ".json")

		var config addon.AddonConfig
		if hasConfig {
			config = addon.ParseConfig(c.Param("config"))
		} else {
			config = addon.ParseConfig("")
		}

		if config.Username == "" || config.Password == "" {
			serverLogger.Info("Request rejected: missing credentials in configuration path")
			c.JSON(http.StatusOK, gin.H{"streams": []interface{}{}})
			return
		}

		serverLogger.Info("Incoming stream resolution request for type=%s id=%s", contentType, id)

		result, err := addon.StreamHandler(contentType, id, config)
		if err != nil {
			serverLogger.Error("StreamHandler execution failed for type=%s id=%s: %v", contentType, id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve streams"})
			return
		}

		serverLogger.Info("Returned %d stream options for type=%s id=%s (cacheMaxAge=%d staleRevalidate=%d staleError=%d)",
			len(result.Streams), contentType, id, result.CacheMaxAge, result.StaleRevalidate, result.StaleError)

		if cacheControl := streamCacheControlValue(result); cacheControl != "" {
			c.Header("Cache-Control", cacheControl)
		}

		c.JSON(http.StatusOK, result)
	}

	r.GET("/stream/:type/:id", func(c *gin.Context) {
		streamRouteHandler(c, false)
	})

	r.GET("/:config/stream/:type/:id", func(c *gin.Context) {
		streamRouteHandler(c, true)
	})

	// -----------------------------------------------------------------------
	// Secure Resolving Proxy Route
	// -----------------------------------------------------------------------

	r.GET("/resolve/:payload/:filename", resolve.CreateResolveHandler(serverLogger))

	// -----------------------------------------------------------------------
	// Configuration & Dynamic Panel Routes
	// -----------------------------------------------------------------------

	r.GET("/", func(c *gin.Context) {
		lang := c.Query("lang")
		redirectURL := "/configure"
		if lang != "" {
			redirectURL = fmt.Sprintf("/configure?lang=%s", url.QueryEscape(lang))
		}
		c.Redirect(http.StatusFound, redirectURL)
	})

	r.GET("/:config", func(c *gin.Context) {
		configStr := c.Param("config")
		if configStr == "favicon.ico" || configStr == "manifest.json" {
			c.Status(http.StatusNotFound)
			return
		}
		lang := c.Query("lang")
		redirectURL := fmt.Sprintf("/configure?config=%s", url.QueryEscape(configStr))
		if lang != "" {
			redirectURL = fmt.Sprintf("/configure?config=%s&lang=%s", url.QueryEscape(configStr), url.QueryEscape(lang))
		}
		c.Redirect(http.StatusFound, redirectURL)
	})

	r.GET("/configure", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Type", "text/html; charset=utf-8")

		lang := c.Query("lang")
		safeLang := i18n.SanitizeUiLanguage(lang)

		configStr := c.Query("config")
		config := addon.ParseConfig(configStr)

		m := addon.BuildManifest()

		for i, field := range m.Config {
			switch field.Key {
			case "username":
				if config.Username != "" {
					m.Config[i].Default = config.Username
				}
			case "password":
				// Do not render a decrypted password back into HTML. Existing
				// installs must re-enter it when changing configuration.
				m.Config[i].Default = ""
			case "strictTitleMatching":
				if config.StrictTitleMatching != "" {
					m.Config[i].Default = config.StrictTitleMatching
				}
			case "enableAltTitles":
				if config.EnableAltTitles != "" {
					m.Config[i].Default = config.EnableAltTitles
				}
			case "altTitleCountry":
				if config.AltTitleCountry != "" {
					m.Config[i].Default = config.AltTitleCountry
				}
			case "preferredLanguage":
				if config.PreferredLanguage != "" {
					m.Config[i].Default = config.PreferredLanguage
				}
			case "sortingPreference":
				if config.SortingPreference != "" {
					m.Config[i].Default = config.SortingPreference
				}
			case "showQualities":
				if config.ShowQualities != "" {
					m.Config[i].Default = config.ShowQualities
				}
			case "maxResultsPerQuality":
				if config.MaxResultsPerQuality != "" {
					m.Config[i].Default = config.MaxResultsPerQuality
				}
			case "maxFileSize":
				if config.MaxFileSize != "" {
					m.Config[i].Default = config.MaxFileSize
				}
			case "uiLanguage":
				if lang != "" {
					m.Config[i].Default = safeLang
				} else if config.UILanguage != "" {
					m.Config[i].Default = config.UILanguage
				}
			}
		}

		html := addon.RenderConfigurePage(m)
		c.String(http.StatusOK, html)
	})

	// -----------------------------------------------------------------------
	// Boot Configuration Reporting Logging
	// -----------------------------------------------------------------------

	serverLogger.Info("Starting server on port %d", port)
	serverLogger.Info("Addon manifest accessible at: http://127.0.0.1:%d/manifest.json", port)

	serverLogger.Info("--- Active configuration report ---")
	serverLogger.Info("PORT: %d", port)
	serverLogger.Info("LOG_LEVEL: %s", os.Getenv("EASYNEWS_LOG_LEVEL"))
	serverLogger.Info("VERSION: %s", shared.GetVersion())
	serverLogger.Info("--- Advanced Search Options ---")
	serverLogger.Info("TOTAL_MAX_RESULTS: %d", shared.ParseIntEnv("TOTAL_MAX_RESULTS", 500))
	serverLogger.Info("MAX_PAGES: %d", shared.ParseIntEnv("MAX_PAGES", 10))
	serverLogger.Info("MAX_RESULTS_PER_PAGE: %d", shared.ParseIntEnv("MAX_RESULTS_PER_PAGE", 250))
	serverLogger.Info("CACHE_TTL: %d hours", shared.ParseIntEnv("CACHE_TTL", 24))
	serverLogger.Info("MAX_CACHE_ENTRIES: %d", shared.ParseIntEnv("MAX_CACHE_ENTRIES", 1000))
	serverLogger.Info("METADATA_CACHE_ENTRIES: %d", shared.ParseIntEnv("METADATA_CACHE_ENTRIES", 2000))
	serverLogger.Info("CACHE_STATS_LOG_EVERY: %d requests", shared.ParseIntEnv("CACHE_STATS_LOG_EVERY", 100))
	serverLogger.Info("STREMIO_STALE_REVALIDATE_SECONDS: %d", shared.ParseIntEnv("STREMIO_STALE_REVALIDATE_SECONDS", 3600))
	serverLogger.Info("STREMIO_STALE_ERROR_SECONDS: %d", shared.ParseIntEnv("STREMIO_STALE_ERROR_SECONDS", 86400))
	serverLogger.Info("--- Integrations ---")
	if os.Getenv("TMDB_API_KEY") != "" {
		serverLogger.Info("TMDB translations: Enabled")
	} else {
		serverLogger.Info("TMDB translations: Disabled")
	}
	serverLogger.Info("-----------------------------------")

	if err := r.Run(fmt.Sprintf(":%d", port)); err != nil {
		serverLogger.Error("Failed to launch HTTP server: %v", err)
	}
}

// corsMiddleware injects headers allowing seamless multi-origin calls from
// browser/client runtimes, including the configure page's seal request.
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// redactFallbackPath prevents sensitive bearer material from being logged even
// for unmatched/404 requests where Gin has no route template available.
func redactFallbackPath(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}

	if parts[0] == "resolve" && len(parts) >= 2 {
		parts[1] = "<payload>"
		return "/" + strings.Join(parts, "/")
	}

	if len(parts) >= 2 && (parts[1] == "manifest.json" || parts[1] == "stream") {
		parts[0] = "<config>"
		return "/" + strings.Join(parts, "/")
	}

	if len(parts) == 1 && (strings.HasPrefix(parts[0], "enc.") ||
		strings.Contains(parts[0], "username=") ||
		strings.Contains(parts[0], "password=")) {
		return "/<config>"
	}

	return path
}

// requestLogPath uses Gin's route template whenever possible. Templates contain
// parameter names rather than their secret values, so configured addon tokens
// and /resolve payloads never enter the request log.
func requestLogPath(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return redactFallbackPath(c.Request.URL.Path)
}

// requestLogger tracks method, route, status and latency without logging raw
// query strings or secret-bearing route parameters.
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		serverLogger.Info("%s %s | Status: %d | Latency: %v",
			c.Request.Method, requestLogPath(c), c.Writer.Status(), latency)
	}
}
