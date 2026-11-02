package agent

// subagent_concurrent_output.go — how a subagent's run reaches the
// terminal when it runs alongside others (SP-174a/c).
//
// A foreground subagent is the only producer while the parent waits on it,
// so its prose streams and its tool lines print as they happen. A parallel
// or background run is not: several of them, plus the primary, would
// interleave line by line. Those runs stay off the transcript while they
// work — the CLI subscriber folds their tool activity into one fleet status
// on the spinner — and each one writes a single block when it finishes.

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Event-metadata keys every subagent's events carry.
const (
	// EventMetaSubagentTaskID names the run, so same-persona workers can be
	// told apart.
	EventMetaSubagentTaskID = "subagent_task_id"
	// EventMetaSubagentConcurrent is true for parallel and background runs,
	// whose tool activity the CLI summarizes instead of printing.
	EventMetaSubagentConcurrent = "subagent_concurrent"
)

// concurrentTailLines is how many lines of a concurrent run's final output
// its completion block shows. The full output is the parent's tool result.
const concurrentTailLines = 6

// isConcurrentSubagentRun reports whether a run shares the terminal with
// other producers. Foreground runs use "subagent-<nanos>" ids; parallel runs
// use the caller's task ids and background runs "bg-…".
func isConcurrentSubagentRun(taskID string) bool {
	return !strings.HasPrefix(taskID, "subagent-")
}

// tagSubagentEvents marks every event the subagent publishes with its run.
func tagSubagentEvents(sub *Agent, taskID string) {
	sub.MergeEventMetadata(map[string]interface{}{
		EventMetaSubagentTaskID:     taskID,
		EventMetaSubagentConcurrent: isConcurrentSubagentRun(taskID),
	})
}

// announceSubagentFinished closes a run: it publishes the per-run lifecycle
// status (the CLI drops the run from its fleet status; the WebUI counts it),
// and for a visible concurrent run prints the run's completion block in one
// write so it cannot interleave with its siblings.
func (r *SubagentRunner) announceSubagentFinished(taskID string, opts SubagentOptions, res *SubagentResult) {
	if res == nil {
		return
	}
	if r.shared != nil && r.shared.EventBus != nil {
		status := "completed"
		if res.Cancelled {
			status = "cancelled"
		}
		data := map[string]interface{}{
			"task_id":     taskID,
			"persona":     opts.Persona,
			"status":      status,
			"message":     subagentOutcomeMessage(res),
			"tokens_used": res.TokensUsed,
			"cost":        res.Cost,
			"elapsed_ms":  res.Elapsed.Milliseconds(),
		}
		if res.Error != nil {
			data["failures"] = 1
		}
		r.shared.EventBus.Publish(events.EventTypeSubagentActivity, data)
	}
	if isConcurrentSubagentRun(taskID) && !opts.Quiet {
		console.PrintLine(formatConcurrentSubagentBlock(opts.Persona, taskID, res))
	}
}

func subagentOutcomeMessage(res *SubagentResult) string {
	switch {
	case res.Cancelled:
		return "Subagent cancelled"
	case res.Error != nil:
		return "Subagent failed: " + res.Error.Error()
	default:
		return "Subagent completed"
	}
}

// formatConcurrentSubagentBlock renders a finished concurrent run: its done
// line, then the last few lines of what it reported, indented beneath.
func formatConcurrentSubagentBlock(persona, taskID string, res *SubagentResult) string {
	var b strings.Builder
	b.WriteString(subagents.FormatSubagentDone(persona+":"+taskID, res))
	body := strings.TrimSpace(res.Output)
	if res.Error != nil && body == "" {
		return b.String()
	}
	lines := strings.Split(body, "\n")
	if len(lines) > concurrentTailLines {
		fmt.Fprintf(&b, "\n    … %d earlier lines in the result", len(lines)-concurrentTailLines)
		lines = lines[len(lines)-concurrentTailLines:]
	}
	for _, line := range lines {
		b.WriteString("\n    " + strings.TrimRight(line, " \t"))
	}
	return b.String()
}
