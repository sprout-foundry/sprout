//go:build !js

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/workflow"
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

// recordContinuationStopReason annotates the current run's session record
// with the continuation loop's stop reason. It is best-effort: a foreground
// run has no session file, and the stop reason is metadata for post-mortems,
// not a reason to fail the run.
func recordContinuationStopReason(result workflow.ContinuationResult) {
	path := strings.TrimSpace(agentAutomateRecordFile)
	if path == "" {
		path = strings.TrimSpace(agentAutomateSessionFile)
	}
	if path == "" {
		return
	}
	if err := automate.RecordSessionStopReason(
		path,
		string(result.StopReason),
		result.Continuations,
		result.RunnableItems,
	); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}
}
