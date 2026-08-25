package resolve

import (
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
)

var easynewsHostRe = regexp.MustCompile(`(?i)^([a-z0-9-]+\.)*easynews\.com$`)

type ResolvedTarget struct {
	CleanUrl   string
	AuthHeader string
}

type ResolveError struct {
	Status  int
	Message string
}

func (e *ResolveError) Error() string { return e.Message }

// IsAllowedEasynewsHost restricts resolver origins to Easynews itself. CDN
// destinations are learned only from Easynews redirect responses.
func IsAllowedEasynewsHost(host string) bool {
	return easynewsHostRe.MatchString(host)
}

// ParseResolvePayload is retained as a compatibility entry point for callers.
// Batch 8 routes it through the secure parser; reversible Base64 payloads are
// accepted only when ALLOW_LEGACY_RESOLVE_PAYLOADS=true.
func ParseResolvePayload(payload string) (ResolvedTarget, error) {
	return ParseSecureResolvePayload(payload)
}

// decodeBase64 exists only for explicitly enabled migration compatibility with
// pre-Batch-8 resolver URLs.
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

// Compatibility wrappers retain the previous cache API while using the shared
// bounded TTL/LRU implementation introduced by Batch 8.
func GetCachedResolvedUrl(payload string) (string, bool) {
	return GetSecureCachedResolvedURL(payload)
}

func SetCachedResolvedUrl(payload, targetURL string) {
	SetSecureCachedResolvedURL(payload, targetURL)
}

func ClearResolvedUrlCache() {
	ClearSecureResolvedURLCache()
}

// StripAuthOnForeignHost drops Authorization if a caller elects to follow an
// upstream redirect to a different hostname.
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
