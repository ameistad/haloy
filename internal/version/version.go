package version

// Version is the current version of Haloy
// This will be overridden during build when using ldflags
var Version = "v0.1.0"

// GetVersion returns the current version string
func GetVersion() string {
	return Version
}
