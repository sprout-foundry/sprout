//go:build !js

package cmd

import "time"

// recordProcessStartTime returns the OS-reported creation time for the given
// PID, or time.Now() as a best-effort fallback on platforms where reading it
// is not implemented. The returned value is stored alongside the PID so that
// loadDesiredWebUIHostPID can detect PID reuse.
func recordProcessStartTime(pid int) time.Time {
	if t, ok := tryReadProcStartTime(pid); ok {
		return t
	}
	return time.Now()
}
