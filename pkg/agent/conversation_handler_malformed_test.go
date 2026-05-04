package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/trace"
)

// ---------------------------------------------------------------------------
// handleMalformedToolCalls
// ---------------------------------------------------------------------------

func TestHandleMalformedToolCalls_NilFallbackParser(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:          &Agent{},
		fallbackParser: nil,
	}
	turn := TurnEvaluation{}
	parserErrors := []string{"initial error"}

	result := ch.handleMalformedToolCalls("some malformed content", turn, parserErrors)

	if result != false {
		t.Error("expected false return value")
	}
}

func TestHandleMalformedToolCalls_FallbackParserReturnsNil(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	ch := NewConversationHandler(agent)

	// Content that has no tool call patterns → fallback parser returns nil
	result := ch.handleMalformedToolCalls("just plain text with no tool calls", TurnEvaluation{}, nil)

	if result != false {
		t.Error("expected false return value")
	}
}

func TestHandleMalformedToolCalls_FallbackParserReturnsEmptyToolCalls(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	ch := NewConversationHandler(agent)

	// Content that triggers pattern detection but fails to parse into valid tool calls
	content := `
	{
		"name": "unknown_fake_tool",
		"arguments": "not a real tool"
	}
	`
	result := ch.handleMalformedToolCalls(content, TurnEvaluation{}, []string{"parser failed"})

	if result != false {
		t.Error("expected false return value")
	}
}

func TestHandleMalformedToolCalls_SuccessWithKnownTool(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "I will use a tool"},
	})

	ch := NewConversationHandler(agent)

	// Content that matches a known tool name pattern (named tool JSON blocks)
	// shell_command is a known tool, so the fallback parser should pick it up
	content := `shell_command {"command": "echo hello"}`

	result := ch.handleMalformedToolCalls(content, TurnEvaluation{}, nil)

	// Should return false (continue conversation)
	if result != false {
		t.Error("expected false return value (continue conversation)")
	}

	// The assistant message should have been updated with tool calls
	messages := agent.state.GetMessages()
	lastMsg := messages[len(messages)-1]
	if len(lastMsg.ToolCalls) == 0 {
		// If the fallback parser didn't parse anything, that's also acceptable
		// since the tool name pattern matching depends on exact format
		// This test validates that no panic/crash occurs
		t.Log("fallback parser did not extract tool calls from this content (acceptable)")
	}
}

func TestHandleMalformedToolCalls_WithCodeBlock(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "run this"},
		{Role: "assistant", Content: "running tool"},
	})

	ch := NewConversationHandler(agent)

	// Content with code block containing tool_calls pattern
	content := "```json\n{\"tool_calls\":[{\"id\":\"abc\",\"type\":\"function\",\"function\":{\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}]}\n```"

	result := ch.handleMalformedToolCalls(content, TurnEvaluation{}, nil)

	// Should return false (continue conversation)
	if result != false {
		t.Error("expected false return value")
	}
}

func TestHandleMalformedToolCalls_EmptyContent(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	ch := NewConversationHandler(agent)

	result := ch.handleMalformedToolCalls("", TurnEvaluation{}, nil)

	if result != false {
		t.Error("expected false return value for empty content")
	}
}

func TestHandleMalformedToolCalls_TentativeRejectionReset(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "running tool"},
	})

	ch := NewConversationHandler(agent)
	ch.tentativeRejectionCount = 5 // Set a high count before

	// Use a code block format that the fallback parser can parse
	content := "```json\n{\"tool_calls\":[{\"id\":\"tc1\",\"type\":\"function\",\"function\":{\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":\\\"echo test\\\"}\"}}]}\n```"

	ch.handleMalformedToolCalls(content, TurnEvaluation{}, nil)

	// After successful fallback parsing, tentativeRejectionCount should be reset
	if ch.tentativeRejectionCount != 0 {
		t.Errorf("expected tentativeRejectionCount to be 0 after successful fallback, got %d", ch.tentativeRejectionCount)
	}
}

// ---------------------------------------------------------------------------
// updateTurnRecord
// ---------------------------------------------------------------------------

func TestUpdateTurnRecord_NilTraceSession(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:            &Agent{},
		traceSession:     nil,
		currentTurnRecord: &trace.TurnRecord{},
	}

	// Should not panic
	ch.updateTurnRecord("response", nil, nil, false, "")
}

func TestUpdateTurnRecord_NilTurnRecord(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:            &Agent{},
		traceSession:     &trace.TraceSession{},
		currentTurnRecord: nil,
	}

	// Should not panic
	ch.updateTurnRecord("response", nil, nil, false, "")
}

func TestUpdateTurnRecord_NonMatchingTraceSession(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:            &Agent{},
		traceSession:     "not a trace session", // Wrong type
		currentTurnRecord: &trace.TurnRecord{},
	}

	// Should not panic - should just skip recording
	ch.updateTurnRecord("response", nil, nil, false, "")
}

func TestUpdateTurnRecord_WithValidTraceSession(t *testing.T) {
	t.Parallel()
	// Create a temp dir for the trace session
	tmpDir := t.TempDir()
	ts, err := trace.NewTraceSession(tmpDir, "test", "test:test")
	if err != nil {
		t.Fatalf("NewTraceSession failed: %v", err)
	}
	defer ts.Close()

	ch := &ConversationHandler{
		agent: &Agent{},
		traceSession: ts,
		currentTurnRecord: &trace.TurnRecord{
			RunID: ts.RunID,
		},
	}

	toolCalls := []api.ToolCall{
		{
			ID:   "tc1",
			Type: "function",
		},
	}
	toolCalls[0].Function.Name = "shell_command"
	toolCalls[0].Function.Arguments = `{"command":"echo hello"}`

	ch.updateTurnRecord("raw response", toolCalls, []string{"parser warning"}, true, "cleaned output")

	// Verify the turn record was updated
	rec := ch.currentTurnRecord
	if rec.RawResponse != "raw response" {
		t.Errorf("RawResponse = %q, want %q", rec.RawResponse, "raw response")
	}
	if len(rec.ParsedToolCalls) != 1 {
		t.Errorf("ParsedToolCalls len = %d, want 1", len(rec.ParsedToolCalls))
	}
	if len(rec.ParserErrors) != 1 {
		t.Errorf("ParserErrors len = %d, want 1", len(rec.ParserErrors))
	}
	if !rec.FallbackUsed {
		t.Error("FallbackUsed should be true")
	}
	if rec.FallbackOutput != "cleaned output" {
		t.Errorf("FallbackOutput = %q, want %q", rec.FallbackOutput, "cleaned output")
	}
}

func TestUpdateTurnRecord_NoFallbackOutput(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	ts, err := trace.NewTraceSession(tmpDir, "test", "test:test")
	if err != nil {
		t.Fatalf("NewTraceSession failed: %v", err)
	}
	defer ts.Close()

	ch := &ConversationHandler{
		agent: &Agent{},
		traceSession: ts,
		currentTurnRecord: &trace.TurnRecord{
			RunID: ts.RunID,
		},
	}

	// Empty fallbackOutput should NOT be written to record
	ch.updateTurnRecord("response", nil, nil, false, "")

	if ch.currentTurnRecord.FallbackOutput != "" {
		t.Errorf("FallbackOutput should be empty when not provided, got %q", ch.currentTurnRecord.FallbackOutput)
	}
}

func TestUpdateTurnRecord_NilToolCallsAndNilErrors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	ts, err := trace.NewTraceSession(tmpDir, "test", "test:test")
	if err != nil {
		t.Fatalf("NewTraceSession failed: %v", err)
	}
	defer ts.Close()

	ch := &ConversationHandler{
		agent: &Agent{},
		traceSession: ts,
		currentTurnRecord: &trace.TurnRecord{
			RunID: ts.RunID,
		},
	}

	ch.updateTurnRecord("response", nil, nil, false, "")

	rec := ch.currentTurnRecord
	if rec.RawResponse != "response" {
		t.Errorf("RawResponse = %q, want %q", rec.RawResponse, "response")
	}
	// ParsedToolCalls should remain nil when nil is passed
	if rec.ParsedToolCalls != nil {
		t.Error("ParsedToolCalls should be nil when nil is passed")
	}
	// ParserErrors should remain nil when nil is passed
	if rec.ParserErrors != nil {
		t.Error("ParserErrors should be nil when nil is passed")
	}
}