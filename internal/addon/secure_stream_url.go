package addon

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/kiskey/stremio-easynews-go/internal/resolve"
	"github.com/kiskey/stremio-easynews-go/internal/seal"
)

// SecureResolverConfigError indicates that stream URLs cannot be generated
// safely with the current server configuration.
type SecureResolverConfigError struct {
	msg string
}

func (e *SecureResolverConfigError) Error() string { return e.msg }

func isTrueEnv(name string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true")
}

func legacyResolveURL(fullURL, username, password, effectiveBaseURL, filename string) string {
	authURL := fmt.Sprintf("%s?u=%s&p=%s", fullURL, url.QueryEscape(username), url.QueryEscape(password))
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(authURL))
	return fmt.Sprintf("%s/resolve/%s/%s", strings.TrimRight(effectiveBaseURL, "/"), encodedURL, filename)
}

// CreateSecureStreamURL is the Batch-8 stream URL generator. The default path
// uses an AES-GCM scope-separated resolver token; reversible Base64 payloads or
// direct credential URLs require explicit legacy opt-in.
func CreateSecureStreamURL(downURL string, dlFarm string, dlPort int, username, password, filePath, baseURL string) (string, error) {
	effectiveBaseURL := strings.TrimSpace(baseURL)
	if effectiveBaseURL == "" {
		effectiveBaseURL = strings.TrimSpace(os.Getenv("ADDON_BASE_URL"))
	}

	sanitizedFilePath := strings.ReplaceAll(filePath, " ", "%20")
	fullURL := fmt.Sprintf("%s/%s/%d/%s", downURL, dlFarm, dlPort, sanitizedFilePath)
	filename := path.Base(filePath)

	if effectiveBaseURL != "" && !seal.Enabled() {
		_ = seal.Init()
	}

	if effectiveBaseURL != "" && seal.Enabled() {
		payload, err := resolve.SealResolveTarget(fullURL, username, password)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s/resolve/%s/%s", strings.TrimRight(effectiveBaseURL, "/"), payload, filename), nil
	}

	if effectiveBaseURL != "" && resolve.LegacyResolvePayloadsAllowed() {
		return legacyResolveURL(fullURL, username, password, effectiveBaseURL, filename), nil
	}

	if effectiveBaseURL == "" && isTrueEnv("ALLOW_INSECURE_CREDENTIAL_URLS") {
		return fmt.Sprintf("%s/%s/%d/%s",
			strings.Replace(downURL, "https://", fmt.Sprintf("https://%s:%s@", username, password), 1),
			dlFarm, dlPort, sanitizedFilePath), nil
	}

	if effectiveBaseURL == "" {
		return "", &MissingBaseUrlError{
			msg: "createSecureStreamURL: ADDON_BASE_URL is required",
		}
	}
	return "", &SecureResolverConfigError{
		msg: "createSecureStreamURL: ADDON_CONFIG_KEY is required for encrypted resolver payloads; set ALLOW_LEGACY_RESOLVE_PAYLOADS=true only for temporary migration compatibility",
	}
}

func isStreamURLConfigurationError(err error) bool {
	if err == nil {
		return false
	}
	switch err.(type) {
	case *MissingBaseUrlError, *SecureResolverConfigError:
		return true
	default:
		return false
	}
}
