package version

// Build holds the build identifier, injected via -ldflags. Default "dev".
var Build = "dev"

// BuildCN returns Build tagged as UTC+8 when using YYYY-MM-DD-HH-MM derived from UTC.
func BuildCN() string {
	return Build + " (UTC+8)"
}
