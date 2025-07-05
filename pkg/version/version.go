package version

var (
	// Version is set via build flags
	Version string = "dev"
	// BuildDate represents the build timestamp
	BuildDate string = "unknown"
)

// Get returns the formatted version string
func Get() string {
	return Version + " (" + BuildDate + ")"
}
