// Package agent: subagents forwarders (SP-141 phase 4, increment 1).
// The subagent data types and their pure helpers (display, preamble,
// lifecycle counter, prefix builder) live in pkg/agent/subagents; these
// aliases and one-line forwarders keep the existing pkg/agent call sites
// (the tool handlers, the task runner, workflow wiring) unchanged. The
// SubagentRunner and its methods stay here until the runner seam.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
)

// Type aliases — the canonical definitions are in pkg/agent/subagents.
type (
	SubagentStatus        = subagents.SubagentStatus
	FileChange            = subagents.FileChange
	SubagentRunMetrics    = subagents.SubagentRunMetrics
	SubagentReturn        = subagents.SubagentReturn
	ProgressEntry         = subagents.ProgressEntry
	SubagentError         = subagents.SubagentError
	SubagentOptions       = subagents.SubagentOptions
	SharedState           = subagents.SharedState
	SubagentResult        = subagents.SubagentResult
	SubagentProgressEntry = subagents.SubagentProgressEntry
	SubagentTask          = subagents.SubagentTask
	SubagentMetrics       = subagents.SubagentMetrics
)

const (
	SubagentStatusCompleted       = subagents.SubagentStatusCompleted
	SubagentStatusCancelled       = subagents.SubagentStatusCancelled
	SubagentStatusTimedOut        = subagents.SubagentStatusTimedOut
	SubagentStatusBudgetExceeded  = subagents.SubagentStatusBudgetExceeded
	SubagentStatusSecurityBlocked = subagents.SubagentStatusSecurityBlocked
	SubagentStatusFailed          = subagents.SubagentStatusFailed
)

// ProgressLogCap bounds the per-run progress log (implementation in
// pkg/agent/subagents).
const ProgressLogCap = subagents.ProgressLogCap

// The original lowercase const name is kept for the existing call sites in
// subagent_task.go.
const subagentProgressLogCap = subagents.ProgressLogCap

// isOutputComplete reports whether a subagent result carries substantive
// output (implementation in pkg/agent/subagents).
func isOutputComplete(r *SubagentResult) bool {
	return subagents.IsOutputComplete(r)
}

// buildSubagentPrefix returns the terminal prefix for a subagent
// (implementation in pkg/agent/subagents).
func buildSubagentPrefix(persona, taskID string) string {
	return subagents.BuildSubagentPrefix(persona, taskID)
}

// IncrementActiveSubagents bumps the process-wide active-subagent counter
// (implementation in pkg/agent/subagents).
func IncrementActiveSubagents() { subagents.IncrementActiveSubagents() }

// DecrementActiveSubagents lowers the active-subagent counter when a
// subagent finishes (implementation in pkg/agent/subagents).
func DecrementActiveSubagents() { subagents.DecrementActiveSubagents() }

// GetActiveSubagents returns the number of running subagents
// (implementation in pkg/agent/subagents).
func GetActiveSubagents() int { return subagents.GetActiveSubagents() }

// appendSubagentPreamble appends the shared subagent operating rules to a
// persona system prompt (implementation in pkg/agent/subagents).
func appendSubagentPreamble(prompt string) string {
	return subagents.AppendSubagentPreamble(prompt)
}

// printSubagentStart announces a delegated subagent run
// (implementation in pkg/agent/subagents).
func printSubagentStart(persona, provider, model string) {
	subagents.PrintSubagentStart(persona, provider, model)
}

// printParallelSubagentStart announces a parallel subagent fan-out
// (implementation in pkg/agent/subagents).
func printParallelSubagentStart(count int, provider, model string) {
	subagents.PrintParallelSubagentStart(count, provider, model)
}

// printSubagentDone prints the subagent completion line with stats
// (implementation in pkg/agent/subagents).
func printSubagentDone(persona string, res *SubagentResult) {
	subagents.PrintSubagentDone(persona, res)
}
