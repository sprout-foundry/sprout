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

// PatchResolution can be either a string (single file) or a map (multi-file patches)
type PatchResolution struct {
	// Single file patch (backward compatibility)
	SingleFile string
	// Multi-file patches
	MultiFile map[string]string
}

// IsEmpty returns true if no patch resolution is provided
func (p *PatchResolution) IsEmpty() bool {
	return p.SingleFile == "" && len(p.MultiFile) == 0
}

// GetFiles returns a map of all files and their contents
func (p *PatchResolution) GetFiles() map[string]string {
	if p.MultiFile != nil {
		return p.MultiFile
	}
	if p.SingleFile != "" {
		// For single file patches, we don't have a filename, so return empty map
		// This might need to be enhanced based on how single file patches are used
		return make(map[string]string)
	}
	return make(map[string]string)
}

// UnmarshalJSON implements custom JSON unmarshaling for PatchResolution
func (p *PatchResolution) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as a string (backward compatibility)
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		p.SingleFile = str
		return nil
	}

	// If that fails, try to unmarshal as a map
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		p.MultiFile = m
		return nil
	}

	// If both fail, return the original error
	return json.Unmarshal(data, &str)
}

// MarshalJSON implements custom JSON marshaling for PatchResolution
func (p *PatchResolution) MarshalJSON() ([]byte, error) {
	// If MultiFile is set, marshal it as an object
	if len(p.MultiFile) > 0 {
		return json.Marshal((map[string]string)(p.MultiFile))
	}
	// Otherwise, marshal SingleFile as a string
	return json.Marshal(p.SingleFile)
}

// CodeReviewResult represents the result of an automated code review.
type CodeReviewResult struct {
	Status           string           `json:"status"`                      // "approved", "needs_revision", "rejected"
	Feedback         string           `json:"feedback"`                    // Explanation for the status
	DetailedGuidance string           `json:"detailed_guidance,omitempty"` // Detailed guidance for LLM if status is "needs_revision"
	PatchResolution  *PatchResolution `json:"patch_resolution,omitempty"`  // Complete updated file content if direct patch is provided
	NewPrompt        string           `json:"new_prompt,omitempty"`        // New prompt suggestion if status is "rejected"
}
