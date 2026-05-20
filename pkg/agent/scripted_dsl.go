package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ScriptedTokenUsage represents token usage metrics for a scripted response
type ScriptedTokenUsage struct {
	PromptTokens        int                 `json:"prompt_tokens"`
	CompletionTokens    int                 `json:"completion_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	EstimatedCost       float64             `json:"estimated_cost"`
	Cost                float64             `json:"cost,omitempty"`
	PromptTokensDetails PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

// PromptTokensDetails contains detailed breakdown of prompt tokens
type PromptTokensDetails struct {
	CachedTokens     int  `json:"cached_tokens"`
	CacheWriteTokens *int `json:"cache_write_tokens"`
}

// ScriptedResponse represents a single scripted response with full configuration options
type ScriptedResponse struct {
	// Message content to return
	Content string

	// Tool calls to include in the response
	ToolCalls []api.ToolCall

	// Finish reason for the choice
	FinishReason string

	// Reasoning content (for models that support it)
	ReasoningContent string

	// Images to include (vision support)
	Images []api.ImageData

	// Delay before returning the response (for rate limit simulation)
	Delay time.Duration

	// Error to return instead of a response
	Error error

	// Rate limit simulation: return rate limit error after N successful responses
	RateLimitAfter int

	// Stream configuration
	StreamConfig *StreamConfig

	// Whether this response should be used for vision requests
	VisionOnly bool

	// Token usage metrics for this response
	Usage ScriptedTokenUsage
}

// StreamConfig configures streaming behavior for a response
type StreamConfig struct {
	// Chunks to stream (content pieces)
	Chunks []string

	// Delay between chunks
	ChunkDelay time.Duration

	// Simulated tokens per chunk
	TokensPerChunk int

	// Error to inject during streaming
	StreamError error

	// Finish reason for the final chunk
	FinishReason string

	// ErrorAfterChunks specifies after how many chunks to fail (0 = never fail)
	ErrorAfterChunks int

	// ChunkErrors allows specifying per-chunk errors (index corresponds to chunk index)
	ChunkErrors []error
}

// ValidateStreamConfig validates a StreamConfig and returns an error if invalid
func ValidateStreamConfig(sc *StreamConfig) error {
	if sc == nil {
		return errors.New("StreamConfig cannot be nil")
	}
	if len(sc.Chunks) == 0 {
		return errors.New("StreamConfig.Chunks must not be empty")
	}
	if sc.ErrorAfterChunks > len(sc.Chunks) {
		return errors.New("ErrorAfterChunks cannot exceed number of chunks")
	}
	return nil
}

// NewToolCallResponse creates a response with tool calls
func NewToolCallResponse(name, args string, toolCalls ...api.ToolCall) *ScriptedResponse {
	if len(toolCalls) == 0 {
		// Create a default tool call if none provided
		toolCalls = []api.ToolCall{
			{
				ID:   fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano()),
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      name,
					Arguments: args,
				},
			},
		}
	}
	return &ScriptedResponse{
		Content:      fmt.Sprintf("Tool call: %s with args: %s", name, args),
		ToolCalls:    toolCalls,
		FinishReason: "tool_calls",
	}
}

// NewStopResponse creates a stop response
func NewStopResponse(content string) *ScriptedResponse {
	return &ScriptedResponse{
		Content:      content,
		FinishReason: "stop",
	}
}

// NewKeepGoingResponse creates a keep-going response (empty finish_reason)
func NewKeepGoingResponse(content string) *ScriptedResponse {
	return &ScriptedResponse{
		Content: content,
		// FinishReason is empty to indicate keep going
	}
}

// NewLengthResponse creates a length finish_reason response
func NewLengthResponse(content string) *ScriptedResponse {
	return &ScriptedResponse{
		Content:      content,
		FinishReason: "length",
	}
}

// NewErrorResponse creates a response that returns an error
func NewErrorResponse(err error) *ScriptedResponse {
	return &ScriptedResponse{
		Error: err,
	}
}

// NewTimeoutResponse creates a response with a timeout error
func NewTimeoutResponse() *ScriptedResponse {
	return &ScriptedResponse{
		Error: context.DeadlineExceeded,
	}
}

// NewRateLimitResponse creates a response that simulates rate limiting
func NewRateLimitResponse() *ScriptedResponse {
	return &ScriptedResponse{
		RateLimitAfter: 1,
	}
}
