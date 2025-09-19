package api

// Common types used across all providers

type ImageData struct {
	URL    string `json:"url,omitempty"`    // URL to image
	Base64 string `json:"base64,omitempty"` // Base64 encoded image data
	Type   string `json:"type,omitempty"`   // MIME type (image/jpeg, image/png, etc.)
}

type Message struct {
	Role             string      `json:"role"`
	Content          string      `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	Images           []ImageData `json:"images,omitempty"` // Support for multiple images
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

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

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type ChatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
	Stream     bool      `json:"stream,omitempty"`
}
