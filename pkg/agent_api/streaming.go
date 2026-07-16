package api

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// StreamCallback is called for each content chunk received.
// contentType is "assistant_text" for regular content or "reasoning" for thinking/reasoning content.
type StreamCallback func(content string, contentType string)

// StreamingChoice represents a streaming response choice
type StreamingChoice struct {
	Index        int            `json:"index"`
	Delta        StreamingDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

// StreamingDelta contains incremental updates
type StreamingDelta struct {
	Role             string              `json:"role,omitempty"`
	Content          string              `json:"content,omitempty"`
	Reasoning        string              `json:"reasoning,omitempty"` // GLM reasoning field
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	ReasoningDetails json.RawMessage     `json:"reasoning_details,omitempty"` // Can be string (Minimax) or array (GLM models)
	ToolCalls        []StreamingToolCall `json:"tool_calls,omitempty"`
}

// StreamingToolCall represents an incremental tool call update
type StreamingToolCall struct {
	Index    int                        `json:"index"`
	ID       string                     `json:"id,omitempty"`
	Type     string                     `json:"type,omitempty"`
	Function *StreamingToolCallFunction `json:"function,omitempty"`
}

// StreamingToolCallFunction contains function details
type StreamingToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamingUsage is the usage block on a streaming chunk (providers send it
// on the final data object). Named (not anonymous) so the flexible cost
// fallback in ParseSSEData can allocate one when a provider reports cost
// without the standard token block.
type StreamingUsage struct {
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	EstimatedCost       float64 `json:"estimated_cost"`
	Cost                float64 `json:"cost,omitempty"` // OpenRouter returns cost directly
	ImageTokens         int     `json:"image_tokens,omitempty"`
	PromptTokensDetails struct {
		CachedTokens     int  `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// StreamingChatResponse represents a streaming response chunk
type StreamingChatResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []StreamingChoice `json:"choices"`
	Usage   *StreamingUsage   `json:"usage,omitempty"`
}

// StreamingResponseBuilder accumulates streaming chunks into a complete response
type StreamingResponseBuilder struct {
	mu               sync.Mutex
	response         ChatResponse
	content          strings.Builder
	reasoningContent strings.Builder
	toolCalls        map[int]*ToolCall        // Index to tool call
	toolCallArgs     map[int]*strings.Builder // Index to arguments builder
	finishReason     string
	streamCallback   StreamCallback
	firstTokenTime   time.Time // Track when first token arrives
	lastTokenTime    time.Time // Track when last token arrives
}

// NewStreamingResponseBuilder creates a new streaming response builder
func NewStreamingResponseBuilder(callback StreamCallback) *StreamingResponseBuilder {
	return &StreamingResponseBuilder{
		toolCalls:      make(map[int]*ToolCall),
		toolCallArgs:   make(map[int]*strings.Builder),
		streamCallback: callback,
	}
}

// ProcessChunk processes a streaming chunk and updates the builder state
func (b *StreamingResponseBuilder) ProcessChunk(chunk *StreamingChatResponse) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Initialize response if needed
	if b.response.ID == "" && chunk.ID != "" {
		b.response.ID = chunk.ID
		b.response.Object = chunk.Object
		b.response.Created = chunk.Created
		b.response.Model = chunk.Model
	}

	// Process choices
	for _, choice := range chunk.Choices {
		// Ensure we have a choice at this index
		for len(b.response.Choices) <= choice.Index {
			b.response.Choices = append(b.response.Choices, Choice{})
		}

		// Track timing for any content (including tool calls)
		hasContent := choice.Delta.Content != "" ||
			choice.Delta.ReasoningContent != "" ||
			choice.Delta.Reasoning != "" ||
			len(choice.Delta.ReasoningDetails) > 0 ||
			len(choice.Delta.ToolCalls) > 0

		if hasContent {
			if b.firstTokenTime.IsZero() {
				b.firstTokenTime = time.Now()
			}
			b.lastTokenTime = time.Now()
		}

		// Process content delta
		if choice.Delta.Content != "" {
			b.content.WriteString(choice.Delta.Content)
			// Sanitize content before displaying (remove think tags and ANSI codes)
			sanitizedContent := sanitizeStreamingContent(choice.Delta.Content)
			// Call stream callback for UI updates and timeout reset
			if b.streamCallback != nil {
				b.streamCallback(sanitizedContent, "assistant_text")
			}
		}

		// Process reasoning content delta (handle multiple formats)
		// Priority: reasoning_content > reasoning > reasoning_details (for Minimax string format)
		reasoningDelta := choice.Delta.ReasoningContent
		if reasoningDelta == "" && choice.Delta.Reasoning != "" {
			reasoningDelta = choice.Delta.Reasoning // GLM format
		}
		if reasoningDelta == "" && len(choice.Delta.ReasoningDetails) > 0 {
			// For Minimax, reasoning_details is a string
			// For GLM models, it's an array (which we ignore since we already used the reasoning field)
			var detailsStr string
			if err := json.Unmarshal(choice.Delta.ReasoningDetails, &detailsStr); err == nil {
				reasoningDelta = detailsStr // Prioritize reasoning_details for Minimax
			}
			// If it's an array (GLM format), we ignore it since we already used the reasoning field
		}

		if reasoningDelta != "" {
			b.reasoningContent.WriteString(reasoningDelta)
			// Pass reasoning content to callback for real-time streaming
			if b.streamCallback != nil {
				sanitizedReasoning := sanitizeStreamingContent(reasoningDelta)
				if sanitizedReasoning != "" {
					b.streamCallback(sanitizedReasoning, "reasoning")
				}
			}
		}

		// Process tool calls
		for _, toolCallDelta := range choice.Delta.ToolCalls {
			b.processToolCallDelta(choice.Index, &toolCallDelta)
		}

		// Update finish reason
		if choice.FinishReason != nil {
			b.finishReason = *choice.FinishReason
		}
	}

	// Process usage data (usually comes in final chunk)
	if chunk.Usage != nil {
		b.response.Usage = ChatUsage{
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens:      chunk.Usage.TotalTokens,
			EstimatedCost:    chunk.Usage.EstimatedCost,
			Cost:             chunk.Usage.Cost,
			CachedTokens:     chunk.Usage.PromptTokensDetails.CachedTokens,
			CacheWriteTokens: chunk.Usage.PromptTokensDetails.CacheWriteTokens,
		}
	}

	return nil
}

// processToolCallDelta handles incremental tool call updates
func (b *StreamingResponseBuilder) processToolCallDelta(choiceIndex int, delta *StreamingToolCall) {
	// Get or create tool call
	if _, exists := b.toolCalls[delta.Index]; !exists {
		b.toolCalls[delta.Index] = &ToolCall{}
		b.toolCallArgs[delta.Index] = &strings.Builder{}
	}

	toolCall := b.toolCalls[delta.Index]

	// Update tool call fields
	if delta.ID != "" {
		toolCall.ID = delta.ID
	}
	if delta.Type != "" {
		toolCall.Type = delta.Type
	}
	if delta.Function != nil {
		if delta.Function.Name != "" {
			// Some models (e.g., Harmony/GPT-OSS) append "<|channel|>xxx" suffix to tool names
			// Strip it to get the actual tool name
			toolCall.Function.Name = strings.Split(delta.Function.Name, "<|channel|>")[0]
		}
		if delta.Function.Arguments != "" {
			b.toolCallArgs[delta.Index].WriteString(delta.Function.Arguments)
		}
	}
}

// GetResponse returns the accumulated response
func (b *StreamingResponseBuilder) GetResponse() *ChatResponse {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Finalize the response
	if len(b.response.Choices) > 0 {
		choice := &b.response.Choices[0]
		content := b.content.String()
		reasoningContent := b.reasoningContent.String()

		// Handle case where reasoning models only provide reasoning_content
		// Move reasoning_content to content field to avoid empty content (causes 502 errors)
		if content == "" && reasoningContent != "" {
			choice.Message.Content = reasoningContent
			choice.Message.ReasoningContent = reasoningContent
		} else {
			choice.Message.Content = content
			choice.Message.ReasoningContent = reasoningContent
		}
		choice.FinishReason = b.finishReason

		// Convert tool calls map to slice
		// IMPORTANT: Sort by index to ensure deterministic ordering
		// Providers like Minimax require tool calls and results to be in the same order
		if len(b.toolCalls) > 0 {
			// Collect and sort indices
			indices := make([]int, 0, len(b.toolCalls))
			for idx := range b.toolCalls {
				indices = append(indices, idx)
			}
			// Simple bubble sort for small slices (tool call count is typically small)
			for i := 0; i < len(indices); i++ {
				for j := i + 1; j < len(indices); j++ {
					if indices[i] > indices[j] {
						indices[i], indices[j] = indices[j], indices[i]
					}
				}
			}

			// Build slice in sorted order
			choice.Message.ToolCalls = make([]ToolCall, len(indices))
			for i, idx := range indices {
				tc := b.toolCalls[idx]
				tc.Function.Arguments = b.toolCallArgs[idx].String()
				choice.Message.ToolCalls[i] = *tc
			}
		}
	}

	return &b.response
}

// GetTokenGenerationDuration returns the duration from first token to last token
func (b *StreamingResponseBuilder) GetTokenGenerationDuration() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.firstTokenTime.IsZero() || b.lastTokenTime.IsZero() {
		return 0
	}

	return b.lastTokenTime.Sub(b.firstTokenTime)
}

// SSEReader reads Server-Sent Events from a reader
type SSEReader struct {
	reader  *bufio.Reader
	onEvent func(event, data string) error
}

// NewSSEReader creates a new SSE reader
func NewSSEReader(r io.Reader, onEvent func(event, data string) error) *SSEReader {
	return &SSEReader{
		reader:  bufio.NewReader(r),
		onEvent: onEvent,
	}
}

// Read processes the SSE stream
func (r *SSEReader) Read() error {
	return r.ReadWithTimeout(0) // Default: no timeout
}

// sseLine is a single line read result from the background reader goroutine.
type sseLine struct {
	line string
	err  error
}

// ReadWithTimeout processes the SSE stream with a timeout.
// Each line read is performed in a short-lived goroutine so that the
// select can respond to the timeout without blocking the main loop.
// The background goroutine will exit when the underlying HTTP response
// body is closed (which triggers ReadString to return an error).
func (r *SSEReader) ReadWithTimeout(timeout time.Duration) error {
	var event string
	var dataBuilder strings.Builder

	// Set up timeout handling if specified
	var timer *time.Timer
	var timerChan <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timerChan = timer.C
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
	}

	for {
		// Use a goroutine to read with timeout.
		// Note: if a timeout fires, the goroutine may block on ReadString
		// until the underlying connection is closed. This is acceptable
		// because SSE readers are always backed by HTTP response bodies
		// that will be closed by the HTTP client or server.
		readChan := make(chan sseLine, 1)

		go func() {
			line, err := r.reader.ReadString('\n')
			readChan <- sseLine{line: line, err: err}
		}()

		select {
		case result := <-readChan:
			if result.err != nil {
				if result.err == io.EOF {
					// Process any remaining data
					if dataBuilder.Len() > 0 && r.onEvent != nil {
						if err := r.onEvent(event, dataBuilder.String()); err != nil {
							return agenterrors.Wrap(err, "failed to process remaining data")
						}
					}
					return nil
				}
				return agenterrors.NewNetwork("failed to read stream", result.err)
			}

			line := strings.TrimSpace(result.line)

			// Empty line signals end of event
			if line == "" {
				if dataBuilder.Len() > 0 && r.onEvent != nil {
					if err := r.onEvent(event, dataBuilder.String()); err != nil {
						return agenterrors.Wrap(err, "processing SSE event")
					}
				}
				// Reset for next event
				event = ""
				dataBuilder.Reset()
				continue
			}

			// Parse field
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(line, "data:")
				if dataBuilder.Len() > 0 {
					dataBuilder.WriteString("\n")
				}
				dataBuilder.WriteString(strings.TrimSpace(data))
			}
			// Ignore other fields like id:, retry:

		case <-timerChan:
			return agenterrors.NewTimeout("SSE stream", timeout)
		}
	}
}

// ParseSSEData parses SSE data into a streaming response
func ParseSSEData(data string) (*StreamingChatResponse, error) {
	// Handle special [DONE] message
	if data == "[DONE]" {
		return nil, io.EOF
	}

	var chunk StreamingChatResponse
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, agenterrors.Wrap(err, "failed to parse SSE data")
	}

	// Flexible cost fallback: if the typed decode didn't capture a cost
	// (provider reported it under a non-standard property name), probe the
	// raw chunk. Providers attach cost to the final data object, so this
	// only matters on that chunk.
	if chunk.Usage == nil || (chunk.Usage.EstimatedCost == 0 && chunk.Usage.Cost == 0) {
		if cost, ok := CostFromJSON([]byte(data)); ok {
			if chunk.Usage == nil {
				chunk.Usage = &StreamingUsage{}
			}
			chunk.Usage.EstimatedCost = cost
		}
	}

	return &chunk, nil
}

// sanitizeStreamingContent removes think tags and ANSI codes from streaming content
// This prevents models from outputting think tags or ANSI codes that clutter the display
func sanitizeStreamingContent(content string) string {
	// Remove think tags by constructing the pattern
	openTag := string(rune(60)) + "think" + string(rune(62))
	closeTag := string(rune(60)) + "/think" + string(rune(62))
	thinkRegex := regexp.MustCompile(regexp.QuoteMeta(openTag) + ".*?" + regexp.QuoteMeta(closeTag))
	content = thinkRegex.ReplaceAllString(content, "")

	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile("\x1b\\[[0-9;]*[mGKHJABCD]")
	content = ansiRegex.ReplaceAllString(content, "")

	ansiRegex2 := regexp.MustCompile("\x1b\\([0-9;]*[AB]")
	content = ansiRegex2.ReplaceAllString(content, "")

	// Remove any remaining escape characters
	content = strings.ReplaceAll(content, "\x1b", "")

	return content
}
