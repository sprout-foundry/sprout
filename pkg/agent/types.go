package agent

import (
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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

// AgentState represents the state of an agent that can be persisted
type AgentState struct {
	Messages        []api.Message `json:"messages"`
	PreviousSummary string        `json:"previous_summary"`
	CompactSummary  string        `json:"compact_summary"` // New: 5K limit summary for continuity
	TaskActions     []TaskAction  `json:"task_actions"`
	SessionID       string        `json:"session_id"`
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
