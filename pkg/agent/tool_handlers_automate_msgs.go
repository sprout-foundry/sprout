package agent

import (
	"fmt"
	"strings"
)

// completionMessageTailLimit is the maximum number of output bytes included
// in an automate completion injection message when the workflow fails.
const completionMessageTailLimit = 2048

// buildAutomateCompletionMessage builds the self-contained completion injection
// message for an automate workflow that has finished. It is extracted from the
// proc.Done() goroutine in handleRunAutomate so it can be unit-tested without
// spinning up real background processes.
func buildAutomateCompletionMessage(wfName, wfDesc, sessionID, status string, exitCode int, outputPath string) string {
	// On failure, include the output tail for diagnostics.
	if exitCode != 0 {
		tail := readOutputTail(outputPath, completionMessageTailLimit)
		if tail != "" {
			return fmt.Sprintf(
				"[automate] Background workflow completed:\n"+
					"  Workflow: %s\n"+
					"  Description: %s\n"+
					"  Session: %s\n"+
					"  Status: %s (exit code %d)\n"+
					"  Output (last 2KB):\n%s",
				wfName, wfDesc, sessionID, status, exitCode, tail,
			)
		}
	}
	return fmt.Sprintf(
		"[automate] Background workflow completed:\n"+
			"  Workflow: %s\n"+
			"  Description: %s\n"+
			"  Session: %s\n"+
			"  Status: %s (exit code %d)",
		wfName, wfDesc, sessionID, status, exitCode,
	)
}

// buildInProcessCompletionMessage builds the completion message for the
// in-process workflow runner. It includes the item counts from the result
// and any error information.
func buildInProcessCompletionMessage(wfName, wfDesc, sessionID, status string, result *WorkflowResult) string {
	if result == nil {
		return fmt.Sprintf(
			"[automate] In-process workflow completed:\n"+
				"  Workflow: %s\n"+
				"  Description: %s\n"+
				"  Session: %s\n"+
				"  Status: %s",
			wfName, wfDesc, sessionID, status,
		)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"[automate] In-process workflow completed:\n"+
			"  Workflow: %s\n"+
			"  Description: %s\n"+
			"  Session: %s\n"+
			"  Status: %s\n"+
			"  Items: %d processed, %d skipped, %d failed",
		wfName, wfDesc, sessionID, status,
		result.ItemsProcessed, result.ItemsSkipped, result.ItemsFailed,
	))

	if result.Error != nil {
		b.WriteString(fmt.Sprintf("\n  Error: %s", result.Error.Error()))
	}

	return b.String()
}