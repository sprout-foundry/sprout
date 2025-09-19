package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// StreamCallback is called for each content chunk received
type StreamCallback func(content string)

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
	ReasoningContent string              `json:"reasoning_content,omitempty"`
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

// StreamingChatResponse represents a streaming response chunk
type StreamingChatResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []StreamingChoice `json:"choices"`
	Usage   *struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
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

		// Process content delta
		if choice.Delta.Content != "" {
			// Track timing for first content token
			if b.firstTokenTime.IsZero() {
				b.firstTokenTime = time.Now()
			}
			b.lastTokenTime = time.Now()

			b.content.WriteString(choice.Delta.Content)
			// Call stream callback for UI updates
			if b.streamCallback != nil {
				b.streamCallback(choice.Delta.Content)
			}
		}

		// Process reasoning content delta
		if choice.Delta.ReasoningContent != "" {
			b.reasoningContent.WriteString(choice.Delta.ReasoningContent)
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
		b.response.Usage = *chunk.Usage
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
			toolCall.Function.Name = delta.Function.Name
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
		choice.Message.Content = b.content.String()
		choice.Message.ReasoningContent = b.reasoningContent.String()
		choice.FinishReason = b.finishReason

		// Convert tool calls map to slice
		if len(b.toolCalls) > 0 {
			choice.Message.ToolCalls = make([]ToolCall, len(b.toolCalls))
			for idx, tc := range b.toolCalls {
				tc.Function.Arguments = b.toolCallArgs[idx].String()
				choice.Message.ToolCalls[idx] = *tc
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
	var event string
	var dataBuilder strings.Builder

	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Process any remaining data
				if dataBuilder.Len() > 0 && r.onEvent != nil {
					if err := r.onEvent(event, dataBuilder.String()); err != nil {
						return err
					}
				}
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)

		// Empty line signals end of event
		if line == "" {
			if dataBuilder.Len() > 0 && r.onEvent != nil {
				if err := r.onEvent(event, dataBuilder.String()); err != nil {
					return err
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
		return nil, fmt.Errorf("failed to parse SSE data: %w", err)
	}

	return &chunk, nil
}
