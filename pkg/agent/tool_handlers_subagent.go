// Subagent tool handlers: constants, types, globals, and shared declarations.
//
// Implementation details are split across:
//   - tool_handlers_subagent_events.go  — event batching and publishing
//   - tool_handlers_subagent_result.go — typed result envelope builders
//   - tool_handlers_subagent_spawn.go  — spawn / dispatch logic
package agent

import (
	"sync"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE  = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE = 1024 * 1024      // 1MB
	// Lines to batch before publishing a subagent "output" event. Kept small
	// so output streams to the WebUI in near-real-time — subagent output is
	// line-level (LLM-paced), not char-level, so this won't flood the event
	// bus, while still coalescing bursty tool dumps. (Was 50, which made most
	// subagent runs show nothing until they finished.)
	BATCH_SIZE                 = 8
	DefaultSubagentTokenBudget = 2_000_000 // Default token budget for subagents
)

// MILESTONE_PHASES defines phases that trigger immediate publish without batching
var MILESTONE_PHASES = []string{"spawn", "complete", "step"}

// subagentBatchBuffer holds buffered output for a subagent task
type subagentBatchBuffer struct {
	lines      []string
	lineCount  int
	taskID     string
	persona    string
	isParallel bool
}

// Global batch buffer manager
var (
	batchBuffers = make(map[string]*subagentBatchBuffer)
	bufferMu     sync.Mutex
)
