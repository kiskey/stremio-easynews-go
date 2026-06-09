package shared

// Version is set at build time via compiler ldflags.
// Default value is "dev".
var Version = "dev"

// GetVersion returns the current version of the application.
func GetVersion() string {
	return Version
}
