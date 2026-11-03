package agent

// subagent_task_finalize.go — closing a subagent run: metrics, progress log,
// change and shell-history snapshots, output-quality signals, and the run's
// announcement. Split out of subagent_task.go. Pure move.

import (
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// finalizeSubagentResult enriches the raw SubagentResult with metrics,
// progress log, change tracker snapshot, and output-quality signals.
func (r *SubagentRunner) finalizeSubagentResult(
	rc *subagentRunContext,
	result *SubagentResult,
	subAgent *Agent,
	taskID string,
	startTime time.Time,
	opts SubagentOptions,
) *SubagentResult {
	// Flush any remaining buffered output. Use TryLock to avoid deadlock
	// if the goroutine leaked and still holds outputMu.
	if rc.outputMu.TryLock() {
		if rc.lineBuf.Len() > 0 {
			remaining := strings.TrimSpace(rc.lineBuf.String())
			if remaining != "" && !opts.Quiet && !isConcurrentSubagentRun(taskID) {
				console.PrintLine(rc.dimGray + rc.prefix + rc.reset + " " + remaining)
			}
			rc.lineBuf.Reset()
		}
		rc.outputMu.Unlock()
	}

	// Mark as completed
	rc.running.Completed.Store(true)

	// Collect metrics from agent state
	tokensUsed := subAgent.state.GetTotalTokens()
	cost := subAgent.state.GetTotalCost()
	toolCalls := subAgent.state.GetTotalToolCalls()
	iterations := subAgent.state.GetCurrentIteration()

	// Determine cancellation status
	cancelled := rc.runCtx.Err() != nil && !rc.budgetExceeded.Load()

	// Merge metrics into result
	if result != nil {
		result.ID = taskID
		result.TokensUsed = tokensUsed
		result.Cost = cost
		// Carry the subagent's role and its
		// prompt/completion token split so the parent can roll the usage up
		// under the correct role with real token counts, not a zero-split
		// aggregate stamped with the parent's role.
		result.Role = subAgent.GetRole()
		result.PromptTokens = subAgent.state.GetPromptTokens()
		result.CompletionTokens = subAgent.state.GetCompletionTokens()
		result.ToolCalls = toolCalls
		result.Iterations = iterations
		result.Cancelled = cancelled
		result.BudgetExceeded = rc.budgetExceeded.Load()
		result.Truncated = subAgent.FleetBudgetExceeded()
		// Snapshot the subagent's change tracker for the parent.
		if tracker := subAgent.GetChangeTracker(); tracker != nil {
			result.FileChanges = tracker.GetChanges()
		}
		// Snapshot the subagent's shell-command history so a worker result
		// can be verified against the test commands it actually ran.
		if hist := subAgent.GetAllShellCommandHistory(); len(hist) > 0 {
			records := make(map[string]subagents.ShellCommandRecord, len(hist))
			for command, entry := range hist {
				if entry == nil {
					continue
				}
				records[command] = subagents.ShellCommandRecord{Command: command, Failed: entry.Error != nil}
			}
			result.ShellHistory = records
		}
		// Copy the captured progress log into the result.
		rc.progressMu.Lock()
		if len(*rc.progressLog) > 0 {
			result.ProgressLog = make([]SubagentProgressEntry, len(*rc.progressLog))
			copy(result.ProgressLog, *rc.progressLog)
		}
		rc.progressMu.Unlock()

		// Output quality signal: set OutputComplete so the orchestrator can
		// distinguish "subagent did useful work" from "produced nothing actionable".
		result.OutputComplete = isOutputComplete(result)

		// Diagnostic: warn when a subagent exits cleanly but produces brief output.
		if result.Error == nil && !result.Cancelled && !result.BudgetExceeded && !result.Truncated {
			trimmed := strings.TrimSpace(result.Output)
			if len(trimmed) < 50 {
				preview := trimmed
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				preview = strings.Map(func(runeVal rune) rune {
					if runeVal == '\n' || runeVal == '\r' {
						return ' '
					}
					return runeVal
				}, preview)
				if r.parentAgent != nil {
					r.parentAgent.Logger().Warn(
						"[subagent] %s task=%s completed with insufficient output: "+
							"len=%d iters=%d tool_calls=%d tokens=%d preview=%q",
						opts.Persona, taskID,
						len(trimmed), iterations, toolCalls, tokensUsed, preview,
					)
				}
			}
		}
	}

	if result != nil {
		logSubagentRun(subagentRunRecord(opts.Persona, result, subAgent.maxIterations,
			subAgent.state.GetLastRunTerminationReason() == RunTerminationMaxIterations,
			rc.wrapUpInjected.Load()))
	}

	r.announceSubagentFinished(taskID, opts, result)

	// Clean up tracking
	r.active.Delete(taskID)

	return result
}
