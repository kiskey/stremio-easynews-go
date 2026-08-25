package server

import (
	"fmt"
	"os"
	"strings"

	"github.com/kiskey/stremio-easynews-go/internal/seal"
)

const legacyFallbackStatement = "return `${secureHost}/${getLegacyConfigString()}/manifest.json`;"

func legacyPlaintextConfigAllowed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_LEGACY_PLAINTEXT_CONFIG")), "true")
}

func configurationTokenAllowed(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if seal.IsSealed(raw) {
		return true
	}
	return legacyPlaintextConfigAllowed()
}

// hardenConfigureHTML fails closed when plaintext compatibility is disabled.
// The existing frontend is retained, but its two automatic plaintext fallback
// branches are replaced with an explicit user-visible failure. If the template
// changes and the expected statements are no longer present, serving the page
// fails rather than silently re-introducing credential-bearing URLs.
func hardenConfigureHTML(html string) (string, error) {
	if legacyPlaintextConfigAllowed() {
		return html, nil
	}

	count := strings.Count(html, legacyFallbackStatement)
	if count != 2 {
		return "", fmt.Errorf("configure template security guard expected 2 legacy fallbacks, found %d", count)
	}

	replacement := `alert("Secure configuration sealing failed. Plaintext credential URLs are disabled. Ensure ADDON_CONFIG_KEY is configured and reload this page."); throw new Error("secure configuration sealing unavailable");`
	return strings.ReplaceAll(html, legacyFallbackStatement, replacement), nil
}
