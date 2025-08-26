package types

import (
	"encoding/json"
	"time"
)

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

// Message represents a conversation message with role and content
type Message struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
	Images    []ImageData `json:"images,omitempty"`
}

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

// ImageData represents image content in a message
type ImageData struct {
	Type string `json:"type"`
	Data string `json:"data"` // base64 encoded image data
}

// ModelInfo contains information about an LLM model
type ModelInfo struct {
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	MaxTokens      int    `json:"max_tokens"`
	SupportsTools  bool   `json:"supports_tools"`
	SupportsImages bool   `json:"supports_images"`
}

// WorkspaceContext represents the context of a workspace
type WorkspaceContext struct {
	Files     []FileInfo     `json:"files"`
	Summary   string         `json:"summary"`
	Language  string         `json:"language"`
	Framework string         `json:"framework"`
	Metadata  map[string]any `json:"metadata"`
}

// FileInfo represents information about a file in the workspace
type FileInfo struct {
	Path     string    `json:"path"`
	Content  string    `json:"content,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Language string    `json:"language"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
}

// ChangeSet represents a set of changes to be applied
type ChangeSet struct {
	ID          string            `json:"id"`
	Files       map[string]string `json:"files"` // filepath -> new content
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Timestamp   time.Time         `json:"timestamp"`
}

// RequestOptions contains options for LLM requests
type RequestOptions struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// ResponseMetadata contains metadata about an LLM response
type ResponseMetadata struct {
	Model      string        `json:"model"`
	TokenUsage TokenUsage    `json:"token_usage"`
	Duration   time.Duration `json:"duration"`
	Provider   string        `json:"provider"`
	Cost       float64       `json:"cost,omitempty"`
}
