package version

// This will be overridden during build when using ldflags
var Version = "v0.1.1"

func GetVersion() string {
	return Version
}
