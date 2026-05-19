package agent

import (
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ScriptedResponseBuilder provides a fluent interface for building ScriptedResponse
type ScriptedResponseBuilder struct {
	response *ScriptedResponse
}

// NewScriptedResponseBuilder creates a new response builder
func NewScriptedResponseBuilder() *ScriptedResponseBuilder {
	return &ScriptedResponseBuilder{
		response: &ScriptedResponse{},
	}
}

// Content sets the message content
func (b *ScriptedResponseBuilder) Content(content string) *ScriptedResponseBuilder {
	b.response.Content = content
	return b
}

// ToolCall adds a single tool call
func (b *ScriptedResponseBuilder) ToolCall(tc api.ToolCall) *ScriptedResponseBuilder {
	b.response.ToolCalls = append(b.response.ToolCalls, tc)
	return b
}

// ToolCalls sets multiple tool calls
func (b *ScriptedResponseBuilder) ToolCalls(tcs []api.ToolCall) *ScriptedResponseBuilder {
	b.response.ToolCalls = tcs
	return b
}

// FinishReason sets the finish reason
func (b *ScriptedResponseBuilder) FinishReason(reason string) *ScriptedResponseBuilder {
	b.response.FinishReason = reason
	return b
}

// Images sets the images for vision support
func (b *ScriptedResponseBuilder) Images(images []api.ImageData) *ScriptedResponseBuilder {
	b.response.Images = images
	return b
}

// Delay sets the delay before returning the response
func (b *ScriptedResponseBuilder) Delay(d time.Duration) *ScriptedResponseBuilder {
	b.response.Delay = d
	return b
}

// Error sets an error to be returned instead of a response
func (b *ScriptedResponseBuilder) Error(err error) *ScriptedResponseBuilder {
	b.response.Error = err
	return b
}

// RateLimitAfter configures rate limit simulation
func (b *ScriptedResponseBuilder) RateLimitAfter(n int) *ScriptedResponseBuilder {
	b.response.RateLimitAfter = n
	return b
}

// ReasoningContent sets reasoning content for models that support it
func (b *ScriptedResponseBuilder) ReasoningContent(content string) *ScriptedResponseBuilder {
	b.response.ReasoningContent = content
	return b
}

// Usage sets the token usage metrics for this response
func (b *ScriptedResponseBuilder) Usage(promptTokens, completionTokens, totalTokens int, estimatedCost float64) *ScriptedResponseBuilder {
	b.response.Usage = ScriptedTokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		EstimatedCost:    estimatedCost,
	}
	return b
}

// VisionOnly marks this response for vision-only requests
func (b *ScriptedResponseBuilder) VisionOnly() *ScriptedResponseBuilder {
	b.response.VisionOnly = true
	return b
}

// StreamConfig sets streaming configuration
func (b *ScriptedResponseBuilder) StreamConfig(sc *StreamConfig) *ScriptedResponseBuilder {
	b.response.StreamConfig = sc
	return b
}

// Build returns the constructed ScriptedResponse
func (b *ScriptedResponseBuilder) Build() *ScriptedResponse {
	return b.response
}
