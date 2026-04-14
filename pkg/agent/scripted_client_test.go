package agent

import (
	"fmt"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Builder Pattern Tests
// ---------------------------------------------------------------------------

func TestScriptedResponseBuilder_Basic(t *testing.T) {
	t.Parallel()

	resp := NewScriptedResponseBuilder().
		Content("test content").
		FinishReason("stop").
		Build()

	if resp.Content != "test content" {
		t.Errorf("expected content 'test content', got '%s'", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", resp.FinishReason)
	}
}

func TestScriptedResponseBuilder_ToolCalls(t *testing.T) {
	t.Parallel()

	toolCall := api.ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "read_file",
			Arguments: `{"path": "/test/file.txt"}`,
		},
	}

	resp := NewScriptedResponseBuilder().
		Content("Let me check the file").
		ToolCall(toolCall).
		Build()

	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_123" {
		t.Errorf("expected tool call ID 'call_123', got '%s'", resp.ToolCalls[0].ID)
	}
}

func TestScriptedResponseBuilder_MultipleToolCalls(t *testing.T) {
	t.Parallel()

	toolCalls := []api.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: `{"path": "/file1.txt"}`,
			},
		},
		{
			ID:   "call_2",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "list_dir",
				Arguments: `{"path": "/src"}`,
			},
		},
	}

	resp := NewScriptedResponseBuilder().
		Content("Running multiple tools").
		ToolCalls(toolCalls).
		Build()

	if len(resp.ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestScriptedResponseBuilder_Vision(t *testing.T) {
	t.Parallel()

	image := api.ImageData{
		Type:   "base64",
		Base64: "base64encodedimage",
	}

	resp := NewScriptedResponseBuilder().
		Content("I see an image").
		Images([]api.ImageData{image}).
		Build()

	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
}

func TestScriptedResponseBuilder_DelayAndRateLimit(t *testing.T) {
	t.Parallel()

	resp := NewScriptedResponseBuilder().
		Content("Delayed response").
		Delay(100 * time.Millisecond).
		RateLimitAfter(3).
		Build()

	if resp.Delay != 100*time.Millisecond {
		t.Errorf("expected delay 100ms, got %v", resp.Delay)
	}
	if resp.RateLimitAfter != 3 {
		t.Errorf("expected rate limit after 3, got %d", resp.RateLimitAfter)
	}
}

func TestScriptedResponseBuilder_Streaming(t *testing.T) {
	t.Parallel()

	streamConfig := &StreamConfig{
		Chunks:         []string{"chunk1", "chunk2", "chunk3"},
		ChunkDelay:     10 * time.Millisecond,
		TokensPerChunk: 100,
		FinishReason:   "stop",
	}

	resp := NewScriptedResponseBuilder().
		Content("Streaming content").
		StreamConfig(streamConfig).
		Build()

	if resp.StreamConfig == nil {
		t.Error("expected StreamConfig to be set")
	}
	if len(resp.StreamConfig.Chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(resp.StreamConfig.Chunks))
	}
}

// ---------------------------------------------------------------------------
// Helper Function Tests
// ---------------------------------------------------------------------------

func TestNewToolCallResponse(t *testing.T) {
	t.Parallel()

	resp := NewToolCallResponse("read_file", `{"path": "/test.txt"}`)

	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got '%s'", resp.ToolCalls[0].Function.Name)
	}
}

func TestNewStopResponse(t *testing.T) {
	t.Parallel()

	resp := NewStopResponse("Done with the task")

	if resp.Content != "Done with the task" {
		t.Errorf("expected content 'Done with the task', got '%s'", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", resp.FinishReason)
	}
}

func TestNewKeepGoingResponse(t *testing.T) {
	t.Parallel()

	resp := NewKeepGoingResponse("Still working on this")

	if resp.Content != "Still working on this" {
		t.Errorf("expected content 'Still working on this', got '%s'", resp.Content)
	}
	if resp.FinishReason != "" {
		t.Errorf("expected empty finish_reason, got '%s'", resp.FinishReason)
	}
}

func TestNewLengthResponse(t *testing.T) {
	t.Parallel()

	resp := NewLengthResponse("This response was cut off")

	if resp.Content != "This response was cut off" {
		t.Errorf("expected content 'This response was cut off', got '%s'", resp.Content)
	}
	if resp.FinishReason != "length" {
		t.Errorf("expected finish_reason 'length', got '%s'", resp.FinishReason)
	}
}

func TestNewErrorResponse(t *testing.T) {
	t.Parallel()

	testErr := fmt.Errorf("test error")
	resp := NewErrorResponse(testErr)

	if resp.Error == nil {
		t.Error("expected error to be set")
	}
}

func TestNewTimeoutResponse(t *testing.T) {
	t.Parallel()

	resp := NewTimeoutResponse()

	if resp.Error == nil {
		t.Error("expected error to be set")
	}
}

func TestNewRateLimitResponse(t *testing.T) {
	t.Parallel()

	resp := NewRateLimitResponse()

	if resp.RateLimitAfter != 1 {
		t.Errorf("expected RateLimitAfter to be 1, got %d", resp.RateLimitAfter)
	}
}

// ---------------------------------------------------------------------------
// Streaming Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_SendChatRequestStream(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Content: "test response",
			StreamConfig: &StreamConfig{
				Chunks:         []string{"chunk1", "chunk2"},
				ChunkDelay:     5 * time.Millisecond,
				TokensPerChunk: 50,
				FinishReason:   "stop",
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err != nil {
		t.Fatalf("SendChatRequestStream returned error: %v", err)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestScriptedClient_SendChatRequestStream_WithError(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			StreamConfig: &StreamConfig{
				Chunks:           []string{"chunk1"},
				StreamError:      fmt.Errorf("stream error"),
				ErrorAfterChunks: 1,
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err == nil {
		t.Error("expected error from StreamConfig.StreamError")
	}
}

func TestScriptedClient_SendChatRequestStream_ErrorAfterChunks(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			StreamConfig: &StreamConfig{
				Chunks:         []string{"chunk1", "chunk2", "chunk3"},
				ErrorAfterChunks: 2,
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err == nil {
		t.Error("expected error after ErrorAfterChunks")
	}
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks before error, got %d", len(chunks))
	}
}

func TestScriptedClient_SendChatRequestStream_TPSCalculation(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			StreamConfig: &StreamConfig{
				Chunks:         []string{"chunk1", "chunk2"},
				ChunkDelay:     10 * time.Millisecond,
				TokensPerChunk: 100,
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err != nil {
		t.Fatalf("SendChatRequestStream returned error: %v", err)
	}

	// TPS should be calculated: 100 tokens / 0.01s = 10000 TPS
	tps := client.GetLastTPS()
	if tps <= 0 {
		t.Errorf("expected positive TPS, got %f", tps)
	}
}

// ---------------------------------------------------------------------------
// Rate Limit Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_RateLimitAfter(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Content:        "response 1",
			RateLimitAfter: 2,
		},
		&ScriptedResponse{
			Content: "response 2",
		},
	)

	// First request should succeed
	resp1, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if resp1.Choices[0].Message.Content != "response 1" {
		t.Errorf("expected 'response 1', got '%s'", resp1.Choices[0].Message.Content)
	}

	// Second request should trigger rate limit
	_, err = client.SendChatRequest(nil, nil, "", false)
	if err == nil {
		t.Error("expected rate limit error on second request")
	}
	if _, ok := err.(*RateLimitExceededError); !ok {
		t.Errorf("expected RateLimitExceededError, got %T", err)
	}
}

func TestScriptedClient_RateLimitAfterStreaming(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Content:        "stream 1",
			RateLimitAfter: 2,
			StreamConfig: &StreamConfig{
				Chunks: []string{"chunk1"},
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	// First request should succeed
	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Second request should trigger rate limit
	_, err = client.SendChatRequestStream(nil, nil, "", false, callback)
	if err == nil {
		t.Error("expected rate limit error on second streaming request")
	}
}

// ---------------------------------------------------------------------------
// Vision Support Tests
// ---------------------------------------------------------------------------

func TestScriptedClientWithVision(t *testing.T) {
	t.Parallel()

	client := NewScriptedClientWithVision("vision-model",
		&ScriptedResponse{
			Content:    "I see the image",
			VisionOnly: true,
			Images: []api.ImageData{
				{Type: "base64", Base64: "test"},
			},
		},
	)

	resp, err := client.SendVisionRequest(nil, nil, "")
	if err != nil {
		t.Fatalf("SendVisionRequest failed: %v", err)
	}
	if resp.Model != "vision-model" {
		t.Errorf("expected model 'vision-model', got '%s'", resp.Model)
	}
}

func TestScriptedClient_VisionFallback(t *testing.T) {
	t.Parallel()

	client := NewScriptedClientWithVision("vision-model",
		&ScriptedResponse{
			Content:    "vision response",
			VisionOnly: true,
		},
		&ScriptedResponse{
			Content: "regular response",
		},
	)

	// First call should use vision response
	resp1, err := client.SendVisionRequest(nil, nil, "")
	if err != nil {
		t.Fatalf("First SendVisionRequest failed: %v", err)
	}
	if resp1.Model != "vision-model" {
		t.Errorf("expected model 'vision-model', got '%s'", resp1.Model)
	}

	// Second call should fall back to regular response
	resp2, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest failed: %v", err)
	}
	if resp2.Choices[0].Message.Content != "regular response" {
		t.Errorf("expected 'regular response', got '%s'", resp2.Choices[0].Message.Content)
	}
	_ = resp2 // suppress unused warning
}

// ---------------------------------------------------------------------------
// Utility Method Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_LastResponse(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
		&ScriptedResponse{Content: "response 2"},
	)

	// Consume first response
	client.SendChatRequest(nil, nil, "", false)

	last := client.LastResponse()
	if last == nil {
		t.Error("expected LastResponse to return a response")
	}
	if last.Content != "response 1" {
		t.Errorf("expected 'response 1', got '%s'", last.Content)
	}
}

func TestScriptedClient_ResponseHistory(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
		&ScriptedResponse{Content: "response 2"},
		&ScriptedResponse{Content: "response 3"},
	)

	// Consume two responses
	client.SendChatRequest(nil, nil, "", false)
	client.SendChatRequest(nil, nil, "", false)

	history := client.ResponseHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 responses in history, got %d", len(history))
	}
	if history[0].Content != "response 1" {
		t.Errorf("expected 'response 1' in history, got '%s'", history[0].Content)
	}
	if history[1].Content != "response 2" {
		t.Errorf("expected 'response 2' in history, got '%s'", history[1].Content)
	}
}

func TestScriptedClient_ClearHistory(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
		&ScriptedResponse{Content: "response 2"},
	)

	// Consume one response
	client.SendChatRequest(nil, nil, "", false)

	history := client.ResponseHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 response in history, got %d", len(history))
	}

	// Clear history
	client.ClearHistory()

	history = client.ResponseHistory()
	if len(history) != 0 {
		t.Errorf("expected 0 responses in history after ClearHistory, got %d", len(history))
	}
}

func TestScriptedClient_AddResponse(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
	)

	client.AddResponse(&ScriptedResponse{Content: "response 2"})

	if client.Length() != 2 {
		t.Errorf("expected length 2, got %d", client.Length())
	}
}

func TestScriptedClient_SetResponses(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
	)

	newResponses := []*ScriptedResponse{
		{Content: "new 1"},
		{Content: "new 2"},
		{Content: "new 3"},
	}

	client.SetResponses(newResponses)

	if client.Length() != 3 {
		t.Errorf("expected length 3, got %d", client.Length())
	}
	if client.GetIndex() != 0 {
		t.Errorf("expected index 0 after SetResponses, got %d", client.GetIndex())
	}
}

func TestScriptedClient_Reset(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
		&ScriptedResponse{Content: "response 2"},
	)

	// Consume one response
	client.SendChatRequest(nil, nil, "", false)

	client.Reset()

	if client.GetIndex() != 0 {
		t.Errorf("expected index 0 after Reset, got %d", client.GetIndex())
	}
}

func TestScriptedClient_IndexManagement(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "response 1"},
		&ScriptedResponse{Content: "response 2"},
		&ScriptedResponse{Content: "response 3"},
	)

	client.SetIndex(2)
	if client.GetIndex() != 2 {
		t.Errorf("expected index 2, got %d", client.GetIndex())
	}

	// Test negative index handling
	client.SetIndex(-1)
	if client.GetIndex() != 0 {
		t.Errorf("expected index 0 for negative input, got %d", client.GetIndex())
	}

	// Test out of bounds handling
	client.SetIndex(100)
	if client.GetIndex() != 3 {
		t.Errorf("expected index 3 for out of bounds, got %d", client.GetIndex())
	}
}

// ---------------------------------------------------------------------------
// Error Injection Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_ErrorInjection(t *testing.T) {
	t.Parallel()

	testErr := fmt.Errorf("injected error")
	client := NewScriptedClient(
		&ScriptedResponse{
			Error: testErr,
		},
	)

	_, err := client.SendChatRequest(nil, nil, "", false)
	if err == nil {
		t.Error("expected injected error")
	}
	if err != testErr {
		t.Errorf("expected injected error, got %v", err)
	}
}

func TestScriptedClient_SequentialErrors(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Error: fmt.Errorf("error 1")},
		&ScriptedResponse{Error: fmt.Errorf("error 2")},
		&ScriptedResponse{Content: "success"},
	)

	_, err1 := client.SendChatRequest(nil, nil, "", false)
	if err1 == nil {
		t.Error("expected error 1")
	}

	_, err2 := client.SendChatRequest(nil, nil, "", false)
	if err2 == nil {
		t.Error("expected error 2")
	}

	resp, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("expected success on third request, got error: %v", err)
	}
	if resp.Choices[0].Message.Content != "success" {
		t.Errorf("expected 'success', got '%s'", resp.Choices[0].Message.Content)
	}
}

func TestScriptedClient_StreamingErrorInjection(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Error: fmt.Errorf("stream error"),
			StreamConfig: &StreamConfig{
				Chunks: []string{"chunk1"},
			},
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err == nil {
		t.Error("expected error from ScriptedResponse.Error")
	}
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_EmptyResponses(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient()

	resp, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest with empty responses failed: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response")
	}
}

func TestScriptedClient_NilResponse(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(nil)

	resp, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest with nil response failed: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response")
	}
}

func TestScriptedClient_OutOfBounds(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Content: "only response"},
	)

	// Consume the only response
	client.SendChatRequest(nil, nil, "", false)

	// Request beyond available responses
	resp, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest beyond responses failed: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response for out-of-bounds")
	}
}

func TestScriptedClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Content: "delayed response",
			Delay:   5 * time.Second,
		},
	)

	// Cancel the client after a short time
	go func() {
		time.Sleep(10 * time.Millisecond)
		client.Close()
	}()

	_, err := client.SendChatRequest(nil, nil, "", false)
	if err == nil {
		t.Error("expected error from context cancellation")
	}
}

// ---------------------------------------------------------------------------
// TPS Stats Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_TPSStats(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient()

	stats := client.GetTPSStats()
	if len(stats) != 2 {
		t.Errorf("expected 2 TPS stats, got %d", len(stats))
	}
	if stats["last"] != 100.0 {
		t.Errorf("expected default last TPS 100.0, got %f", stats["last"])
	}

	// Reset stats
	client.ResetTPSStats()
	stats = client.GetTPSStats()
	if stats["last"] != 100.0 {
		t.Errorf("expected reset TPS 100.0, got %f", stats["last"])
	}
}

// ---------------------------------------------------------------------------
// Stream Error Index Advancement Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_SendChatRequestStream_StreamErrorAdvancesIndex(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{Error: fmt.Errorf("stream error")},
		&ScriptedResponse{Content: "retry content", FinishReason: "stop"},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	// First call should fail
	_, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err == nil {
		t.Error("expected error from first call")
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks on error, got %d", len(chunks))
	}

	// Second call should succeed (index was advanced past the error)
	resp, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if resp.Choices[0].Message.Content != "retry content" {
		t.Errorf("expected 'retry content', got '%s'", resp.Choices[0].Message.Content)
	}
}

// ---------------------------------------------------------------------------
// Stream Response Content Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_SendChatRequestStream_IncludesToolCalls(t *testing.T) {
	t.Parallel()

	toolCall := api.ToolCall{
		ID:   "call_stream_123",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "shell_command",
			Arguments: `{"command": "ls"}`,
		},
	}

	client := NewScriptedClient(
		&ScriptedResponse{
			Content:      "Running command",
			ToolCalls:    []api.ToolCall{toolCall},
			FinishReason: "tool_calls",
		},
	)

	var chunks []string
	callback := func(chunk, role string) {
		chunks = append(chunks, chunk)
	}

	resp, err := client.SendChatRequestStream(nil, nil, "", false, callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "shell_command" {
		t.Errorf("expected tool name 'shell_command', got '%s'", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
	if resp.Choices[0].Message.Content != "Running command" {
		t.Errorf("expected content 'Running command', got '%s'", resp.Choices[0].Message.Content)
	}
}

func TestScriptedClient_SendChatRequestStream_IncludesReasoningContent(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(
		&ScriptedResponse{
			Content:          "Final answer",
			ReasoningContent: "Let me think about this...",
			FinishReason:     "stop",
		},
	)

	resp, err := client.SendChatRequestStream(nil, nil, "", false, func(chunk, role string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.ReasoningContent != "Let me think about this..." {
		t.Errorf("expected reasoning content 'Let me think about this...', got '%s'", resp.Choices[0].Message.ReasoningContent)
	}
}

// ---------------------------------------------------------------------------
// Prompt Tokens Details Tests
// ---------------------------------------------------------------------------

func TestScriptedClient_PromptTokensDetails(t *testing.T) {
	t.Parallel()

	cacheWriteTokens := 42
	client := NewScriptedClient(
		&ScriptedResponse{
			Content: "test",
			Usage: ScriptedTokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
				EstimatedCost:    0.001,
				PromptTokensDetails: PromptTokensDetails{
					CachedTokens:     30,
					CacheWriteTokens: &cacheWriteTokens,
				},
			},
		},
	)

	resp, err := client.SendChatRequest(nil, nil, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("expected 30 cached tokens, got %d", resp.Usage.PromptTokensDetails.CachedTokens)
	}
	if resp.Usage.PromptTokensDetails.CacheWriteTokens == nil || *resp.Usage.PromptTokensDetails.CacheWriteTokens != 42 {
		t.Errorf("expected cache write tokens 42, got %v", resp.Usage.PromptTokensDetails.CacheWriteTokens)
	}
}

func TestScriptedClient_SendChatRequestStream_PromptTokensDetails(t *testing.T) {
	t.Parallel()

	cacheWriteTokens := 99
	client := NewScriptedClient(
		&ScriptedResponse{
			Content: "streamed",
			Usage: ScriptedTokenUsage{
				PromptTokens:     200,
				CompletionTokens: 100,
				TotalTokens:      300,
				EstimatedCost:    0.002,
				PromptTokensDetails: PromptTokensDetails{
					CachedTokens:     80,
					CacheWriteTokens: &cacheWriteTokens,
				},
			},
			StreamConfig: &StreamConfig{
				Chunks: []string{"chunk1", "chunk2"},
			},
		},
	)

	resp, err := client.SendChatRequestStream(nil, nil, "", false, func(chunk, role string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.PromptTokens != 200 {
		t.Errorf("expected 200 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.PromptTokensDetails.CachedTokens != 80 {
		t.Errorf("expected 80 cached tokens, got %d", resp.Usage.PromptTokensDetails.CachedTokens)
	}
	if resp.Usage.PromptTokensDetails.CacheWriteTokens == nil || *resp.Usage.PromptTokensDetails.CacheWriteTokens != 99 {
		t.Errorf("expected cache write tokens 99, got %v", resp.Usage.PromptTokensDetails.CacheWriteTokens)
	}
}
