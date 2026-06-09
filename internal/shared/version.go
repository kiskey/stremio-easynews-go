package shared

// Version is set at build time via compiler ldflags.
// Injected at compile time using -ldflags "-X github.com/kiskey/stremio-easynews-go/internal/shared.Version=2.8.2"
var Version = "2.8.2"

// GetVersion returns the current version of the application.
func GetVersion() string {
	return Version
}
