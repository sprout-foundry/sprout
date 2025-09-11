package types

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

// ToolCall represents a tool call in the response
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool represents a tool definition
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

// Choice represents a response choice
type Choice struct {
	Index   int `json:"index"`
	Message struct {
		Role             string      `json:"role"`
		Content          string      `json:"content"`
		ReasoningContent string      `json:"reasoning_content,omitempty"`
		Images           []ImageData `json:"images,omitempty"`
		ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	Estimated        bool    `json:"estimated,omitempty"`
	PromptTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
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

// ProviderInterface defines the interface that all providers must implement
type ProviderInterface interface {
	SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	CheckConnection() error
	SetDebug(debug bool)
	SetModel(model string) error
	GetModel() string
	GetProvider() string
	GetModelContextLimit() (int, error)
	ListModels() ([]ModelInfo, error)
	SupportsVision() bool
	SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
}

// TokenUsage represents token usage from LLM responses (alias for Usage)
type TokenUsage = Usage

// ModelPricing represents cost per 1K tokens for different models
type ModelPricing struct {
	InputCost     float64 `json:"input_cost"`
	OutputCost    float64 `json:"output_cost"`
	InputCostPer1K  float64 `json:"input_cost_per_1k"`  // Alias for compatibility
	OutputCostPer1K float64 `json:"output_cost_per_1k"` // Alias for compatibility
}

// PricingTable holds per-model pricing that can be loaded from disk
type PricingTable struct {
	Models map[string]ModelPricing `json:"models"`
}

// PatchResolution represents the resolution of a patch/diff
type PatchResolution struct {
	ApprovedChanges []string `json:"approved_changes,omitempty"`
	RejectedChanges []string `json:"rejected_changes,omitempty"`
	Comments        []string `json:"comments,omitempty"`
	Status          string   `json:"status,omitempty"`
	MultiFile       []string `json:"multi_file,omitempty"`
	SingleFile      string   `json:"single_file,omitempty"`
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