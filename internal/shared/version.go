package shared

import (
	"regexp"
	"strings"
)

// Version is set at build time via compiler ldflags.
var Version = "2.8.6"

// Precompiled regex matching standard SemVer pattern (starts with a digit, followed by dot-separated digits)
var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// GetVersion returns the compiled application version.
// Stremio strictly rejects manifests that do not match the Semantic Versioning spec (e.g. "prod", "dev", or hex hashes).
// This accessor automatically sanitizes the string, strips leading 'v' prefixes,
// and falls back safely to the hardcoded "2.8.6" SemVer if the build-injected string is malformed.
func GetVersion() string {
	v := strings.TrimPrefix(Version, "v")
	if semverRe.MatchString(v) {
		return v
	}
	return "2.8.6" // Ultimate fallback safety boundary
}
