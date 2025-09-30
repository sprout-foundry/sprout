package types

import (
	"encoding/json"
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ImageData represents an image in a message
type ImageData struct {
	URL    string `json:"url,omitempty"`    // URL to image
	Base64 string `json:"base64,omitempty"` // Base64 encoded image data
	Type   string `json:"type,omitempty"`   // MIME type (image/jpeg, image/png, etc.)
}

// Message represents a chat message
type Message struct {
	Role             string      `json:"role"`
	Content          string      `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	Images           []ImageData `json:"images,omitempty"` // Support for multiple images
}

// Tool and ToolCall types moved to pkg/agent_api for consolidation
// Import from there: github.com/alantheprice/ledit/pkg/agent_api

// Choice represents a response choice
type Choice struct {
	Index   int `json:"index"`
	Message struct {
		Role             string         `json:"role"`
		Content          string         `json:"content"`
		ReasoningContent string         `json:"reasoning_content,omitempty"`
		Images           []ImageData    `json:"images,omitempty"`
		ToolCalls        []api.ToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	EstimatedCost       float64 `json:"estimated_cost"`
	Estimated           bool    `json:"estimated,omitempty"`
	PromptTokensDetails struct {
		CachedTokens     int  `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// ChatResponse represents a chat API response
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// ModelInfo represents information about a model
type ModelInfo struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Provider      string  `json:"provider"`
	Description   string  `json:"description,omitempty"`
	ContextLength int     `json:"context_length,omitempty"`
	InputCost     float64 `json:"input_cost,omitempty"`
	OutputCost    float64 `json:"output_cost,omitempty"`
	Cost          float64 `json:"cost,omitempty"`
}

// TokenUsage represents token usage from LLM responses (alias for Usage)
type TokenUsage = Usage

// ModelPricing represents cost per 1K tokens for different models
type ModelPricing struct {
	InputCost       float64 `json:"input_cost"`
	OutputCost      float64 `json:"output_cost"`
	InputCostPer1K  float64 `json:"input_cost_per_1k"`  // Alias for compatibility
	OutputCostPer1K float64 `json:"output_cost_per_1k"` // Alias for compatibility
}

// PricingTable holds per-model pricing that can be loaded from disk
type PricingTable struct {
	Models map[string]ModelPricing `json:"models"`
}

// PatchResolution represents the resolution of a patch/diff
type PatchResolution struct {
	ApprovedChanges []string          `json:"approved_changes,omitempty"`
	RejectedChanges []string          `json:"rejected_changes,omitempty"`
	Comments        []string          `json:"comments,omitempty"`
	Status          string            `json:"status,omitempty"`
	SingleFile      string            `json:"SingleFile"`
	MultiFile       map[string]string `json:"MultiFile"`
}

// IsEmpty checks if the patch resolution is empty
func (pr *PatchResolution) IsEmpty() bool {
	if pr == nil {
		return true
	}
	return len(pr.ApprovedChanges) == 0 && len(pr.RejectedChanges) == 0 &&
		len(pr.Comments) == 0 && pr.Status == "" &&
		len(pr.MultiFile) == 0 && pr.SingleFile == ""
}

// UnmarshalJSON implements custom JSON unmarshaling for PatchResolution
// It can handle both string format (single file) and object format (multi-file)
func (pr *PatchResolution) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first (single file format)
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		pr.SingleFile = str
		pr.MultiFile = nil
		return nil
	}

	// Try to unmarshal as map[string]string (simple multi-file format)
	var multiFile map[string]string
	if err := json.Unmarshal(data, &multiFile); err == nil {
		pr.MultiFile = multiFile
		pr.SingleFile = ""
		return nil
	}

	// Try to unmarshal as full object
	type PatchResolutionAlias PatchResolution // Avoid infinite recursion
	var alias PatchResolutionAlias
	if err := json.Unmarshal(data, &alias); err == nil {
		*pr = PatchResolution(alias)
		return nil
	}

	return fmt.Errorf("cannot unmarshal PatchResolution from %s", string(data))
}

// MarshalJSON implements custom JSON marshaling for PatchResolution
func (pr *PatchResolution) MarshalJSON() ([]byte, error) {
	// Always marshal as full object to match test expectations
	type PatchResolutionAlias PatchResolution // Avoid infinite recursion
	return json.Marshal((*PatchResolutionAlias)(pr))
}

// CodeReviewResult represents the result of a code review
type CodeReviewResult struct {
	Issues           []string         `json:"issues,omitempty"`
	Suggestions      []string         `json:"suggestions,omitempty"`
	Approved         bool             `json:"approved"`
	Status           string           `json:"status,omitempty"`
	Feedback         string           `json:"feedback,omitempty"`
	DetailedGuidance string           `json:"detailed_guidance,omitempty"`
	NewPrompt        string           `json:"new_prompt,omitempty"`
	PatchResolution  *PatchResolution `json:"patch_resolution,omitempty"`
}

// ToolCallFunction represents the function part of a tool call
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentTokenUsage represents token usage from agent operations
type AgentTokenUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	Model            string  `json:"model,omitempty"`
	Provider         string  `json:"provider,omitempty"`
}

// IntentAnalysis represents the analysis of user intent
type IntentAnalysis struct {
	Intent       string            `json:"intent"`
	Actions      []string          `json:"actions"`
	Files        []string          `json:"files"`
	Confidence   float64           `json:"confidence"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

// EditPlan represents a plan for code edits
type EditPlan struct {
	Target      string   `json:"target"`
	Changes     []string `json:"changes"`
	Rationale   string   `json:"rationale"`
	Files       []string `json:"files"`
	TestChanges bool     `json:"test_changes"`
}

// AgentContext represents context for agent operations
type AgentContext struct {
	WorkspaceRoot string            `json:"workspace_root"`
	CurrentFiles  []string          `json:"current_files"`
	GitStatus     string            `json:"git_status,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}
