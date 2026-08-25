package resolve

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"github.com/kiskey/stremio-easynews-go/internal/seal"
)

const resolverSealScope = "easynews-resolver"

type sealedResolvePayload struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// LegacyResolvePayloadsAllowed enables migration compatibility for pre-Batch-8
// Base64 resolver URLs. It is deliberately opt-in: new deployments reject
// reversible credential payloads by default.
func LegacyResolvePayloadsAllowed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_LEGACY_RESOLVE_PAYLOADS")), "true")
}

func validateEasynewsURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, &ResolveError{Status: 400, Message: "Invalid url"}
	}
	if parsed.Scheme != "https" {
		return nil, &ResolveError{Status: 400, Message: "Only HTTPS URLs are permitted"}
	}
	if !IsAllowedEasynewsHost(parsed.Hostname()) {
		return nil, &ResolveError{Status: 403, Message: "Domain not allowed"}
	}
	if parsed.User != nil {
		return nil, &ResolveError{Status: 400, Message: "Embedded URL credentials are not permitted"}
	}
	return parsed, nil
}

func resolvedTargetFromParts(rawURL, username, password string) (ResolvedTarget, error) {
	parsed, err := validateEasynewsURL(rawURL)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if username == "" || password == "" {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Missing resolver credentials"}
	}
	return ResolvedTarget{
		CleanUrl:   parsed.String(),
		AuthHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
	}, nil
}

// SealResolveTarget returns an authenticated opaque resolver token. Credentials
// are encrypted inside AES-256-GCM ciphertext and never appear in the URL.
func SealResolveTarget(rawURL, username, password string) (string, error) {
	if _, err := resolvedTargetFromParts(rawURL, username, password); err != nil {
		return "", err
	}
	body, err := json.Marshal(sealedResolvePayload{
		URL:      rawURL,
		Username: username,
		Password: password,
	})
	if err != nil {
		return "", err
	}
	return seal.SealScoped(resolverSealScope, body)
}

func parseScopedResolvePayload(payload string) (ResolvedTarget, error) {
	body, err := seal.OpenScoped(resolverSealScope, payload)
	if err != nil {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Invalid secure resolver payload"}
	}
	var decoded sealedResolvePayload
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Invalid secure resolver payload"}
	}
	return resolvedTargetFromParts(decoded.URL, decoded.Username, decoded.Password)
}

func parseLegacyResolvePayload(payload string) (ResolvedTarget, error) {
	targetURLBytes, err := decodeBase64(payload)
	if err != nil {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Invalid legacy resolver payload"}
	}
	parsed, err := url.Parse(string(targetURLBytes))
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
	query := parsed.Query()
	query.Del("u")
	query.Del("p")
	parsed.RawQuery = query.Encode()

	return resolvedTargetFromParts(parsed.String(), username, password)
}

// ParseSecureResolvePayload accepts the Batch-8 scope-separated encrypted
// resolver format. Old Base64 resolver URLs are accepted only when explicitly
// enabled with ALLOW_LEGACY_RESOLVE_PAYLOADS=true.
func ParseSecureResolvePayload(payload string) (ResolvedTarget, error) {
	if payload == "" {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Missing resolver payload"}
	}
	if seal.IsScoped(payload) {
		return parseScopedResolvePayload(payload)
	}
	if !LegacyResolvePayloadsAllowed() {
		return ResolvedTarget{}, &ResolveError{Status: 400, Message: "Legacy resolver payloads are disabled"}
	}
	return parseLegacyResolvePayload(payload)
}
