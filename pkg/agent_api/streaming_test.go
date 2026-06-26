package api

import (
	"errors"
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
	callback := func(content string, contentType string) {
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
			Usage: &StreamingUsage{
				PromptTokens:     10,
				CompletionTokens: 2,
				TotalTokens:      12,
				EstimatedCost:    0.001,
				Cost:             0.0,
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
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error to contain %v, got %v", expectedErr, err)
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
		builder := NewStreamingResponseBuilder(func(string, string) {})

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

// TestStreamingResponseBuilder_ZAIReasoningAndContent tests the Z.AI GLM-5 streaming pattern where
// reasoning and content arrive in separate chunks (reasoning first, then content).
func TestStreamingResponseBuilder_ZAIReasoningAndContent(t *testing.T) {
	var capturedReasoning strings.Builder
	var capturedContent strings.Builder

	callback := func(content string, contentType string) {
		switch contentType {
		case "reasoning":
			capturedReasoning.WriteString(content)
		case "assistant_text":
			capturedContent.WriteString(content)
		}
	}

	builder := NewStreamingResponseBuilder(callback)

	// Phase 1: Reasoning-only chunks (simulating Z.AI GLM-5 streaming pattern)
	reasoningChunks := []StreamingChatResponse{
		{
			ID:      "zai-test-123",
			Model:   "glm-5",
			Created: 1234567890,
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Role:             "assistant",
						ReasoningContent: "The user is asking a simple math question. ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ReasoningContent: "I need to calculate 1 + 1 and provide the result. ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ReasoningContent: "The answer is 2.",
					},
				},
			},
		},
	}

	for _, chunk := range reasoningChunks {
		if err := builder.ProcessChunk(&chunk); err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	// Phase 2: Content-only chunks (Z.AI sends content separately from reasoning)
	contentChunks := []StreamingChatResponse{
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Content: "1 + 1 = ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Content: "2",
					},
				},
			},
		},
	}

	for _, chunk := range contentChunks {
		if err := builder.ProcessChunk(&chunk); err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	// Phase 3: Final chunk with finish_reason
	finalChunk := StreamingChatResponse{
		Choices: []StreamingChoice{
			{
				Index:        0,
				FinishReason: stringPtr("stop"),
			},
		},
		Usage: &StreamingUsage{
			PromptTokens:     10,
			CompletionTokens: 15,
			TotalTokens:      25,
			EstimatedCost:    0.001,
		},
	}

	if err := builder.ProcessChunk(&finalChunk); err != nil {
		t.Fatalf("ProcessChunk() error = %v", err)
	}

	// Get final response
	response := builder.GetResponse()

	// Verify basic response structure
	if response.ID != "zai-test-123" {
		t.Errorf("Expected ID 'zai-test-123', got '%s'", response.ID)
	}

	if response.Model != "glm-5" {
		t.Errorf("Expected model 'glm-5', got '%s'", response.Model)
	}

	// Verify content field contains accumulated content (not reasoning)
	expectedContent := "1 + 1 = 2"
	if len(response.Choices) == 0 {
		t.Fatal("Expected at least one choice in response")
	}

	choice := response.Choices[0]
	if choice.Message.Content != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, choice.Message.Content)
	}

	// Verify reasoning_content field contains accumulated reasoning
	expectedReasoning := "The user is asking a simple math question. I need to calculate 1 + 1 and provide the result. The answer is 2."
	if choice.Message.ReasoningContent != expectedReasoning {
		t.Errorf("Expected reasoning_content '%s', got '%s'", expectedReasoning, choice.Message.ReasoningContent)
	}

	// Verify finish reason
	if choice.FinishReason != "stop" {
		t.Errorf("Expected finish reason 'stop', got '%s'", choice.FinishReason)
	}

	// Verify usage
	if response.Usage.PromptTokens != 10 {
		t.Errorf("Expected 10 prompt tokens, got %d", response.Usage.PromptTokens)
	}

	// Verify callbacks captured the correct content
	expectedCapturedContent := expectedContent
	if capturedContent.String() != expectedCapturedContent {
		t.Errorf("Expected callback content '%s', got '%s'", expectedCapturedContent, capturedContent.String())
	}

	expectedCapturedReasoning := expectedReasoning
	if capturedReasoning.String() != expectedCapturedReasoning {
		t.Errorf("Expected callback reasoning '%s', got '%s'", expectedCapturedReasoning, capturedReasoning.String())
	}
}

// TestStreamingResponseBuilder_ReasoningOnlyContent tests the scenario where only reasoning
// content is provided (no separate content chunks), which is common with reasoning models.
// This validates the fallback behavior where reasoning is copied to the content field.
func TestStreamingResponseBuilder_ReasoningOnlyContent(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Simulate a pure reasoning response (no content field ever populated)
	chunks := []StreamingChatResponse{
		{
			ID:      "reasoning-only-123",
			Model:   "glm-5",
			Created: 1234567890,
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						Role:             "assistant",
						ReasoningContent: "First reasoning step: ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ReasoningContent: "Second reasoning step: the answer is ",
					},
				},
			},
		},
		{
			Choices: []StreamingChoice{
				{
					Index: 0,
					Delta: StreamingDelta{
						ReasoningContent: "42.",
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
		},
	}

	for _, chunk := range chunks {
		if err := builder.ProcessChunk(&chunk); err != nil {
			t.Fatalf("ProcessChunk() error = %v", err)
		}
	}

	response := builder.GetResponse()

	if len(response.Choices) == 0 {
		t.Fatal("Expected at least one choice in response")
	}

	choice := response.Choices[0]

	// When only reasoning is provided, content should be populated from reasoning
	// to avoid empty content field (which causes 502 errors)
	expectedContent := "First reasoning step: Second reasoning step: the answer is 42."
	if choice.Message.Content == "" {
		t.Error("Expected content to be populated from reasoning_content, but it was empty")
	}

	if choice.Message.Content != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, choice.Message.Content)
	}

	// Reasoning content should also be preserved
	if choice.Message.ReasoningContent != expectedContent {
		t.Errorf("Expected reasoning_content '%s', got '%s'", expectedContent, choice.Message.ReasoningContent)
	}
}

// TestStreamingResponseBuilder_DataPrefixVariants tests SSE parsing edge cases with
// different data: prefix variants that providers might use.
func TestStreamingResponseBuilder_DataPrefixVariants(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []StreamingChatResponse
		wantErr  bool
	}{
		{
			name:  "Standard data: prefix",
			input: "data: {\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
			expected: []StreamingChatResponse{
				{ID: "test", Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: "Hello"}}}},
			},
		},
		{
			name:  "Data: without space (some providers use this)",
			input: "data:{\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
			expected: []StreamingChatResponse{
				{ID: "test", Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: "Hello"}}}},
			},
		},
		{
			name:  "Multiple SSE events",
			input: "data: {\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" World\"}}]}\n\n",
			expected: []StreamingChatResponse{
				{ID: "test", Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: "Hello"}}}},
				{Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: " World"}}}},
			},
		},
		{
			name:     "Done message",
			input:    "data: [DONE]\n\n",
			expected: nil, // [DONE] returns io.EOF, not a response
			wantErr:  true,
		},
		{
			name:  "Empty line skipped",
			input: "\ndata: {\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
			expected: []StreamingChatResponse{
				{ID: "test", Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: "Hello"}}}},
			},
		},
		{
			name:    "Invalid JSON",
			input:   "data: {invalid json}\n\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results []StreamingChatResponse
			reader := strings.NewReader(tt.input)
			sseReader := NewSSEReader(reader, func(event, data string) error {
				chunk, err := ParseSSEData(data)
				if err != nil {
					// For [DONE], just return the io.EOF
					if err == io.EOF {
						return io.EOF
					}
					return err
				}
				if chunk != nil {
					results = append(results, *chunk)
				}
				return nil
			})

			err := sseReader.Read()

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("SSEReader.Read() error = %v", err)
			}

			if len(results) != len(tt.expected) {
				t.Errorf("Expected %d responses, got %d", len(tt.expected), len(results))
				for i, r := range results {
					t.Logf("Result %d: ID=%s, Content=%s", i, r.ID, r.Choices[0].Delta.Content)
				}
				return
			}

			for i, exp := range tt.expected {
				if results[i].ID != exp.ID {
					t.Errorf("Result %d: expected ID '%s', got '%s'", i, exp.ID, results[i].ID)
				}
				if len(results[i].Choices) != len(exp.Choices) {
					t.Errorf("Result %d: expected %d choices, got %d", i, len(exp.Choices), len(results[i].Choices))
					continue
				}
				if len(exp.Choices) > 0 && len(results[i].Choices) > 0 {
					if results[i].Choices[0].Delta.Content != exp.Choices[0].Delta.Content {
						t.Errorf("Result %d: expected content '%s', got '%s'", i, exp.Choices[0].Delta.Content, results[i].Choices[0].Delta.Content)
					}
				}
			}
		})
	}
}
