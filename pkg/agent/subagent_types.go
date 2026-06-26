package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
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
// regex-scraping stdout. Iterations is the assistant-turn count, exposed
// so the primary's LLM can reason about how much budget a delegated task
// burned.
type SubagentRunMetrics struct {
	TokensUsed int     `json:"tokens_used"`
	Cost       float64 `json:"cost"`
	ToolCalls  int     `json:"tool_calls"`
	Iterations int     `json:"iterations"`
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
	// ProgressLog is a capped timeline of subagent activity events
	// (spawn / output / complete) so the primary's LLM can reason about
	// what the subagent actually did, not just its final assistant
	// message. nil when no events were captured. SP-059 Phase 3a.
	ProgressLog []ProgressEntry `json:"progress_log,omitempty"`
}

// ProgressEntry is the envelope-facing form of SubagentProgressEntry,
// kept separately from the runner-internal type so the runner struct
// can change without affecting the wire shape.
type ProgressEntry struct {
	OffsetMS int64  `json:"offset_ms"`
	Phase    string `json:"phase"`
	Message  string `json:"message"`
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

// ── Runner types (moved from subagent_runner.go) ──

// SubagentOptions configures an in-process subagent
type SubagentOptions struct {
	Persona                string        // "coder", "tester", "debugger", etc.
	Model                  string        // optional model override
	Provider               string        // optional provider override
	SystemPrompt           string        // optional system prompt override
	MaxTokens              int           // token budget (0 = unlimited)
	Timeout                time.Duration // execution timeout (0 = unlimited)
	WorkingDir             string        // optional: override workspace root (must be within $HOME)
	MaxConcurrentSubagents int           // max parallel subagents (0 = unlimited, default unlimited)
	FleetTokenBudget       int           // shared token budget across all parallel subagents (0 = unlimited)
}

// SharedState holds resources shared between parent and subagents
type SharedState struct {
	EventBus      *events.EventBus
	TodoManager   *tools.TodoManager
	EmbeddingMgr  *embedding.EmbeddingManager
	ConfigManager *configuration.Manager
	WorkspaceRoot string
}

// SubagentResult is the structured output from a subagent
type SubagentResult struct {
	ID         string
	Output     string
	Error      error
	TokensUsed int
	Cost       float64
	ToolCalls  int
	// Iterations is the assistant-turn count consumed by this subagent
	// run. Surfaced to the primary via SubagentRunMetrics.Iterations so
	// the model has visibility into how many LLM rounds a delegated task
	// burned. SP-059 Phase 5.
	Iterations     int
	Elapsed        time.Duration
	Cancelled      bool
	BudgetExceeded bool // true if task was skipped because fleet budget was already exceeded before starting
	Truncated      bool // true if subagent was cut short due to fleet budget exceeded mid-run
	// FileChanges is the manifest of writes/edits this subagent performed,
	// captured via its own ChangeTracker. nil when tracking wasn't
	// initialized for this run. SP-059 Phase 2c.
	FileChanges []TrackedFileChange
	// ProgressLog is a per-run timeline of notable subagent events
	// (spawn, output, complete). Surfaced to the primary's LLM via the
	// SubagentReturn envelope so the model can reason about *what* the
	// subagent did, not just the final assistant message. Capped to
	// subagentProgressLogCap entries. SP-059 Phase 3a.
	ProgressLog []SubagentProgressEntry
}

// SubagentProgressEntry is one timeline entry from a subagent run. Kept
// minimal to avoid bloating the envelope the primary's LLM sees.
type SubagentProgressEntry struct {
	OffsetMS int64  `json:"offset_ms"` // ms since subagent started
	Phase    string `json:"phase"`     // "spawn" | "output" | "complete"
	Message  string `json:"message"`
}

// subagentProgressLogCap bounds the per-run progress log. Beyond this,
// the buffer becomes head-trimmed (oldest entries dropped) so the LLM
// always sees the most recent activity.
const subagentProgressLogCap = 50

// SubagentTask represents a single parallel subagent task
type SubagentTask struct {
	ID         string
	Prompt     string
	Model      string
	Provider   string
	Persona    string
	WorkingDir string // optional: override workspace root
}

// SubagentMetrics tracks operational metrics for the subagent runner.
type SubagentMetrics struct {
	Active            int64 // Currently executing subagents
	Queued            int64 // Waiting for semaphore slot
	Completed         int64 // Successfully completed
	Failed            int64 // Completed with error
	Cancelled         int64 // Cancelled (parent ctx or budget)
	TotalQueuedWaitMS int64 // Cumulative milliseconds spent waiting in queue
}

// SubagentRunner manages in-process subagent execution
type SubagentRunner struct {
	parentAgent *Agent
	shared      *SharedState
	active      sync.Map // taskID -> *runningSubagent

	// Operational metrics (atomic for concurrent access)
	metricActive       atomic.Int64
	metricQueued       atomic.Int64
	metricCompleted    atomic.Int64
	metricFailed       atomic.Int64
	metricCancelled    atomic.Int64
	metricQueuedWaitMS atomic.Int64

	// testClientFactory overrides client creation for testing only.
	// When non-nil, it is called instead of factory.CreateProviderClient.
	// This field is never set in production code.
	testClientFactory func(clientType agent_api.ClientType, model string) (agent_api.ClientInterface, error)
}

// runningSubagent tracks an active subagent execution
type runningSubagent struct {
	ID        string
	Persona   string
	Prompt    string
	StartedAt time.Time
	Agent     *Agent
	Ctx       context.Context
	Cancel    context.CancelFunc
	Completed atomic.Bool
}
