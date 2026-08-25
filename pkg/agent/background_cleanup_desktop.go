//go:build !js

package agent

import (
	"fmt"
	"os"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// cleanupOrphanedBackgroundProcesses kills any background processes left behind
// by a previous unclean exit (kill, segfault, etc.). Called once per process
// from agent_creation.go behind backgroundOrphanCleanupOnce.
func cleanupOrphanedBackgroundProcesses(debug bool) {
	baseDir := tools.GetBackgroundOutputBaseDir()
	if err := tools.CleanupOrphanedBackgroundProcesses(baseDir); err != nil && debug {
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("WARNING: Failed to clean up orphaned background processes: %v\n", err)))
	}
}
