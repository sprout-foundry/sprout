package types

import "encoding/json"

// ToolCall represents a call to a tool made by the LLM
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction represents the function call details
type ToolCallFunction struct {
	Name       string          `json:"name"`
	Arguments  string          `json:"arguments,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ToolMessage represents a tool call message in the conversation
type ToolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// TokenUsage represents actual token usage from an API response
type TokenUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Estimated        bool `json:"estimated,omitempty"`
}

// TokenUsageInterface defines methods for accessing token usage information
type TokenUsageInterface interface {
	GetTotalTokens() int
	GetPromptTokens() int
	GetCompletionTokens() int
}

// GetTotalTokens implements TokenUsageInterface for TokenUsage
func (t TokenUsage) GetTotalTokens() int {
	return t.TotalTokens
}

// GetPromptTokens implements TokenUsageInterface for TokenUsage
func (t TokenUsage) GetPromptTokens() int {
	return t.PromptTokens
}

// GetCompletionTokens implements TokenUsageInterface for TokenUsage
func (t TokenUsage) GetCompletionTokens() int {
	return t.CompletionTokens
}

// SplitUsage tracks prompt vs completion tokens for a category
type SplitUsage struct {
	Prompt     int
	Completion int
}

// AgentTokenUsage tracks token usage across different agent operations
type AgentTokenUsage struct {
	IntentAnalysis     int
	Planning           int // Tokens used by orchestration model for detailed planning
	CodeGeneration     int
	Validation         int
	ProgressEvaluation int
	Total              int

	// Split accounting for precise input/output costs
	IntentSplit     SplitUsage
	PlanningSplit   SplitUsage
	CodegenSplit    SplitUsage
	ValidationSplit SplitUsage
	ProgressSplit   SplitUsage
}

// GetTotalTokens implements TokenUsageInterface for AgentTokenUsage
func (a AgentTokenUsage) GetTotalTokens() int {
	return a.Total
}

// GetPromptTokens implements TokenUsageInterface for AgentTokenUsage
func (a AgentTokenUsage) GetPromptTokens() int {
	// Sum up all prompt tokens from split usage
	total := a.IntentSplit.Prompt + a.PlanningSplit.Prompt +
		a.CodegenSplit.Prompt + a.ValidationSplit.Prompt + a.ProgressSplit.Prompt
	return total
}

// GetCompletionTokens implements TokenUsageInterface for AgentTokenUsage
func (a AgentTokenUsage) GetCompletionTokens() int {
	// Sum up all completion tokens from split usage
	total := a.IntentSplit.Completion + a.PlanningSplit.Completion +
		a.CodegenSplit.Completion + a.ValidationSplit.Completion + a.ProgressSplit.Completion
	return total
}

// ModelPricing represents cost per 1K tokens for different models
type ModelPricing struct {
	InputCostPer1K  float64 // Cost per 1K input tokens
	OutputCostPer1K float64 // Cost per 1K output tokens
}

// PricingTable holds per-model pricing that can be loaded from disk
type PricingTable struct {
	Models map[string]ModelPricing `json:"models"`
}
