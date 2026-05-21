// Tool executor: core struct, constructor, and orchestration entry point.
//
// Companions (all in this package):
//   Execution:    tool_executor_sequential.go, tool_executor_parallel.go
//   Config:       tool_executor_config.go
//   Context:      tool_execution_context.go, tool_executor_helpers.go
//   Safety:       tool_executor_circuit_breaker.go
//   Observability: tool_executor_trace.go
//   Formatting:   tool_call_format.go
//   Constraint:   tool_result_constraint.go
//   Todo events:  tool_executor_todo_events.go
//   JSON repair:  tool_json_repair.go
package agent

import (
	"sync"
	"sync/atomic"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ToolExecutor handles tool execution logic
type ToolExecutor struct {
	agent           *Agent
	handlerRegistry atomic.Pointer[tools.ToolRegistry] // SP-038: new handler registry (nil = backward compat)
	toolIndex       int                                 // Counter for tool execution order within each turn
	idCounter       int64                               // Atomic counter for unique tool call ID generation
	idCounterMu     sync.Mutex
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(agent *Agent) *ToolExecutor {
	return &ToolExecutor{
		agent: agent,
	}
}

// SetHandlerRegistry sets the new tool handler registry for dual dispatch.
// When set, the executor checks this registry first before falling back to
// the legacy dispatch path. This enables gradual migration of tools.
// Thread-safe: may be called concurrently with tool execution.
func (te *ToolExecutor) SetHandlerRegistry(r *tools.ToolRegistry) {
	te.handlerRegistry.Store(r)
}

// getHandlerRegistry returns the current handler registry (nil if not set).
func (te *ToolExecutor) getHandlerRegistry() *tools.ToolRegistry {
	return te.handlerRegistry.Load()
}

// ExecuteTools executes a list of tool calls and returns the results
func (te *ToolExecutor) ExecuteTools(toolCalls []api.ToolCall) []api.Message {
	// Reset tool index counter at the start of each tool execution batch
	te.toolIndex = 0

	// Log tool calls at the beginning of the process
	if te.agent != nil {
		te.agent.debugLog("[tool] Executing %d tool calls\n", len(toolCalls))
		for i, tc := range toolCalls {
			te.agent.LogToolCall(tc, "executing")

			// Extract persona and subagent info from subagent arguments
			args, _, _ := parseToolArgumentsWithRepair(tc.Function.Arguments)
			persona := ""
			isSubagent := isSubagentTool(tc.Function.Name)
			subagentType := "single"
			if isSubagent {
				if p, ok := args["persona"].(string); ok {
					persona = p
				}
				if tc.Function.Name == "run_parallel_subagents" {
					subagentType = "parallel"
				}
			}
			displayName := formatToolCall(tc)
			te.agent.PublishToolStart(
				tc.Function.Name, tc.ID, tc.Function.Arguments,
				displayName, persona, isSubagent, subagentType, i,
			)
		}
	}

	// Check for interrupt before executing
	select {
	case <-te.agent.interruptCtx.Done():
		// Context cancelled, interrupt requested
		var results []api.Message
		for _, tc := range toolCalls {
			toolCallID := tc.ID
			if toolCallID == "" {
				toolCallID = te.GenerateToolCallID(tc.Function.Name)
			}
			results = append(results, api.Message{
				Role:       "tool",
				Content:    "Execution interrupted by user",
				ToolCallID: toolCallID,
			})
		}
		return results
	default:
		// Context not cancelled
	}

	// Optimize parallel execution for independent, side-effect-free batched tools.
	if te.canExecuteInParallel(toolCalls) {
		return te.executeParallel(toolCalls)
	}

	// Sequential execution for other tools
	return te.executeSequential(toolCalls)
}
