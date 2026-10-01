// Package subagents: the process-wide active-subagent counter and the
// terminal display-prefix builder (SP-141 phase 4, increment 1). The
// counter is read by the CLI status footer via GetActiveSubagents; the
// prefix shapes the "[persona]" / "[persona:taskID]" terminal labels.
// pkg/agent forwards both so existing call sites are unchanged.
package subagents

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// buildSubagentPrefix returns the terminal prefix for a subagent based on persona and taskID.
// For single subagents (taskID starting with "subagent-"), returns "[{persona}]".
// For parallel subagents (other taskIDs), returns "[{persona}:{taskID}]".
func BuildSubagentPrefix(persona, taskID string) string {
	if taskID != "" && !strings.HasPrefix(taskID, "subagent-") {
		return fmt.Sprintf("[%s:%s]", persona, taskID)
	}
	return fmt.Sprintf("[%s]", persona)
}

// activeSubagentCount is the process-wide count of currently-running
// subagents. The CLI status footer reads it via GetActiveSubagents()
// to render " · N sub" while delegation is in flight.
var activeSubagentCount atomic.Int64

// IncrementActiveSubagents bumps the active-subagent counter; paired with
// DecrementActiveSubagents under a defer in the spawner.
func IncrementActiveSubagents() { activeSubagentCount.Add(1) }

// DecrementActiveSubagents lowers the active-subagent counter when a
// subagent finishes (success, error, cancel — any terminal state).
func DecrementActiveSubagents() { activeSubagentCount.Add(-1) }

// GetActiveSubagents returns the current number of running subagents.
func GetActiveSubagents() int { return int(activeSubagentCount.Load()) }
