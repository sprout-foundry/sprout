package test

import (
	"context"
	"testing"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/events"
)

// --- Tool lifecycle event tests (tool_start + tool_end) ---

func TestE2E_ToolLifecycle_EventsPublished(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider returns a tool call, then a final answer
	h.Provider().AddToolCallResponse(
		"Reading file...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("The file contains 'hello'.")
	h.Executor().AddToolResult("call_1", "hello")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Read test.txt")
	h.AssertNoError(err)

	// Both tool_start and tool_end should be published
	h.AssertEventPublished(events.EventTypeToolStart)
	h.AssertEventPublished(events.EventTypeToolEnd)
}

func TestE2E_ToolEndEvent_ContainsCorrectFields(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Searching...",
		core.ToolCall{
			ID: "call_abc",
			Function: core.ToolCallFunction{
				Name:      "search",
				Arguments: `{"query":"test"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")
	h.Executor().AddToolResult("call_abc", "search results found")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Search for something")
	h.AssertNoError(err)

	// Find tool_start events
	startEvents := h.FindEvents(events.EventTypeToolStart)
	if len(startEvents) != 1 {
		t.Fatalf("expected 1 tool_start event, got %d", len(startEvents))
	}
	startData := startEvents[0].Data.(map[string]interface{})
	if startData["tool_name"] != "search" {
		t.Errorf("expected tool_name='search', got %v", startData["tool_name"])
	}
	if startData["tool_call_id"] != "call_abc" {
		t.Errorf("expected tool_call_id='call_abc', got %v", startData["tool_call_id"])
	}

	// Find tool_end events
	endEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(endEvents) != 1 {
		t.Fatalf("expected 1 tool_end event, got %d", len(endEvents))
	}
	endData := endEvents[0].Data.(map[string]interface{})

	// Verify required fields
	if endData["tool_call_id"] != "call_abc" {
		t.Errorf("expected tool_call_id='call_abc', got %v", endData["tool_call_id"])
	}
	if endData["tool_name"] != "search" {
		t.Errorf("expected tool_name='search', got %v", endData["tool_name"])
	}
	if endData["status"] != "completed" {
		t.Errorf("expected status='completed', got %v", endData["status"])
	}
	if endData["result"] != "search results found" {
		t.Errorf("expected result='search results found', got %v", endData["result"])
	}

	// Duration should be tracked (non-negative)
	durationMs, ok := endData["duration_ms"]
	if !ok {
		t.Error("expected duration_ms field in tool_end event")
	}
	if durationMs.(int64) < 0 {
		t.Errorf("expected non-negative duration_ms, got %d", durationMs.(int64))
	}
}

func TestE2E_ToolEndEvent_MultipleTools(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse("Working...",
		core.ToolCall{
			ID:       "call_1",
			Function: core.ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`},
		},
		core.ToolCall{
			ID:       "call_2",
			Function: core.ToolCallFunction{Name: "shell", Arguments: `{"cmd":"pwd"}`},
		},
	)
	h.Provider().AddTextResponse("Done.")
	h.Executor().AddToolResult("call_1", "file.txt")
	h.Executor().AddToolResult("call_2", "/home")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Run ls and pwd")
	h.AssertNoError(err)

	// Should have 2 tool_start and 2 tool_end events
	startEvents := h.FindEvents(events.EventTypeToolStart)
	if len(startEvents) != 2 {
		t.Fatalf("expected 2 tool_start events, got %d", len(startEvents))
	}
	endEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(endEvents) != 2 {
		t.Fatalf("expected 2 tool_end events, got %d", len(endEvents))
	}

	// Verify each tool_end maps to its tool call
	endByID := make(map[string]map[string]interface{})
	for _, ev := range endEvents {
		data := ev.Data.(map[string]interface{})
		id := data["tool_call_id"].(string)
		endByID[id] = data
	}

	if endByID["call_1"]["tool_name"] != "shell" {
		t.Errorf("expected call_1 tool_name='shell', got %v", endByID["call_1"]["tool_name"])
	}
	if endByID["call_1"]["status"] != "completed" {
		t.Errorf("expected call_1 status='completed', got %v", endByID["call_1"]["status"])
	}
	if endByID["call_2"]["tool_name"] != "shell" {
		t.Errorf("expected call_2 tool_name='shell', got %v", endByID["call_2"]["tool_name"])
	}
}

func TestE2E_ToolEndEvent_MissingResult_DefensivePublish(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse("Working...",
		core.ToolCall{
			ID:       "call_1",
			Function: core.ToolCallFunction{Name: "read_file", Arguments: `{}`},
		},
		core.ToolCall{
			ID:       "call_2",
			Function: core.ToolCallFunction{Name: "write_file", Arguments: `{}`},
		},
	)
	h.Provider().AddTextResponse("Done.")

	// Use a custom executor that returns only one result for call_1, omitting call_2
	partialExecutor := &partialMockExecutor{
		results: []core.Message{
			{Role: "tool", Content: "file content", ToolCallID: "call_1"},
		},
	}

	agent, err := core.NewAgent(core.Options{
		Provider:       h.Provider(),
		Executor:       partialExecutor,
		UI:             h.UI(),
		EventPublisher: h.EventBus(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Run(context.Background(), "Do things")
	h.AssertNoError(err)

	// Should still get 2 tool_end events
	endEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(endEvents) != 2 {
		t.Fatalf("expected 2 tool_end events (including synthetic recovery), got %d", len(endEvents))
	}

	endByID := make(map[string]map[string]interface{})
	for _, ev := range endEvents {
		data := ev.Data.(map[string]interface{})
		id := data["tool_call_id"].(string)
		endByID[id] = data
	}

	// call_1 should be completed with result
	if endByID["call_1"]["status"] != "completed" {
		t.Errorf("expected call_1 status='completed', got %v", endByID["call_1"]["status"])
	}

	// call_2 should have error status (synthetic result from threading recovery)
	if endByID["call_2"]["status"] != core.ToolStatusError {
		t.Errorf("expected call_2 status=%q, got %v", core.ToolStatusError, endByID["call_2"]["status"])
	}
	// The synthetic result is published through the normal tool_end path (which
	// sets "result" with the synthetic content), not the defensive publish path
	// (which sets "error"). Either is acceptable — the key invariant is that a
	// tool_end event exists for call_2 with error status.
	if endByID["call_2"]["error"] == nil && endByID["call_2"]["result"] == nil {
		t.Error("expected call_2 to have an error or result field")
	}
}

// partialMockExecutor returns a fixed set of results regardless of how many calls come in.
type partialMockExecutor struct {
	results []core.Message
}

func (m *partialMockExecutor) GetTools() []core.Tool { return nil }
func (m *partialMockExecutor) Execute(_ context.Context, _ []core.ToolCall) []core.Message {
	return m.results
}

func TestE2E_ToolEndEvent_NoToolCalls_NoEvents(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("No tools needed.")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Just chat")
	h.AssertNoError(err)

	// No tool events should be published
	startEvents := h.FindEvents(events.EventTypeToolStart)
	endEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(startEvents) != 0 {
		t.Errorf("expected 0 tool_start events, got %d", len(startEvents))
	}
	if len(endEvents) != 0 {
		t.Errorf("expected 0 tool_end events, got %d", len(endEvents))
	}
}

func TestE2E_ToolEndEvent_EmptyResultContent_StillCompleted(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse("Trying...",
		core.ToolCall{
			ID:       "call_1",
			Function: core.ToolCallFunction{Name: "mkdir", Arguments: `{}`},
		},
	)
	h.Provider().AddTextResponse("Directory created.")
	// Executor returns empty content result (successful tool that produces no output)
	h.Executor().AddToolResult("call_1", "")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Create directory")
	h.AssertNoError(err)

	endEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(endEvents) != 1 {
		t.Fatalf("expected 1 tool_end event, got %d", len(endEvents))
	}
	endData := endEvents[0].Data.(map[string]interface{})
	// Empty content does NOT mean failure — the tool executed successfully
	if endData["status"] != "completed" {
		t.Errorf("expected status='completed' for empty result, got %v", endData["status"])
	}
}
