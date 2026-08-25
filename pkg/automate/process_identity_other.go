//go:build !linux && !js

package automate

import "time"

// processStartedBefore is a stub for non-Linux platforms (macOS, Windows, WASM)
// where reading process start time is platform-specific and not worth the
// complexity. Returns true (fail-open) — legitimate operations are never
// blocked. PID reuse is rare on these desktop/dev environments.
func processStartedBefore(_ int, _ time.Time) bool {
	return true
}
