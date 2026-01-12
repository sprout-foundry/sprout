package api

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSSEReader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			event string
			data  string
		}
	}{
		{
			name: "Simple SSE event",
			input: `data: {"text": "Hello"}

`,
			expected: []struct {
				event string
				data  string
			}{
				{"", `{"text": "Hello"}`},
			},
		},
		{
			name: "Multiple events",
			input: `data: {"text": "First"}

data: {"text": "Second"}

`,
			expected: []struct {
				event string
				data  string
			}{
				{"", `{"text": "First"}`},
				{"", `{"text": "Second"}`},
			},
		},
		{
			name: "Event with type",
			input: `event: message
data: {"text": "Hello"}

`,
			expected: []struct {
				event string
				data  string
			}{
				{"message", `{"text": "Hello"}`},
			},
		},
		{
			name: "Multi-line data",
			input: `data: {"text": "Line 1
data: Line 2"}

`,
			expected: []struct {
				event string
				data  string
			}{
				{"", `{"text": "Line 1
Line 2"}`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			var results []struct {
				event string
				data  string
			}

			sseReader := NewSSEReader(reader, func(event, data string) error {
				results = append(results, struct {
					event string
					data  string
				}{event, data})
				return nil
			})

			err := sseReader.Read()
			if err != nil {
				t.Fatalf("SSEReader.Read() error = %v", err)
			}

			if len(results) != len(tt.expected) {
				t.Fatalf("Expected %d events, got %d", len(tt.expected), len(results))
			}

			for i, expected := range tt.expected {
				if results[i].event != expected.event || results[i].data != expected.data {
					t.Errorf("Event %d: expected (%q, %q), got (%q, %q)",
						i, expected.event, expected.data, results[i].event, results[i].data)
				}
			}
		})
	}
}

func TestParseSSEData(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantError bool
		isDone    bool
	}{
		{
			name: "Valid streaming response",
			data: `{"id":"123","choices":[{"delta":{"content":"Hello"}}]}`,
		},
		{
			name:   "Done message",
			data:   "[DONE]",
			isDone: true,
		},
		{
			name:      "Invalid JSON",
			data:      `{"invalid": json`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ParseSSEData(tt.data)

			if tt.isDone {
				if err != io.EOF {
					t.Errorf("Expected io.EOF for [DONE], got %v", err)
				}
				return
			}

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseSSEData() error = %v", err)
			}

			if resp == nil {
				t.Fatal("Expected response, got nil")
			}
		})
	}
}

func TestStreamingResponseBuilder(t *testing.T) {
	var capturedContent strings.Builder
	callback := func(content string) {
		capturedContent.WriteString(content)
	}

	builder := NewStreamingResponseBuilder(callback)

	// Simulate streaming chunks
	chunks := []StreamingChatResponse{
		{
			ID:      "test-123",
			Model:   "test-model",
			Created: 1234567890,
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Content: "Hello ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Content: "world!",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index:        0,
					FinishReason: stringPtr("stop"),
				},
			},
			Usage: &struct {
				PromptTokens        int     `json:"prompt_tokens"`
				CompletionTokens    int     `json:"completion_tokens"`
				TotalTokens         int     `json:"total_tokens"`
				EstimatedCost       float64 `json:"estimated_cost"`
				PromptTokensDetails struct {
					CachedTokens     int  `json:"cached_tokens"`
					CacheWriteTokens *int `json:"cache_write_tokens"`
				} `json:"prompt_tokens_details,omitempty"`
			}{
				PromptTokens:     10,
				CompletionTokens: 2,
				TotalTokens:      12,
				EstimatedCost:    0.001,
			},
		},
	}

	// Process chunks
	for _, chunk := range chunks {
		err := builder.ProcessChunk(&chunk)
		if err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	// Get final response
	response := builder.GetResponse()

	// Verify response
	if response.ID != "test-123" {
		t.Errorf("Expected ID 'test-123', got '%s'", response.ID)
	}

	if len(response.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(response.Choices))
	}

	if response.Choices[0].Message.Content != "Hello world!" {
		t.Errorf("Expected content 'Hello world!', got '%s'", response.Choices[0].Message.Content)
	}

	if response.Choices[0].FinishReason != "stop" {
		t.Errorf("Expected finish reason 'stop', got '%s'", response.Choices[0].FinishReason)
	}

	if response.Usage.TotalTokens != 12 {
		t.Errorf("Expected 12 total tokens, got %d", response.Usage.TotalTokens)
	}

	// Verify callback was called
	if capturedContent.String() != "Hello world!" {
		t.Errorf("Expected callback content 'Hello world!', got '%s'", capturedContent.String())
	}
}

func TestStreamingWithToolCalls(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Simulate tool call streaming
	chunks := []StreamingChatResponse{
		{
			ID: "test-tool",
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 0,
								ID:    "call_123",
								Type:  "function",
								Function: &StreamingToolCallFunction{
									Name: "get_weather",
								},
							},
						},
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 0,
								Function: &StreamingToolCallFunction{
									Arguments: `{"location": "`,
								},
							},
						},
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 0,
								Function: &StreamingToolCallFunction{
									Arguments: `New York"}`,
								},
							},
						},
					},
				},
			},
		},
	}

	// Process chunks
	for _, chunk := range chunks {
		err := builder.ProcessChunk(&chunk)
		if err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	// Get final response
	response := builder.GetResponse()

	// Verify tool calls
	if len(response.Choices) == 0 || len(response.Choices[0].Message.ToolCalls) != 1 {
		t.Fatal("Expected 1 tool call")
	}

	toolCall := response.Choices[0].Message.ToolCalls[0]
	if toolCall.ID != "call_123" {
		t.Errorf("Expected tool call ID 'call_123', got '%s'", toolCall.ID)
	}

	if toolCall.Function.Name != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got '%s'", toolCall.Function.Name)
	}

	expectedArgs := `{"location": "New York"}`
	if toolCall.Function.Arguments != expectedArgs {
		t.Errorf("Expected arguments '%s', got '%s'", expectedArgs, toolCall.Function.Arguments)
	}
}

// TestStreamingWithMultipleToolCalls verifies that tool calls maintain their order
// This is critical for providers like Minimax that require tool calls and results
// to be in the same order
func TestStreamingWithMultipleToolCalls(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Simulate multiple tool calls streaming in out-of-order chunks
	// This tests that the final ordering is correct (sorted by index)
	chunks := []StreamingChatResponse{
		{
			ID: "test-multi-tool",
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 1, // Second tool call arrives first
								ID:    "call_456",
								Type:  "function",
								Function: &StreamingToolCallFunction{
									Name:      "read_file",
									Arguments: `{"path": "file2.txt"}`,
								},
							},
						},
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 0, // First tool call arrives second
								ID:    "call_123",
								Type:  "function",
								Function: &StreamingToolCallFunction{
									Name:      "read_file",
									Arguments: `{"path": "file1.txt"}`,
								},
							},
						},
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ToolCalls: []StreamingToolCall{
							{
								Index: 2, // Third tool call arrives third
								ID:    "call_789",
								Type:  "function",
								Function: &StreamingToolCallFunction{
									Name:      "write_file",
									Arguments: `{"path": "output.txt"}`,
								},
							},
						},
					},
				},
			},
		},
	}

	// Process chunks
	for _, chunk := range chunks {
		err := builder.ProcessChunk(&chunk)
		if err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	// Get final response
	response := builder.GetResponse()

	// Verify tool calls
	if len(response.Choices) == 0 || len(response.Choices[0].Message.ToolCalls) != 3 {
		t.Fatalf("Expected 3 tool calls, got %d", len(response.Choices[0].Message.ToolCalls))
	}

	// CRITICAL: Verify tool calls are in index order (0, 1, 2), not arrival order
	toolCalls := response.Choices[0].Message.ToolCalls
	if toolCalls[0].ID != "call_123" {
		t.Errorf("Expected first tool call ID 'call_123', got '%s'", toolCalls[0].ID)
	}
	if toolCalls[1].ID != "call_456" {
		t.Errorf("Expected second tool call ID 'call_456', got '%s'", toolCalls[1].ID)
	}
	if toolCalls[2].ID != "call_789" {
		t.Errorf("Expected third tool call ID 'call_789', got '%s'", toolCalls[2].ID)
	}

	// Verify tool call names are also in correct order
	if toolCalls[0].Function.Name != "read_file" || toolCalls[0].Function.Arguments != `{"path": "file1.txt"}` {
		t.Errorf("First tool call has incorrect data: %v", toolCalls[0])
	}
	if toolCalls[1].Function.Name != "read_file" || toolCalls[1].Function.Arguments != `{"path": "file2.txt"}` {
		t.Errorf("Second tool call has incorrect data: %v", toolCalls[1])
	}
	if toolCalls[2].Function.Name != "write_file" || toolCalls[2].Function.Arguments != `{"path": "output.txt"}` {
		t.Errorf("Third tool call has incorrect data: %v", toolCalls[2])
	}
}

// Helper function for string pointers
func stringPtr(s string) *string {
	return &s
}

// Test streaming error handling
func TestStreamingErrorScenarios(t *testing.T) {
	t.Run("Empty reader", func(t *testing.T) {
		reader := strings.NewReader("")
		sseReader := NewSSEReader(reader, func(event, data string) error {
			t.Error("Callback should not be called for empty reader")
			return nil
		})

		err := sseReader.Read()
		if err != nil {
			t.Errorf("Expected nil error for empty reader, got %v", err)
		}
	})

	t.Run("Callback error propagation", func(t *testing.T) {
		reader := strings.NewReader("data: test\n\n")
		expectedErr := fmt.Errorf("callback error")

		sseReader := NewSSEReader(reader, func(event, data string) error {
			return expectedErr
		})

		err := sseReader.Read()
		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})

	t.Run("Malformed SSE", func(t *testing.T) {
		reader := strings.NewReader("malformed sse data\n\n")
		var called bool

		sseReader := NewSSEReader(reader, func(event, data string) error {
			called = true
			return nil
		})

		err := sseReader.Read()
		if err != nil {
			t.Errorf("SSEReader should handle malformed data gracefully, got error: %v", err)
		}

		if called {
			t.Error("Callback should not be called for malformed SSE")
		}
	})
}

// Benchmark streaming response building
// Disabled: Rename to BenchmarkStreamingResponseBuilder to enable
// This benchmark can consume significant memory with high iteration counts
func BenchmarkStreamingResponseBuilder_DISABLED(b *testing.B) {
	for i := 0; i < b.N; i++ {
		builder := NewStreamingResponseBuilder(func(string) {})

		// Simulate 100 content chunks
		for j := 0; j < 100; j++ {
			chunk := &StreamingChatResponse{
				Choices: []StreamingChoice{
					{
						Index: 0,
						Delta: StreamingDelta{
							Content: fmt.Sprintf("Chunk %d ", j),
						},
					},
				},
			}
			builder.ProcessChunk(chunk)
		}

		builder.GetResponse()
	}
}

func TestStreamingResponseBuilder_EmptyContentHandling(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Test case: reasoning-only response (empty content, non-empty reasoning_content)
	chunk := &StreamingChatResponse{
		Choices: []StreamingChoice{
			{
				Index: 0,
				Delta: StreamingDelta{
					ReasoningContent: "The user is asking about...",
				},
			},
		},
	}

	// Process the chunk
	builder.ProcessChunk(chunk)

	// Finalize the response
	response := builder.GetResponse()

	// Verify the response
	if len(response.Choices) == 0 {
		t.Fatal("Expected at least one choice in response")
	}

	choice := response.Choices[0]

	// Content should be populated with reasoning content to avoid empty content field
	if choice.Message.Content == "" {
		t.Error("Expected content to be populated from reasoning_content, but it was empty")
	}

	if choice.Message.Content != "The user is asking about..." {
		t.Errorf("Expected content to be 'The user is asking about...', got: %s", choice.Message.Content)
	}

	if choice.Message.ReasoningContent != "The user is asking about..." {
		t.Errorf("Expected reasoning_content to be preserved, got: %s", choice.Message.ReasoningContent)
	}
}
