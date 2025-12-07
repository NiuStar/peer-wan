package version

import (
	"time"
)

// Build holds the build identifier, injected via -ldflags. Default "dev".
var Build = "dev"

func init() {
	if Build == "" || Build == "dev" {
		// fallback to current time in UTC+8
		loc, _ := time.LoadLocation("Asia/Shanghai")
		Build = time.Now().In(loc).Format("2006-01-02-15-04")
	}
}

// BuildCN returns Build tagged as UTC+8 when using YYYY-MM-DD-HH-MM derived from UTC.
func BuildCN() string {
	return Build + " (UTC+8)"
}
