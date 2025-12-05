package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestProcessResponsePreservesToolOutputForLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:          newStubClient("openrouter", "openai/gpt-4o-mini"),
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

	prepared := handler.prepareMessages()
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
		client:          newStubClient("openrouter", "openai/gpt-4o-mini"),
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
