//go:build !consul

package agent

import "context"

// WatchEnabled returns false when consul build tag is not present.
func WatchEnabled() bool { return false }

// StartConsulWatch is a no-op without consul tag.
func StartConsulWatch(_ context.Context, _ string, _ string, _ func(int64)) error { return nil }
