package server

import (
    "fmt"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/gin-contrib/gzip"
    "github.com/kiskey/stremio-easynews-go/internal/addon"
    "github.com/kiskey/stremio-easynews-go/internal/i18n"
    "github.com/kiskey/stremio-easynews-go/internal/resolve"
    "github.com/kiskey/stremio-easynews-go/internal/shared"
)

var serverLogger = shared.CreateLogger("Server", "")

// ServeHTTP initializes and boots the high-performance HTTP gateway.
func ServeHTTP(port int) {
    if port == 0 {
        port = shared.ParseIntEnv("PORT", 1337)
    }

    // Set Gin mode to Release in production to suppress debugging output allocations
    if os.Getenv("EASYNEWS_LOG_LEVEL") != "debug" && os.Getenv("EASYNEWS_LOG_LEVEL") != "silly" {
        gin.SetMode(gin.ReleaseMode)
    }

    r := gin.New()
    r.Use(gin.Recovery())                       // Protects the master daemon from crashing on runtime exceptions
    r.Use(gzip.Gzip(gzip.DefaultCompression)) // Global Gzip compression [4]
    r.Use(corsMiddleware())
    r.Use(requestLogger())

    // Cache-Control headers configuration
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

    // Default unconfigured manifest endpoint (forces setup workflow)
    r.GET("/manifest.json", func(c *gin.Context) {
        m := addon.BuildManifest()
        c.JSON(http.StatusOK, m)
    })

    // User-configured manifest endpoint (disables setup requirements for direct installation)
    r.GET("/:config/manifest.json", func(c *gin.Context) {
        configStr := c.Param("config")
        config := addon.ParseConfig(configStr)
        m := addon.BuildManifest()

        // Disable installation blocks because configuration is now provided in the path!
        m.BehaviorHints.ConfigurationRequired = false

        // Propagate user configuration back to defaults in Stremio UI
        for i, field := range m.Config {
            switch field.Key {
            case "username":
                m.Config[i].Default = config.Username
            case "password":
                m.Config[i].Default = config.Password
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

        c.JSON(http.StatusOK, m)
    })

    // Stream extraction endpoint parser
    streamRouteHandler := func(c *gin.Context, hasConfig bool) {
        contentType := c.Param("type")
        idParam := c.Param("id")

        // Resiliency: Strip trailing .json if Stremio appended it to the ID
        id := strings.TrimSuffix(idParam, ".json")

        var config addon.AddonConfig
        if hasConfig {
            configStr := c.Param("config")
            config = addon.ParseConfig(configStr)
        } else {
            config = addon.ParseConfig("")
        }

        if config.Username == "" || config.Password == "" {
            serverLogger.Info("Request rejected: missing credentials (username/password) in configuration path")
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

        serverLogger.Info("Returned %d stream options for type=%s id=%s (cacheMaxAge=%d)", len(result.Streams), contentType, id, result.CacheMaxAge)

        if result.CacheMaxAge > 0 {
            c.Header("Cache-Control", fmt.Sprintf("max-age=%d, public", result.CacheMaxAge))
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

    // Match and redirect configured root URLs back to pre-populated configure panel
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

        // Populate active settings if configuring from an existing install URL
        for i, field := range m.Config {
            switch field.Key {
            case "username":
                if config.Username != "" {
                    m.Config[i].Default = config.Username
                }
            case "password":
                if config.Password != "" {
                    m.Config[i].Default = config.Password
                }
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

// corsMiddleware injects headers allowing seamless multi-origin calls from various browser/client player runtimes.
func corsMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("Access-Control-Allow-Origin", "*")
        c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
        c.Header("Access-Control-Allow-Headers", "*")
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(http.StatusNoContent)
            return
        }
        c.Next()
    }
}

// requestLogger tracks network metrics, errors, and latencies across endpoints.
// Escalated to INFO level to guarantee observability under the default log settings.
func requestLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        path := c.Request.URL.Path
        raw := c.Request.URL.RawQuery
        if raw != "" {
            path = path + "?" + raw
        }
        c.Next()
        latency := time.Since(start)
        serverLogger.Info("%s %s | Status: %d | Latency: %v", c.Request.Method, path, c.Writer.Status(), latency)
    }
}
