package agent

import (
	"encoding/json"
	"fmt"
)

// SubagentStatus enumerates terminal states of a subagent run. Replaces
// the legacy SUBAGENT_SECURITY_ERROR / SUBAGENT_TOKEN_BUDGET_EXCEEDED /
// SUBAGENT_FAILED sentinel string prefixes — those literals are retained
// in the human-readable Output so any LLM behavior keyed on the legacy
// shape still works, but in-process callers should switch to Status.
//
// See SP-059 Phase 2d.
type SubagentStatus string

const (
	SubagentStatusCompleted       SubagentStatus = "completed"
	SubagentStatusCancelled       SubagentStatus = "cancelled"
	SubagentStatusTimedOut        SubagentStatus = "timed_out"
	SubagentStatusBudgetExceeded  SubagentStatus = "budget_exceeded"
	SubagentStatusSecurityBlocked SubagentStatus = "security_blocked"
	SubagentStatusFailed          SubagentStatus = "failed"
)

// FileChange is a single tracked write/edit/delete from a subagent run.
// Sourced from ChangeTracker.GetChanges() (SP-059 Phase 2c) when change
// tracking is enabled; nil when it isn't.
type FileChange struct {
	Path string `json:"path"`
	Op   string `json:"op"` // "created" | "modified" | "deleted"
}

// SubagentRunMetrics is the structured token/cost accounting for a subagent
// run. Sourced directly from SubagentResult (subagent_runner.go), not by
// regex-scraping stdout (SP-059 Phase 2b).
type SubagentRunMetrics struct {
	TokensUsed int     `json:"tokens_used"`
	Cost       float64 `json:"cost"`
	ToolCalls  int     `json:"tool_calls"`
}

// SubagentReturn is the typed envelope a subagent tool call returns to
// the primary's LLM. It marshals to JSON with backward-compatible keys
// for the old map[string]string shape so existing LLM behavior keeps
// working, plus new typed fields (status, files_modified, metrics) for
// callers that want them.
//
// See SP-059 Phase 2a.
type SubagentReturn struct {
	// Output is the subagent's final assistant message (was: "stdout").
	Output string `json:"stdout"`
	// Stderr carries the subagent's terminal error message if any.
	Stderr string `json:"stderr"`
	// ExitCode is "0" on success, "1" otherwise. Kept as string for
	// shape-compat with the legacy resultMap.
	ExitCode string `json:"exit_code"`
	// Completed is "true" on natural completion, "false" if cancelled.
	Completed string `json:"completed"`
	// TimedOut is "true" if the run hit its timeout.
	TimedOut string `json:"timed_out"`
	// BudgetExceeded is "true" if the run hit its token budget.
	BudgetExceeded string `json:"budget_exceeded"`
	// ElapsedSeconds is the wall-clock duration, formatted "%.1f".
	ElapsedSeconds string `json:"elapsed_seconds"`
	// TokensUsed is the rolled-up token count (string for shape-compat).
	TokensUsed string `json:"tokens_used"`
	// Cost is the rolled-up dollar cost (string for shape-compat).
	Cost string `json:"cost"`
	// ToolCallCount is the number of tool calls the subagent made.
	ToolCallCount string `json:"tool_calls"`
	// Summary is JSON-stringified human-readable highlights (file ops,
	// build/test status, errors). Kept for shape-compat.
	Summary string `json:"summary,omitempty"`
	// ContextUsed is "true" / "false" reflecting whether the subagent
	// received the parent's context bundle.
	ContextUsed string `json:"context_used,omitempty"`
	// FilesUsed is the parent-provided files-of-interest list.
	FilesUsed string `json:"files_used,omitempty"`
	// WorkingDir is the directory the subagent executed under.
	WorkingDir string `json:"working_dir,omitempty"`

	// ── New typed fields (SP-059) ──

	// Status is the terminal state. Always populated, even on success.
	Status SubagentStatus `json:"status"`
	// ErrorReason carries free-form context when Status != completed.
	ErrorReason string `json:"error_reason,omitempty"`
	// FilesModified is the change-tracker-sourced manifest. nil when
	// change tracking is disabled (caller treats nil as "not reported").
	FilesModified []FileChange `json:"files_modified,omitempty"`
	// Metrics is the structured token/cost rollup. Mirror of the
	// TokensUsed/Cost/ToolCallCount string fields above for callers
	// that prefer typed access.
	Metrics SubagentRunMetrics `json:"metrics"`
}

// MarshalJSONIndent renders the envelope as a 2-space-indented JSON
// string for the tool result.
func (r *SubagentReturn) MarshalJSONIndent() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SubagentError is an in-process error value carrying both the terminal
// Status and a free-form Reason. Returned alongside the JSON envelope
// when callers in Go-land want to switch on the failure mode without
// re-parsing the result JSON.
type SubagentError struct {
	Status SubagentStatus
	Reason string
}

func (e *SubagentError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason == "" {
		return fmt.Sprintf("subagent %s", e.Status)
	}
	return fmt.Sprintf("subagent %s: %s", e.Status, e.Reason)
}
