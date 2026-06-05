//go:build js

package automate

import "fmt"

// SweepStaleSessions is a no-op for JS/WASM builds.
func SweepStaleSessions(sproutDir string) (int, error) {
	return 0, fmt.Errorf("stale session sweep is not supported on JS/WASM platforms")
}
