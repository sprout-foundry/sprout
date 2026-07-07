package agent

import (
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TaskAction represents a completed action during task execution
type TaskAction struct {
	Type        string // "file_created", "file_modified", "command_executed", "file_read"
	Description string // Human-readable description
	Details     string // Additional details like file path, command, etc.
}

// ShellCommandResult tracks shell command execution for deduplication
type ShellCommandResult struct {
	Command         string // The command that was run
	FullOutput      string // Complete output (for future reference)
	TruncatedOutput string // Truncated output (what was shown)
	Error           error  // Any error that occurred
	ExecutedAt      int64  // Unix timestamp
	MessageIndex    int    // Index in messages array where this result appears
	WasTruncated    bool   // Whether output was truncated
	FullOutputPath  string // Optional path to the saved full output
	TruncatedTokens int    // Number of tokens omitted from the middle section
	TruncatedLines  int    // Approximate number of lines omitted from the middle
}

// TurnCheckpoint stores a compact summary for a completed user turn while
// preserving the original full messages for cache-efficient reuse until needed.
//
// SP-066 Phase 2 generalizes this struct: a Level=0 entry is a per-turn
// checkpoint (the historical default). A Level>0 entry is a "rollup" that
// folds many lower-level checkpoints into one coarser summary. Both kinds
// substitute identically through seed's BuildCheckpointCompactedMessages —
// the rollup is just a checkpoint whose StartIndex/EndIndex span a wider
// historical range. The extra fields are sprout-side metadata for the
// rollup worker and the WebUI; seed doesn't read them.
type TurnCheckpoint struct {
	StartIndex        int    `json:"start_index"`
	EndIndex          int    `json:"end_index"`
	Summary           string `json:"summary"`
	ActionableSummary string `json:"actionable_summary,omitempty"`
	// FileChanges is the git-style manifest (M/A/D/R) of files touched
	// during this turn. Populated from the agent's ChangeTracker at
	// checkpoint-record time. Empty when tracking is disabled or the turn
	// didn't write any files. For rollups (Level>0), this is the union of
	// the source checkpoints' file changes so the manifest doesn't get
	// lost as rollups stack.
	FileChanges []CheckpointFileChange `json:"file_changes,omitempty"`
	// RevisionID is the ChangeTracker revision that was active when this
	// turn ran. When set, the summary text references it so the model can
	// call the view_history tool to recover the exact diff. Empty when
	// tracking is disabled. For rollups, this is the most recent
	// revision_id from the source set.
	RevisionID string `json:"revision_id,omitempty"`

	// SP-066 Phase 2 — rollup metadata. Absent on legacy/per-turn
	// checkpoints; populated by the rollup worker.

	// ID is a stable identifier for this checkpoint, independent of its
	// position in the TurnCheckpoints slice. Used by rollups to reference
	// their source checkpoints via SourceCheckpointIDs.
	ID string `json:"id,omitempty"`
	// Level is the rollup depth. 0 = per-turn (existing behavior).
	// 1 = rollup of per-turn checkpoints. 2 = rollup of rollups. Etc.
	Level int `json:"level,omitempty"`
	// CoveredTurns is the count of original per-turn checkpoints this
	// entry effectively replaces. For Level=0 this is 1 (or omitted).
	// For rollups this is the sum of CoveredTurns from the source set.
	CoveredTurns int `json:"covered_turns,omitempty"`
	// SourceCheckpointIDs lists the checkpoint IDs this rollup consumed.
	// Lets the UI drill down and lets a re-roll-up operate on the right
	// source. Empty for Level=0.
	SourceCheckpointIDs []string `json:"source_checkpoint_ids,omitempty"`
}

// CheckpointFileChange is a single file-change entry in a TurnCheckpoint's
// manifest. Op is one of "A" (added), "M" (modified), "D" (deleted), "R"
// (renamed) to mirror git's status codes; anything else is "?" (other).
type CheckpointFileChange struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// AgentState represents the state of an agent that can be persisted
type AgentState struct {
	Messages          []api.Message    `json:"messages"`
	MessageTimestamps []time.Time      `json:"message_timestamps,omitempty"`
	TurnCheckpoints   []TurnCheckpoint `json:"turn_checkpoints,omitempty"`
	PreviousSummary   string           `json:"previous_summary"`
	CompactSummary    string           `json:"compact_summary"` // New: 5K limit summary for continuity
	TaskActions       []TaskAction     `json:"task_actions"`
	SessionID         string           `json:"session_id"`
	// Token and cost metrics
	TotalTokens             int     `json:"total_tokens"`
	TotalCost               float64 `json:"total_cost"`
	PromptTokens            int     `json:"prompt_tokens"`
	CompletionTokens        int     `json:"completion_tokens"`
	EstimatedTokenResponses int     `json:"estimated_token_responses"`
	CachedTokens            int     `json:"cached_tokens"`
	CacheWriteTokens        int     `json:"cache_write_tokens,omitempty"`
	CachedCostSavings       float64 `json:"cached_cost_savings"`
	// Billing-model-aware cost tracking (SP-080)
	ChargedCostTotal   float64 `json:"charged_cost_total,omitempty"`
	TokenCostTotal     float64 `json:"token_cost_total,omitempty"`
	SubscriptionTokens int     `json:"subscription_tokens,omitempty"`
	FreeTokens         int     `json:"free_tokens,omitempty"`
}

// DiffChange represents a change region in the diff
type DiffChange struct {
	OldStart  int
	OldLength int
	NewStart  int
	NewLength int
}

// CircuitBreakerAction tracks repetitive actions for circuit breaker logic
type CircuitBreakerAction struct {
	ActionType string // "edit_file", "shell_command", etc.
	Target     string // file path, command, etc.
	Count      int    // number of times this action was performed
	LastUsed   int64  // unix timestamp of last use
}

// CircuitBreakerState tracks repetitive actions across the session.
//
// Locking Strategy:
//   - The Actions map is protected by mu (sync.RWMutex)
//   - Use RLock/RLock for read-only access when you don't need exclusive access
//   - Use Lock for write operations or when you need exclusive access
//   - Always use defer to unlock (defer mu.Unlock() or defer mu.RUnlock())
//   - Helper functions ending with "Locked" must be called while holding the lock
//     (they perform no locking themselves, allowing callers to hold lock for multiple ops)
//
// Example patterns:
//
//	// Read-only access:
//	cb.mu.RLock()
//	defer cb.mu.RUnlock()
//	action := cb.Actions[key]
//
//	// Write access:
//	cb.mu.Lock()
//	defer cb.mu.Unlock()
//	cb.Actions[key] = &CircuitBreakerAction{...}
type CircuitBreakerState struct {
	mu      sync.RWMutex
	Actions map[string]*CircuitBreakerAction // key: actionType:target
}
