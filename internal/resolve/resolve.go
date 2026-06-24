package resolve

import (
    "encoding/base64"
    "net/http"
    "net/url"
    "regexp"
    "strings"
    "sync"
    "time"

    "github.com/kiskey/stremio-easynews-go/internal/shared"
)

var (
    easynewsHostRe = regexp.MustCompile(`(?i)^([a-z0-9-]+\.)*easynews\.com$`)

    resolvedUrlCache      = make(map[string]*resolvedEntry)
    resolvedUrlCacheMu    sync.RWMutex
    resolvedUrlTTL        = time.Duration(shared.ParseIntEnv("RESOLVE_CACHE_TTL_SECONDS", 300)) * time.Second
    resolvedUrlMaxEntries = 5000
)

type resolvedEntry struct {
    url     string
    expires int64 // UnixNano
}

type ResolvedTarget struct {
    CleanUrl   string
    AuthHeader string
}

type ResolveError struct {
    Status  int
    Message string
}

func (e *ResolveError) Error() string { return e.Message }

// IsAllowedEasynewsHost checks if the target hostname resolves to a valid sub-domain of easynews.com.
func IsAllowedEasynewsHost(host string) bool {
    return easynewsHostRe.MatchString(host)
}

// ParseResolvePayload decodes and validates the Base64URL-encoded resolve proxy parameters,
// returning the clean stream URL along with the formatted Authorization header.
func ParseResolvePayload(payloadBase64url string) (ResolvedTarget, error) {
    if payloadBase64url == "" {
        return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Missing url parameter"}
    }

    targetUrlBytes, err := decodeBase64(payloadBase64url)
    if err != nil {
        return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Invalid url encoding"}
    }

    parsed, err := url.Parse(string(targetUrlBytes))
    if err != nil {
        return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Invalid url"}
    }

    if parsed.Scheme != "https" {
        return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Only HTTPS URLs are permitted"}
    }

    if !IsAllowedEasynewsHost(parsed.Hostname()) {
        return ResolvedTarget{}, &ResolveError{Status: 403, Message: "Domain not allowed"}
    }

    username := parsed.Query().Get("u")
    password := parsed.Query().Get("p")

    // Strip credentials from the URL so they are not leaked inside location redirects or headers
    query := parsed.Query()
    query.Del("u")
    query.Del("p")
    parsed.RawQuery = query.Encode()

    authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

    return ResolvedTarget{
        CleanUrl:   parsed.String(),
        AuthHeader: authHeader,
    }, nil
}

// decodeBase64 decodes standard and raw URL-safe or standard Base64 payloads with maximum resiliency.
func decodeBase64(input string) ([]byte, error) {
    if data, err := base64.URLEncoding.DecodeString(input); err == nil {
        return data, nil
    }
    if data, err := base64.RawURLEncoding.DecodeString(input); err == nil {
        return data, nil
    }
    if data, err := base64.StdEncoding.DecodeString(input); err == nil {
        return data, nil
    }
    return base64.RawStdEncoding.DecodeString(input)
}

// GetCachedResolvedUrl checks for a cached CDN location matching the current resolution payload.
func GetCachedResolvedUrl(payload string) (string, bool) {
    resolvedUrlCacheMu.RLock()
    entry, ok := resolvedUrlCache[payload]
    resolvedUrlCacheMu.RUnlock()
    if !ok {
        return "", false
    }
    if time.Now().UnixNano() > entry.expires {
        resolvedUrlCacheMu.Lock()
        // Double-Checked Locking: Verify again under write lock to protect against race deletion of newly written fresh items
        entryCheck, okCheck := resolvedUrlCache[payload]
        if okCheck && time.Now().UnixNano() > entryCheck.expires {
            delete(resolvedUrlCache, payload)
        }
        resolvedUrlCacheMu.Unlock()
        return "", false
    }
    return entry.url, true
}

// SetCachedResolvedUrl caches a resolved CDN location against its Base64 payload.
func SetCachedResolvedUrl(payload, url string) {
    resolvedUrlCacheMu.Lock()
    defer resolvedUrlCacheMu.Unlock()

    resolvedUrlCache[payload] = &resolvedEntry{
        url:     url,
        expires: time.Now().Add(resolvedUrlTTL).UnixNano(),
    }

    if len(resolvedUrlCache) > resolvedUrlMaxEntries {
        toDelete := len(resolvedUrlCache) / 2
        for k := range resolvedUrlCache {
            if toDelete <= 0 {
                break
            }
            delete(resolvedUrlCache, k)
            toDelete--
        }
    }
}

// ClearResolvedUrlCache flushes the CDN resolution cache (used primarily for test isolation).
func ClearResolvedUrlCache() {
    resolvedUrlCacheMu.Lock()
    resolvedUrlCache = make(map[string]*resolvedEntry)
    resolvedUrlCacheMu.Unlock()
}

// StripAuthOnForeignHost returns an HTTP client CheckRedirect policy hook which drops the
// Authorization credentials header if the client is redirected to a non-matching destination host.
func StripAuthOnForeignHost(originalHost string) func(req *http.Request, via []*http.Request) error {
    original := strings.ToLower(originalHost)
    return func(req *http.Request, via []*http.Request) error {
        targetHost := strings.ToLower(req.URL.Hostname())
        if targetHost != "" && targetHost != original {
            req.Header.Del("Authorization")
            req.Header.Del("authorization")
        }
        return nil
    }
}
