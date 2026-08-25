//go:build !js

package cmd

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

// finalizeAutomateSession writes the terminal session record for a detached
// automate run. The launcher returned immediately after spawning this
// process, so the child is the only writer of the run's end state — without
// it, status can never distinguish a clean finish from a crash. Exit code
// mirrors cobra's process-exit semantics for RunAgent: nil error → 0,
// anything else → 1.
func finalizeAutomateSession(sessionFilePath string, runErr error) {
	if sessionFilePath == "" {
		return
	}
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}
	if err := automate.FinalizeSessionFileByPath(sessionFilePath, exitCode); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}
}
