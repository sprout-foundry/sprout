package agent

import (
	"fmt"
	"strings"
	"time"
)

// Delegate exit status constants.
const (
	// DelegateExitCompleted indicates the delegate finished its task successfully.
	DelegateExitCompleted = "completed"

	// DelegateExitMaxIterations indicates the delegate ran out of allowed iterations.
	DelegateExitMaxIterations = "max_iterations"

	// DelegateExitError indicates the delegate terminated due to an error.
	DelegateExitError = "error"
)

const (
	// DefaultMaxDelegateIterations is the default maximum number of iterations a delegate agent can run.
	DefaultMaxDelegateIterations = 100

	// MaxDelegateNestingDepth is the maximum allowed nesting depth for delegate calls.
	MaxDelegateNestingDepth = 3
)

// ToolCallRecord tracks a single tool invocation within a delegate session.
type ToolCallRecord struct {
	// ToolName is the name of the tool that was invoked.
	ToolName string `json:"tool_name"`

	// Input is a truncated summary of the tool input.
	Input string `json:"input"`

	// Output is a truncated summary of the tool output.
	Output string `json:"output"`

	// Timestamp is when the tool call was made.
	Timestamp time.Time `json:"timestamp"`

	// Duration is the tool execution time in milliseconds.
	Duration int64 `json:"duration_ms"`

	// Success indicates whether the tool call completed without error.
	Success bool `json:"success"`
}

// DelegateResult is the structured return value from a delegate execution.
type DelegateResult struct {
	// Summary is a high-level description of what the delegate accomplished.
	Summary string `json:"summary"`

	// FilesChanged lists the paths of files modified during the delegate session.
	FilesChanged []string `json:"files_changed"`

	// ToolsCalled records every tool invocation during the session.
	ToolsCalled []ToolCallRecord `json:"tools_called"`

	// TokensUsed is the total number of tokens consumed during the session.
	TokensUsed int `json:"tokens_used"`

	// Cost is the estimated monetary cost of the session.
	Cost float64 `json:"cost"`

	// Iterations is the number of agent iterations the delegate performed.
	Iterations int `json:"iterations"`

	// ExitStatus indicates how the delegate terminated: "completed", "max_iterations", or "error".
	ExitStatus string `json:"exit_status"`

	// ErrorMessage contains the error details if ExitStatus is "error".
	ErrorMessage string `json:"error_message,omitempty"`
}

// DelegateConfig specifies how a delegate agent should be created.
type DelegateConfig struct {
	// Prompt is the task description given to the delegate agent (required).
	Prompt string `json:"prompt"`

	// Role is the persona or role the delegate should assume (e.g., "coder", "debugger").
	Role string `json:"role,omitempty"`

	// Provider is the LLM provider to use (e.g., "openai", "anthropic").
	Provider string `json:"provider,omitempty"`

	// Model is the specific model to use for the delegate.
	Model string `json:"model,omitempty"`

	// Tools is a list of tool names the delegate is allowed to use.
	Tools []string `json:"tools,omitempty"`

	// Context is additional context passed to the delegate (e.g., prior subagent results).
	Context string `json:"context,omitempty"`

	// MaxIterations limits how many times the delegate can iterate (defaults to DefaultMaxDelegateIterations if zero).
	MaxIterations int `json:"max_iterations,omitempty"`

	// Files is a list of file paths the delegate should have access to.
	Files []string `json:"files,omitempty"`

	// FollowUpMessages is a list of follow-up messages to inject into the delegate
	// during execution via the input injection channel. Messages are injected with
	// a small delay between them to avoid overwhelming the channel.
	FollowUpMessages []string `json:"follow_up,omitempty"`
}

// Validate checks that the DelegateConfig has all required fields set properly.
// Returns an error if the prompt is empty or max_iterations is negative.
// If max_iterations is zero, it is set to DefaultMaxDelegateIterations.
func (c *DelegateConfig) Validate() error {
	if strings.TrimSpace(c.Prompt) == "" {
		return fmt.Errorf("delegate config: prompt is required")
	}
	if c.MaxIterations < 0 {
		return fmt.Errorf("delegate config: max_iterations must be non-negative")
	}
	if c.MaxIterations == 0 {
		c.MaxIterations = DefaultMaxDelegateIterations
	}
	return nil
}
