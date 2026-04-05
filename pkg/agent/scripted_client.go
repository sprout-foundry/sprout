package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

// Compile-time check that ScriptedClient implements api.ClientInterface
var _ api.ClientInterface = (*ScriptedClient)(nil)

// ScriptedTokenUsage represents token usage metrics for a scripted response
type ScriptedTokenUsage struct {
	PromptTokens        int             `json:"prompt_tokens"`
	CompletionTokens    int             `json:"completion_tokens"`
	TotalTokens         int             `json:"total_tokens"`
	EstimatedCost       float64         `json:"estimated_cost"`
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

// ScriptedClient is an enhanced mock client for comprehensive E2E testing
// It supports:
// - Sequential scripted responses with tool calls
// - Streaming simulation
// - Error injection
// - Vision support
// - Rate limit simulation
type ScriptedClient struct {
	*factory.TestClient

	// Scripted responses in order
	responses []*ScriptedResponse

	// Current index in the responses slice
	index int

	// Mutex for thread-safe access
	mu sync.Mutex

	// Rate limit simulation state
	rateLimitCounter    int
	rateLimitExceeded   bool
	rateLimitThreshold  int

	// Vision support flag
	supportsVision bool

	// Vision model name
	visionModel string

	// Debug mode (atomic for lock-free access)
	debug atomic.Bool

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// TPS simulation
	lastTPS    float64
	averageTPS float64

	// Response history for debugging and testing
	responseHistory []*ScriptedResponse

	// Sent requests recording for testing
	sentRequests [][]api.Message
}

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

// NewScriptedClient creates a new scripted client with optional initial responses
func NewScriptedClient(responses ...*ScriptedResponse) *ScriptedClient {
	ctx, cancel := context.WithCancel(context.Background())

	client := &ScriptedClient{
		TestClient:      &factory.TestClient{},
		responses:       responses,
		index:           0,
		ctx:             ctx,
		cancel:          cancel,
		lastTPS:         100.0,
		averageTPS:      100.0,
		supportsVision:  false,
		responseHistory: make([]*ScriptedResponse, 0),
		sentRequests:   make([][]api.Message, 0),
	}

	return client
}

// NewScriptedClientWithVision creates a scripted client that supports vision models
func NewScriptedClientWithVision(model string, responses ...*ScriptedResponse) *ScriptedClient {
	ctx, cancel := context.WithCancel(context.Background())

	client := &ScriptedClient{
		TestClient:      &factory.TestClient{},
		responses:       responses,
		index:           0,
		ctx:             ctx,
		cancel:          cancel,
		lastTPS:         100.0,
		averageTPS:      100.0,
		supportsVision:  true,
		visionModel:     model,
		responseHistory: make([]*ScriptedResponse, 0),
		sentRequests:    make([][]api.Message, 0),
	}

	return client
}

// AddResponse appends a response to the end of the queue
func (c *ScriptedClient) AddResponse(response *ScriptedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, response)
}

// SetResponses replaces all responses and resets all derived state.
func (c *ScriptedClient) SetResponses(responses []*ScriptedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = responses
	c.index = 0
	c.rateLimitCounter = 0
	c.rateLimitExceeded = false
	c.rateLimitThreshold = 0
	c.sentRequests = make([][]api.Message, 0)
}

// Reset resets the response index to the beginning
func (c *ScriptedClient) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.index = 0
	c.rateLimitCounter = 0
	c.rateLimitExceeded = false
	c.rateLimitThreshold = 0
	c.sentRequests = make([][]api.Message, 0)
}

// GetNextResponse returns the next response without advancing the index
func (c *ScriptedClient) GetNextResponse() *ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses == nil || c.index >= len(c.responses) {
		return nil
	}
	return c.responses[c.index]
}

// AdvanceIndex advances to the next response
func (c *ScriptedClient) AdvanceIndex() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses != nil && c.index < len(c.responses) {
		c.index++
	}
}

// GetIndex returns the current response index
func (c *ScriptedClient) GetIndex() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.index
}

// SetIndex sets the response index (useful for replay scenarios)
func (c *ScriptedClient) SetIndex(idx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx < 0 {
		idx = 0
	}
	if c.responses == nil || idx > len(c.responses) {
		idx = len(c.responses)
	}
	c.index = idx
}

// Length returns the number of scripted responses
func (c *ScriptedClient) Length() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses == nil {
		return 0
	}
	return len(c.responses)
}

// LastResponse returns the last consumed response
func (c *ScriptedClient) LastResponse() *ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responseHistory) == 0 {
		return nil
	}
	return c.responseHistory[len(c.responseHistory)-1]
}

// ResponseHistory returns all consumed responses
func (c *ScriptedClient) ResponseHistory() []*ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Return a copy to prevent external modification
	history := make([]*ScriptedResponse, len(c.responseHistory))
	copy(history, c.responseHistory)
	return history
}

// ClearHistory clears the response history
func (c *ScriptedClient) ClearHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseHistory = make([]*ScriptedResponse, 0)
}

// GetSentRequests returns a defensive deep copy of all recorded request message arrays.
// Both the outer slice and each inner []api.Message slice are copied to prevent
// external mutation of the client's internal state.
func (c *ScriptedClient) GetSentRequests() [][]api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([][]api.Message, len(c.sentRequests))
	for i, msgs := range c.sentRequests {
		result[i] = append([]api.Message(nil), msgs...)
	}
	return result
}

// GetSentRequest returns a specific request's messages (nil if out of range)
func (c *ScriptedClient) GetSentRequest(index int) []api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.sentRequests) {
		return nil
	}
	return append([]api.Message(nil), c.sentRequests[index]...)
}

// ClearSentRequests clears all recorded sent requests
func (c *ScriptedClient) ClearSentRequests() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sentRequests = make([][]api.Message, 0)
}

// debugLog logs a message if debug mode is enabled
func (c *ScriptedClient) debugLog(format string, args ...interface{}) {
	if c.debug.Load() {
		// In a real implementation, you might want to use a proper logger
		fmt.Printf("[DEBUG] %s\n", fmt.Sprintf(format, args...))
	}
}

// advanceIndex advances past the current response and records it in history.
// This must be called while NOT holding c.mu.
func (c *ScriptedClient) advanceIndex(resp *ScriptedResponse) {
	c.mu.Lock()
	if c.responses != nil && c.index < len(c.responses) {
		c.index++
	}
	if resp != nil {
		c.responseHistory = append(c.responseHistory, resp)
	}
	c.mu.Unlock()
}

// resolveUsage extracts usage metrics from a response, falling back to defaults.
func resolveUsage(resp *ScriptedResponse) ScriptedTokenUsage {
	if resp != nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 || resp.Usage.TotalTokens > 0) {
		return resp.Usage
	}
	return ScriptedTokenUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		EstimatedCost:    0.0,
	}
}

// buildChatResponse constructs an api.ChatResponse from the given parameters.
// idPrefix is typically "scripted-response-" or "vision-response-".
func (c *ScriptedClient) buildChatResponse(
	idPrefix string,
	model string,
	content string,
	finishReason string,
	reasoningContent string,
	images []api.ImageData,
	toolCalls []api.ToolCall,
	usage ScriptedTokenUsage,
) *api.ChatResponse {
	// Map PromptTokensDetails from usage when present
	promptTokenDetails := struct {
		CachedTokens     int  `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	}{}
	if usage.PromptTokensDetails.CachedTokens > 0 || usage.PromptTokensDetails.CacheWriteTokens != nil {
		promptTokenDetails.CachedTokens = usage.PromptTokensDetails.CachedTokens
		promptTokenDetails.CacheWriteTokens = usage.PromptTokensDetails.CacheWriteTokens
	}

	return &api.ChatResponse{
		ID:      idPrefix + fmt.Sprintf("%d", c.GetIndex()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []api.Choice{{
			Index: 0,
			Message: struct {
				Role             string          `json:"role"`
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content,omitempty"`
				Images           []api.ImageData `json:"images,omitempty"`
				ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             "assistant",
				Content:          content,
				ReasoningContent: reasoningContent,
				Images:           images,
				ToolCalls:        toolCalls,
			},
			FinishReason: finishReason,
		}},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:        usage.PromptTokens,
			CompletionTokens:    usage.CompletionTokens,
			TotalTokens:         usage.TotalTokens,
			EstimatedCost:       usage.EstimatedCost,
			PromptTokensDetails: promptTokenDetails,
		},
	}
}

// SendChatRequest sends a chat request and returns a scripted response
func (c *ScriptedClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	c.mu.Lock()

	// Record sent request
	msgCopy := append([]api.Message(nil), messages...)
	c.sentRequests = append(c.sentRequests, msgCopy)

	// Rate limit simulation:
	// - rateLimitExceeded is set once the counter reaches the threshold; all subsequent
	//   calls fail immediately with RateLimitExceededError.
	// - Per-response RateLimitAfter allows individual responses to trigger rate limiting.
	if c.rateLimitExceeded {
		c.mu.Unlock()
		c.debugLog("Rate limit exceeded after %d attempts", c.rateLimitCounter)
		return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
	}

	var resp *ScriptedResponse
	if c.responses != nil && c.index < len(c.responses) {
		resp = c.responses[c.index]
	}

	// Handle rate limit simulation - increment counter first, then check
	// This ensures we count the current request.
	// Once a threshold is established (from any response), it persists until reset.
	if resp != nil && resp.RateLimitAfter > 0 {
		c.rateLimitThreshold = resp.RateLimitAfter
	}
	if c.rateLimitThreshold > 0 {
		c.rateLimitCounter++
		if c.rateLimitCounter >= c.rateLimitThreshold {
			c.rateLimitExceeded = true
			c.mu.Unlock()
			c.debugLog("Rate limit triggered after %d responses", c.rateLimitCounter)
			return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
		}
	}

	// Get response content before releasing lock
	var content string
	var toolCalls []api.ToolCall
	var finishReason string
	var reasoningContent string
	var images []api.ImageData

	if resp != nil {
		content = resp.Content
		toolCalls = resp.ToolCalls
		finishReason = resp.FinishReason
		reasoningContent = resp.ReasoningContent
		images = resp.Images
	}

	c.mu.Unlock()

	// Handle explicit error injection
	if resp != nil && resp.Error != nil {
		c.debugLog("Returning injected error: %v", resp.Error)
		c.advanceIndex(resp)
		return nil, resp.Error
	}

	// Check for delay (rate limit simulation)
	if resp != nil && resp.Delay > 0 {
		c.debugLog("Applying delay of %v", resp.Delay)
		select {
		case <-time.After(resp.Delay):
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		}
	}

	response := c.buildChatResponse(
		"scripted-response-",
		c.GetModel(),
		content,
		finishReason,
		reasoningContent,
		images,
		toolCalls,
		resolveUsage(resp),
	)

	c.advanceIndex(resp)

	c.debugLog("Consumed response at index %d", c.GetIndex()-1)

	return response, nil
}

// SendChatRequestStream sends a streaming chat request with full simulation support
func (c *ScriptedClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	c.mu.Lock()

	// Record sent request
	msgCopy := append([]api.Message(nil), messages...)
	c.sentRequests = append(c.sentRequests, msgCopy)

	// Rate limit simulation:
	// - rateLimitExceeded is set once the counter reaches the threshold; all subsequent
	//   calls fail immediately with RateLimitExceededError.
	// - Per-response RateLimitAfter allows individual responses to trigger rate limiting.
	if c.rateLimitExceeded {
		c.mu.Unlock()
		c.debugLog("Rate limit exceeded after %d attempts", c.rateLimitCounter)
		return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
	}

	var resp *ScriptedResponse
	if c.responses != nil && c.index < len(c.responses) {
		resp = c.responses[c.index]
	}

	// Handle rate limit simulation - increment counter first, then check.
	// Once a threshold is established (from any response), it persists until reset.
	if resp != nil && resp.RateLimitAfter > 0 {
		c.rateLimitThreshold = resp.RateLimitAfter
	}
	if c.rateLimitThreshold > 0 {
		c.rateLimitCounter++
		if c.rateLimitCounter >= c.rateLimitThreshold {
			c.rateLimitExceeded = true
			c.mu.Unlock()
			c.debugLog("Rate limit triggered after %d responses", c.rateLimitCounter)
			return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
		}
	}

	// Get response content before releasing lock
	var content string
	var finishReason string
	var reasoningContent string
	var images []api.ImageData
	var toolCalls []api.ToolCall

	if resp != nil {
		content = resp.Content
		finishReason = resp.FinishReason
		reasoningContent = resp.ReasoningContent
		images = resp.Images
		toolCalls = resp.ToolCalls
	} else {
		content = "Test response from mock provider"
		finishReason = "stop"
	}

	c.mu.Unlock()

	// Handle explicit error injection
	if resp != nil && resp.Error != nil {
		c.debugLog("Returning injected error: %v", resp.Error)
		c.advanceIndex(resp)
		return nil, resp.Error
	}

	// Check for delay
	if resp != nil && resp.Delay > 0 {
		c.debugLog("Applying delay of %v", resp.Delay)
		select {
		case <-time.After(resp.Delay):
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		}
	}

	// Handle streaming configuration
	if resp != nil && resp.StreamConfig != nil {
		streamConfig := resp.StreamConfig

		// Validate stream config
		if len(streamConfig.Chunks) == 0 {
			c.advanceIndex(resp)
			return nil, fmt.Errorf("ScriptedResponse.StreamConfig.Chunks must not be empty")
		}
		if streamConfig.ChunkErrors != nil && len(streamConfig.ChunkErrors) > len(streamConfig.Chunks) {
			c.advanceIndex(resp)
			return nil, fmt.Errorf("ScriptedResponse.StreamConfig.ChunkErrors length exceeds number of chunks")
		}

		totalTokens := 0
		startTime := time.Now()

		// Stream each chunk
		for i, chunk := range streamConfig.Chunks {
			select {
			case <-c.ctx.Done():
				// Context cancellation: do NOT advance index (soft interruption, retry makes sense)
				return nil, c.ctx.Err()
			default:
			}

			callback(chunk, "assistant_text")

			// Check for per-chunk errors
			if i < len(streamConfig.ChunkErrors) && streamConfig.ChunkErrors[i] != nil {
				c.debugLog("Chunk %d error: %v", i, streamConfig.ChunkErrors[i])
				c.advanceIndex(resp)
				return nil, streamConfig.ChunkErrors[i]
			}

			// Check for error after N chunks
			if streamConfig.ErrorAfterChunks > 0 && i >= streamConfig.ErrorAfterChunks-1 {
				c.debugLog("Error after %d chunks", streamConfig.ErrorAfterChunks)
				c.advanceIndex(resp)
				if streamConfig.StreamError != nil {
					return nil, streamConfig.StreamError
				}
				return nil, fmt.Errorf("simulated stream error after %d chunks", streamConfig.ErrorAfterChunks)
			}

			// Add delay between chunks if configured
			if streamConfig.ChunkDelay > 0 && i < len(streamConfig.Chunks)-1 {
				select {
				case <-time.After(streamConfig.ChunkDelay):
				case <-c.ctx.Done():
					// Context cancellation: do NOT advance index
					return nil, c.ctx.Err()
				}
			}

			// Calculate TPS for this chunk
			if streamConfig.ChunkDelay > 0 && streamConfig.TokensPerChunk > 0 {
				chunkTPS := float64(streamConfig.TokensPerChunk) / streamConfig.ChunkDelay.Seconds()
				c.mu.Lock()
				c.lastTPS = chunkTPS
				// Update average TPS
				c.averageTPS = (c.averageTPS + chunkTPS) / 2
				c.mu.Unlock()
			}

			totalTokens += streamConfig.TokensPerChunk

			c.debugLog("Streamed chunk %d (%d tokens, %.2f TPS)", i+1, streamConfig.TokensPerChunk, c.lastTPS)
		}

		finishReason = streamConfig.FinishReason

		// Set final TPS based on total stream
		if streamConfig.ChunkDelay > 0 && len(streamConfig.Chunks) > 0 {
			totalTime := time.Since(startTime)
			if totalTime > 0 {
				finalTPS := float64(totalTokens) / totalTime.Seconds()
				c.mu.Lock()
				c.lastTPS = finalTPS
				c.mu.Unlock()
			}
		}
	} else {
		// Simple streaming: send content as single chunk
		callback(content, "assistant_text")
	}

	response := c.buildChatResponse(
		"scripted-response-",
		c.GetModel(),
		content,
		finishReason,
		reasoningContent,
		images,
		toolCalls,
		resolveUsage(resp),
	)

	c.advanceIndex(resp)

	c.debugLog("Consumed streaming response at index %d", c.GetIndex()-1)

	return response, nil
}

// SendVisionRequest sends a vision-enabled chat request
func (c *ScriptedClient) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	if !c.supportsVision {
		return nil, fmt.Errorf("vision requests not supported in ScriptedClient")
	}

	c.mu.Lock()

	// Record sent request
	msgCopy := append([]api.Message(nil), messages...)
	c.sentRequests = append(c.sentRequests, msgCopy)

	// For vision requests, find vision-only responses
	var resp *ScriptedResponse
	var foundIndex int = -1

	if c.responses != nil {
		for i := c.index; i < len(c.responses); i++ {
			if c.responses[i].VisionOnly {
				resp = c.responses[i]
				foundIndex = i
				break
			}
		}
	}

	// Rate limit simulation:
	// - rateLimitExceeded is set once the counter reaches the threshold; all subsequent
	//   calls fail immediately with RateLimitExceededError.
	// - Per-response RateLimitAfter allows individual responses to trigger rate limiting.
	if c.rateLimitExceeded {
		c.mu.Unlock()
		c.debugLog("Vision rate limit exceeded after %d attempts", c.rateLimitCounter)
		return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
	}

	// Handle rate limit simulation for vision requests.
	// Once a threshold is established (from any response), it persists until reset.
	if resp != nil && resp.RateLimitAfter > 0 {
		c.rateLimitThreshold = resp.RateLimitAfter
	}
	if resp != nil && c.rateLimitThreshold > 0 {
		c.rateLimitCounter++
		if c.rateLimitCounter >= c.rateLimitThreshold {
			c.rateLimitExceeded = true
			c.mu.Unlock()
			c.debugLog("Vision rate limit triggered after %d responses", c.rateLimitCounter)
			return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: fmt.Errorf("rate limit exceeded")}
		}
	}

	// Update index if we found a vision-only response
	if foundIndex >= 0 {
		c.index = foundIndex + 1
	}

	// Copy all necessary data while holding the lock to avoid race conditions
	var content string
	var finishReason string
	var reasoningContent string
	var images []api.ImageData
	var toolCalls []api.ToolCall
	var errorToReturn error
	var delay time.Duration

	if resp != nil {
		content = resp.Content
		finishReason = resp.FinishReason
		reasoningContent = resp.ReasoningContent
		images = resp.Images
		toolCalls = resp.ToolCalls
		errorToReturn = resp.Error
		delay = resp.Delay
	}

	c.mu.Unlock()

	// Check for error injection (outside lock)
	if errorToReturn != nil {
		c.debugLog("Vision request returning injected error: %v", errorToReturn)
		return nil, errorToReturn
	}

	// Check for delay (outside lock)
	if delay > 0 {
		c.debugLog("Vision request applying delay of %v", delay)
		select {
		case <-time.After(delay):
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		}
	}

	// Build vision response (outside lock)
	response := c.buildChatResponse(
		"vision-response-",
		c.visionModel,
		content,
		finishReason,
		reasoningContent,
		images,
		toolCalls,
		resolveUsage(resp),
	)

	c.mu.Lock()
	// Add to response history (index is already managed inside the lock section above)
	if resp != nil {
		c.responseHistory = append(c.responseHistory, resp)
	}
	c.mu.Unlock()

	c.debugLog("Vision request consumed response at index %d", c.GetIndex()-1)

	return response, nil
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

// CheckConnection always returns nil for test client
func (c *ScriptedClient) CheckConnection() error {
	return nil
}

// SetDebug enables debug mode
func (c *ScriptedClient) SetDebug(debug bool) {
	c.debug.Store(debug)
}

// SetModel sets the model name
func (c *ScriptedClient) SetModel(model string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.TestClient.SetModel(model)
}

// GetModel returns the current model
func (c *ScriptedClient) GetModel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.TestClient.GetModel()
}

// GetProvider returns the provider name
func (c *ScriptedClient) GetProvider() string {
	return "test"
}

// GetModelContextLimit returns the context limit
func (c *ScriptedClient) GetModelContextLimit() (int, error) {
	return 4096, nil
}

// ListModels returns available models
func (c *ScriptedClient) ListModels() ([]api.ModelInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	model := c.TestClient.GetModel()
	if model == "" {
		return nil, fmt.Errorf("no model configured")
	}

	models := []api.ModelInfo{
		{Name: model, ContextLength: 4096},
	}

	if c.supportsVision && c.visionModel != "" {
		models = append(models, api.ModelInfo{Name: c.visionModel, ContextLength: 8192})
	}

	return models, nil
}

// SupportsVision returns whether vision is supported
func (c *ScriptedClient) SupportsVision() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.supportsVision
}

// GetVisionModel returns the vision model name
func (c *ScriptedClient) GetVisionModel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.visionModel
}

// GetLastTPS returns the last tokens per second
func (c *ScriptedClient) GetLastTPS() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastTPS
}

// GetAverageTPS returns the average tokens per second
func (c *ScriptedClient) GetAverageTPS() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.averageTPS
}

// GetTPSStats returns TPS statistics
func (c *ScriptedClient) GetTPSStats() map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]float64{
		"last":    c.lastTPS,
		"average": c.averageTPS,
	}
}

// ResetTPSStats resets TPS statistics
func (c *ScriptedClient) ResetTPSStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastTPS = 100.0
	c.averageTPS = 100.0
}

// Cancel cancels any pending operations
func (c *ScriptedClient) Cancel() {
	c.cancel()
}

// Close closes the client and releases resources
func (c *ScriptedClient) Close() {
	c.Cancel()
}
