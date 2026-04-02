package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestProcessResponsePreservesToolOutputForLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:          NewScriptedClient(),
		systemPrompt:    "system",
		messages:        []api.Message{{Role: "user", Content: "Show the first line of README.md"}},
		interruptCtx:    ctx,
		interruptCancel: cancel,
		outputMutex:     &sync.Mutex{},
	}

	handler := NewConversationHandler(agent)

	// Create a temporary file so the read_file tool succeeds
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(tempFile, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// Build a fake tool call from the model
	toolCall := api.ToolCall{
		Type: "function",
	}
	toolCall.Function.Name = "read_file"
	args := map[string]interface{}{
		"file_path":  tempFile,
		"start_line": 1,
		"end_line":   1,
	}
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal tool args: %v", err)
	}
	toolCall.Function.Arguments = string(payload)

	resp := &api.ChatResponse{
		Choices: []api.Choice{{}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.Content = "Let me read the file first."
	resp.Choices[0].Message.ToolCalls = []api.ToolCall{toolCall}

	stopped := handler.processResponse(resp)
	if stopped {
		t.Fatalf("expected conversation to continue after tool call execution")
	}

	if len(agent.messages) < 3 {
		t.Fatalf("expected at least user, assistant, and tool messages, got %d", len(agent.messages))
	}

	// Debug: Print all messages to understand the structure
	t.Logf("All messages (%d):", len(agent.messages))
	for i, msg := range agent.messages {
		t.Logf("  [%d] Role: %s, ToolCalls: %d", i, msg.Role, len(msg.ToolCalls))
		if len(msg.ToolCalls) > 0 {
			for j, tc := range msg.ToolCalls {
				t.Logf("    [%d] ID: %s, Function: %s", j, tc.ID, tc.Function.Name)
			}
		}
	}

	// Find the assistant message with tool calls (it should be before the tool result and summary)
	var assistantMsg *api.Message
	for i := len(agent.messages) - 1; i >= 0; i-- {
		if agent.messages[i].Role == "assistant" && len(agent.messages[i].ToolCalls) > 0 {
			assistantMsg = &agent.messages[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatalf("could not find assistant message with tool calls")
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected assistant message to retain tool_calls metadata, got %d entries", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].ID == "" {
		t.Fatalf("expected missing tool call IDs to be generated and recorded")
	}

	prepared := handler.prepareMessages(nil)
	foundTool := false
	for _, msg := range prepared {
		if msg.Role == "tool" {
			foundTool = true
			if msg.ToolCallId != assistantMsg.ToolCalls[0].ID {
				t.Fatalf("tool message tool_call_id mismatch: got %s want %s", msg.ToolCallId, assistantMsg.ToolCalls[0].ID)
			}
			break
		}
	}

	if !foundTool {
		t.Fatalf("expected tool output to survive sanitization so it can be sent back to the LLM")
	}
}

func TestProcessResponseDeduplicatesDuplicateToolCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:          NewScriptedClient(),
		systemPrompt:    "system",
		messages:        []api.Message{{Role: "user", Content: "Read a sample file"}},
		interruptCtx:    ctx,
		interruptCancel: cancel,
		outputMutex:     &sync.Mutex{},
	}

	handler := NewConversationHandler(agent)

	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(tempFile, []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	args := map[string]interface{}{
		"file_path":  tempFile,
		"start_line": 1,
		"end_line":   1,
	}
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	toolCallA := api.ToolCall{
		ID:   "call_sample_read_a",
		Type: "function",
	}
	toolCallA.Function.Name = "read_file"
	toolCallA.Function.Arguments = string(payload)

	toolCallB := toolCallA
	toolCallB.ID = "call_sample_read_b"

	// Simulate duplicate entries (as seen when some providers stream tool_calls repeatedly with fresh IDs)
	resp := &api.ChatResponse{
		Choices: []api.Choice{{}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.ToolCalls = []api.ToolCall{toolCallA, toolCallB}

	stopped := handler.processResponse(resp)
	if stopped {
		t.Fatalf("expected conversation to continue after deduplicating tool calls")
	}

	// Ensure only one tool message was added
	toolMessages := 0
	for _, msg := range agent.messages {
		if msg.Role == "tool" {
			toolMessages++
		}
	}
	if toolMessages != 1 {
		t.Fatalf("expected exactly one tool result message, got %d", toolMessages)
	}

	// Find the assistant message with tool calls (it should be before the tool result and summary)
	var assistantMsg *api.Message
	for i := len(agent.messages) - 1; i >= 0; i-- {
		if agent.messages[i].Role == "assistant" && len(agent.messages[i].ToolCalls) > 0 {
			assistantMsg = &agent.messages[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatalf("could not find assistant message with tool calls")
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected assistant message to keep only one tool_call entry, got %d", len(assistantMsg.ToolCalls))
	}
}

func TestProcessResponseDoesNotExecuteIrreparableStructuredToolCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:          NewScriptedClient(),
		systemPrompt:    "system",
		messages:        []api.Message{{Role: "user", Content: "Delegate this to a subagent"}},
		interruptCtx:    ctx,
		interruptCancel: cancel,
		outputMutex:     &sync.Mutex{},
	}

	handler := NewConversationHandler(agent)

	toolCall := api.ToolCall{
		ID:   "call_subagent",
		Type: "function",
	}
	toolCall.Function.Name = "run_subagent"
	toolCall.Function.Arguments = `{"prompt":"review the diff","persona":"code_reviewer`

	resp := &api.ChatResponse{
		Choices: []api.Choice{{FinishReason: "length"}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.ToolCalls = []api.ToolCall{toolCall}

	stopped := handler.processResponse(resp)
	if stopped {
		t.Fatalf("expected conversation to continue so the model can re-emit the tool call")
	}

	for _, msg := range agent.messages {
		if msg.Role == "tool" {
			t.Fatalf("expected malformed structured tool call not to execute, found tool result: %#v", msg)
		}
	}

	last := agent.messages[len(agent.messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected assistant message to remain last, got %s", last.Role)
	}
	if len(last.ToolCalls) != 0 {
		t.Fatalf("expected malformed tool calls to be cleared from history, got %d", len(last.ToolCalls))
	}

	prepared := handler.prepareMessages(nil)
	foundReminder := false
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "incomplete or invalid JSON") {
			foundReminder = true
			break
		}
	}
	if !foundReminder {
		t.Fatalf("expected reminder asking the model to re-emit valid JSON tool arguments")
	}
}

// TestNormalizeToolCallsForExecution_TypeFieldNormalization tests that the normalizeToolCallsForExecution
// function properly validates and normalizes the Type field of tool calls to always be "function".
func TestNormalizeToolCallsForExecution_TypeFieldNormalization(t *testing.T) {
	tests := []struct {
		name          string
		toolCalls     []api.ToolCall
		wantNormalized int    // expected number of normalized tool calls
		wantMalformed  int    // expected number of malformed tool calls
		wantTypeValues map[int]string // map of index -> expected Type value in normalized slice
	}{
		{
			name: "Type='function' remains unchanged",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
			},
			wantNormalized: 1,
			wantMalformed:  0,
			wantTypeValues: map[int]string{0: "function"},
		},
		{
			name: "Type='' (empty) should be normalized to 'function'",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
			},
			wantNormalized: 1,
			wantMalformed:  0,
			wantTypeValues: map[int]string{0: "function"},
		},
		{
			name: "Type='invalid' should be normalized to 'function'",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "invalid",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
			},
			wantNormalized: 1,
			wantMalformed:  0,
			wantTypeValues: map[int]string{0: "function"},
		},
		{
			name: "Type='xyz' should be normalized to 'function'",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "xyz",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "write_file",
						Arguments: `{"file_path":"test.txt","content":"hello"}`,
					},
				},
			},
			wantNormalized: 1,
			wantMalformed:  0,
			wantTypeValues: map[int]string{0: "function"},
		},
		{
			name: "Multiple tool calls with various Type values",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
				{
					ID:   "call_2",
					Type: "",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "write_file",
						Arguments: `{"file_path":"out.txt","content":"data"}`,
					},
				},
				{
					ID:   "call_3",
					Type: "invalid",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "shell",
						Arguments: `{"command":"ls"}`,
					},
				},
			},
			wantNormalized: 3,
			wantMalformed:  0,
			wantTypeValues: map[int]string{0: "function", 1: "function", 2: "function"},
		},
		{
			name: "Mix of valid and malformed (invalid JSON) tool calls",
			toolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
				{
					ID:   "call_2",
					Type: "",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "write_file",
						Arguments: `invalid json`, // This should go to malformed
					},
				},
			},
			wantNormalized: 1,
			wantMalformed:  1,
			wantTypeValues: map[int]string{0: "function"},
		},
		{
			name: "Empty input returns nil slices",
			toolCalls:     []api.ToolCall{},
			wantNormalized: 0,
			wantMalformed:  0,
			wantTypeValues: map[int]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, malformed := normalizeToolCallsForExecution(tt.toolCalls)

			if len(normalized) != tt.wantNormalized {
				t.Errorf("normalizeToolCallsForExecution() returned %d normalized calls, want %d",
					len(normalized), tt.wantNormalized)
			}

			if len(malformed) != tt.wantMalformed {
				t.Errorf("normalizeToolCallsForExecution() returned %d malformed calls, want %d",
					len(malformed), tt.wantMalformed)
			}

			// Verify that all normalized tool calls have Type = "function"
			for i, tc := range normalized {
				if tc.Type != "function" {
					t.Errorf("normalized tool call at index %d has Type=%q, want 'function'", i, tc.Type)
				}
			}

			// Verify specific Type values if expected
			for idx, expectedType := range tt.wantTypeValues {
				if idx >= len(normalized) {
					t.Errorf("wantTypeValues specifies index %d but normalized slice only has %d elements",
						idx, len(normalized))
					continue
				}
				if normalized[idx].Type != expectedType {
					t.Errorf("normalized[%d].Type = %q, want %q", idx, normalized[idx].Type, expectedType)
				}
			}
		})
	}
}

// TestNormalizeToolCallsForExecution_PreservesOtherFields verifies that Type normalization
// doesn't modify other fields of the tool call.
func TestNormalizeToolCallsForExecution_PreservesOtherFields(t *testing.T) {
	toolCalls := []api.ToolCall{
		{
			ID:   "call_abc123",
			Type: "", // Should be normalized to "function"
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: `{"file_path":"sample.txt","start_line":1,"end_line":10}`,
			},
		},
	}

	normalized, malformed := normalizeToolCallsForExecution(toolCalls)

	if len(malformed) != 0 {
		t.Errorf("expected 0 malformed calls, got %d", len(malformed))
	}
	if len(normalized) != 1 {
		t.Fatalf("expected 1 normalized call, got %d", len(normalized))
	}

	tc := normalized[0]

	// Verify Type was normalized
	if tc.Type != "function" {
		t.Errorf("Type = %q, want 'function'", tc.Type)
	}

	// Verify other fields are preserved
	if tc.ID != "call_abc123" {
		t.Errorf("ID = %q, want 'call_abc123'", tc.ID)
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("Function.Name = %q, want 'read_file'", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"file_path":"sample.txt","start_line":1,"end_line":10}` {
		t.Errorf("Function.Arguments = %q, want original value", tc.Function.Arguments)
	}
}

// TestNormalizeToolCallsForExecution_TypeNormalizationIsPrioritized verifies that
// Type normalization happens regardless of other factors (as long as JSON is valid).
func TestNormalizeToolCallsForExecution_TypeNormalizationIsPrioritized(t *testing.T) {
	// Test with various invalid Type values but valid JSON
	invalidTypes := []string{"", "invalid", "xyz", "NOT_A_FUNCTION", "123", "  ", "\t\n"}

	for _, invalidType := range invalidTypes {
		t.Run(invalidType, func(t *testing.T) {
			toolCalls := []api.ToolCall{
				{
					ID:   "call_1",
					Type: invalidType,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "test_tool",
						Arguments: `{"arg":"value"}`,
					},
				},
			}

			normalized, malformed := normalizeToolCallsForExecution(toolCalls)

			if len(malformed) != 0 {
				t.Errorf("expected 0 malformed calls for Type=%q, got %d", invalidType, len(malformed))
			}
			if len(normalized) != 1 {
				t.Fatalf("expected 1 normalized call, got %d", len(normalized))
			}

			if normalized[0].Type != "function" {
				t.Errorf("Type=%q was not normalized to 'function', got %q", invalidType, normalized[0].Type)
			}
		})
	}
}
