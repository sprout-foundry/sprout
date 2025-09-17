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
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	EstimatedCost       float64 `json:"estimated_cost"`
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

// StreamCallback is called for each content chunk received during streaming
type StreamCallback func(content string)

// ProviderInterface defines the interface that all providers must implement
type ProviderInterface interface {
	SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error)
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
