//go:build !linux && !js

package cmd

import "time"

// tryReadProcStartTime is a stub for non-Linux platforms (macOS, Windows)
// where /proc is unavailable. Always returns false so recordProcessStartTime
// falls back to time.Now().
func tryReadProcStartTime(_ int) (time.Time, bool) {
	return time.Time{}, false
}
