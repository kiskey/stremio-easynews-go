package addon

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
)

const requestCacheVersion = "v26"

// buildRequestCacheKey scopes a cached Stremio stream response to every setting
// that can materially change the returned stream set or stream URLs.
//
// The identity is length-delimited before hashing, so values containing
// punctuation cannot create ambiguous concatenations. Plaintext credentials,
// base URLs and user configuration values are never exposed in the resulting
// map key.
func buildRequestCacheKey(contentType, id, credentialFingerprint string, config AddonConfig) string {
	effectiveBaseURL := strings.TrimSpace(config.BaseUrl)
	if effectiveBaseURL == "" {
		effectiveBaseURL = strings.TrimSpace(os.Getenv("ADDON_BASE_URL"))
	}
	effectiveBaseURL = strings.TrimRight(effectiveBaseURL, "/")

	parts := []string{
		requestCacheVersion,
		contentType,
		id,
		credentialFingerprint,
		effectiveBaseURL,
		config.StrictTitleMatching,
		config.PreferredLanguage,
		config.SortingPreference,
		config.ShowQualities,
		config.MaxResultsPerQuality,
		config.MaxFileSize,
		config.EnableAltTitles,
		config.AltTitleCountry,
	}

	h := sha256.New()
	var size [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(part))
	}

	return requestCacheVersion + ":" + hex.EncodeToString(h.Sum(nil))
}
