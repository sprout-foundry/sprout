//go:build unix && !js

package automate

import "github.com/sprout-foundry/sprout/pkg/utils/pidalive"

// IsProcessAlive (Unix) — see pidalive_windows.go for the rationale and
// SP-112-3 for the deduplication.
func IsProcessAlive(pid int) bool {
	return pidalive.IsAlive(pid)
}
