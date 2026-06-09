package shared

// Version is set at build time via compiler ldflags.
// Bumped to v2.8.0 to support Dynamic TMDB Alternative Title Resolution.
var Version = "2.8.0"

// GetVersion returns the current version of the application.
func GetVersion() string {
	return Version
}
