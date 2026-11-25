package test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/events"
)

// --- Tool Lifecycle Event Tests ---

func TestE2E_ToolLifecycleEvents_SingleTool(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider returns a tool call, then a final text response
	h.Provider().AddToolCallResponse(
		"Let me check that.",
		core.ToolCall{
			ID: "call_abc123",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/tmp/test.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("The file contains 'hello world'.")

	h.Executor().AddToolResult("call_abc123", "hello world")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read /tmp/test.txt")
	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains 'hello world'.")

	// --- Assert tool_start event ---
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 1 {
		h.fail("expected 1 tool_start event, got %d", len(toolStartEvents))
	}

	startEvt := toolStartEvents[0]
	startData, ok := startEvt.Data.(map[string]interface{})
	if !ok {
		h.fail("tool_start event data is not a map")
	}

	if startData["tool_name"] != "read_file" {
		h.fail("expected tool_name 'read_file', got %v", startData["tool_name"])
	}
	if startData["tool_call_id"] != "call_abc123" {
		h.fail("expected tool_call_id 'call_abc123', got %v", startData["tool_call_id"])
	}
	if startData["arguments"] != `{"path":"/tmp/test.txt"}` {
		h.fail("expected arguments JSON, got %v", startData["arguments"])
	}
	if startData["tool_index"] != 0 {
		h.fail("expected tool_index 0, got %v", startData["tool_index"])
	}

	// --- Assert tool_end event ---
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 1 {
		h.fail("expected 1 tool_end event, got %d", len(toolEndEvents))
	}

	endEvt := toolEndEvents[0]
	endData, ok := endEvt.Data.(map[string]interface{})
	if !ok {
		h.fail("tool_end event data is not a map")
	}

	if endData["tool_call_id"] != "call_abc123" {
		h.fail("expected tool_call_id 'call_abc123', got %v", endData["tool_call_id"])
	}
	if endData["tool_name"] != "read_file" {
		h.fail("expected tool_name 'read_file', got %v", endData["tool_name"])
	}
	if endData["status"] != "completed" {
		h.fail("expected status 'completed', got %v", endData["status"])
	}
	if endData["result"] != "hello world" {
		h.fail("expected result 'hello world', got %v", endData["result"])
	}

	// --- Assert ordering: tool_start at or before tool_end ---
	if startEvt.Timestamp.After(endEvt.Timestamp) {
		h.fail("expected tool_start before tool_end, but start=%v end=%v",
			startEvt.Timestamp, endEvt.Timestamp)
	}
}

func TestE2E_ToolLifecycleEvents_MultipleTools(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider returns two tool calls in one response
	h.Provider().AddToolCallResponse(
		"Running both commands.",
		core.ToolCall{
			ID: "call_ls",
			Function: core.ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"ls"}`,
			},
		},
		core.ToolCall{
			ID: "call_pwd",
			Function: core.ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"pwd"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done with both commands.")

	h.Executor().AddToolResult("call_ls", "file1.txt\nfile2.txt")
	h.Executor().AddToolResult("call_pwd", "/home/user")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Run ls and pwd")
	h.AssertNoError(err)
	h.AssertEquals(result, "Done with both commands.")

	// --- Assert 2 tool_start events ---
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 2 {
		h.fail("expected 2 tool_start events, got %d", len(toolStartEvents))
	}

	// Extract tool_call_ids from start events
	startIDs := make([]string, len(toolStartEvents))
	for i, evt := range toolStartEvents {
		data := evt.Data.(map[string]interface{})
		startIDs[i] = data["tool_call_id"].(string)
	}

	// Verify both tool calls were started
	sort.Strings(startIDs)
	if startIDs[0] != "call_ls" || startIDs[1] != "call_pwd" {
		h.fail("expected start IDs [call_ls, call_pwd], got %v", startIDs)
	}

	// Verify tool_index values
	for _, evt := range toolStartEvents {
		data := evt.Data.(map[string]interface{})
		idx, ok := data["tool_index"].(int)
		if !ok {
			h.fail("unexpected tool_index type %T", data["tool_index"])
			continue
		}
		if idx != 0 && idx != 1 {
			h.fail("unexpected tool_index %d", idx)
		}
	}

	// --- Assert 2 tool_end events ---
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 2 {
		h.fail("expected 2 tool_end events, got %d", len(toolEndEvents))
	}

	// Build a map of tool_call_id -> result for verification
	endResults := make(map[string]string)
	for _, evt := range toolEndEvents {
		data := evt.Data.(map[string]interface{})
		id := data["tool_call_id"].(string)
		result := data["result"].(string)
		endResults[id] = result
	}

	if endResults["call_ls"] != "file1.txt\nfile2.txt" {
		h.fail("expected ls result, got %q", endResults["call_ls"])
	}
	if endResults["call_pwd"] != "/home/user" {
		h.fail("expected pwd result, got %q", endResults["call_pwd"])
	}

	// Both should have "completed" status
	for _, evt := range toolEndEvents {
		data := evt.Data.(map[string]interface{})
		if data["status"] != "completed" {
			h.fail("expected status 'completed', got %v", data["status"])
		}
	}
}

func TestE2E_ToolLifecycleEvents_WithStreaming(t *testing.T) {
	h := NewHarnessWithT(t)

	// First: streamed tool call response
	h.Provider().
		WithStreaming().
		AddStreamChunks("Checking ", "file...").
		AddToolCallResponse(
			"Checking file...",
			core.ToolCall{
				ID: "call_stream",
				Function: core.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"stream.txt"}`,
				},
			},
		)
	// Second: streamed final answer
	h.Provider().
		AddStreamChunks("Result: ", "ok").
		AddTextResponse("Result: ok")

	h.Executor().AddToolResult("call_stream", "stream content")

	agent := h.NewAgent()
	_, err := agent.RunStream(context.Background(), "Read stream.txt")
	h.AssertNoError(err)

	// tool_start should be published for streamed tool calls too
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 1 {
		h.fail("expected 1 tool_start event in streaming path, got %d", len(toolStartEvents))
	}

	startData := toolStartEvents[0].Data.(map[string]interface{})
	if startData["tool_call_id"] != "call_stream" {
		h.fail("expected tool_call_id 'call_stream', got %v", startData["tool_call_id"])
	}

	// tool_end should also be published
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 1 {
		h.fail("expected 1 tool_end event in streaming path, got %d", len(toolEndEvents))
	}

	endData := toolEndEvents[0].Data.(map[string]interface{})
	if endData["tool_call_id"] != "call_stream" {
		h.fail("expected tool_call_id 'call_stream', got %v", endData["tool_call_id"])
	}
	if endData["status"] != "completed" {
		h.fail("expected status 'completed', got %v", endData["status"])
	}
}

func TestE2E_ToolLifecycleEvents_DurationPresent(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Running...",
		core.ToolCall{
			ID: "call_timer",
			Function: core.ToolCallFunction{
				Name:      "slow_tool",
				Arguments: `{}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")

	h.Executor().AddToolResult("call_timer", "result")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 1 {
		h.fail("expected 1 tool_end event, got %d", len(toolEndEvents))
	}

	endData := toolEndEvents[0].Data.(map[string]interface{})
	durationMs, ok := endData["duration_ms"]
	if !ok {
		h.fail("expected duration_ms in tool_end event")
	}

	// Duration should be a non-negative number (int64 from time.Duration.Milliseconds())
	durInt, ok := durationMs.(int64)
	if !ok {
		h.fail("expected duration_ms to be int64, got %T", durationMs)
	}
	if durInt < 0 {
		h.fail("expected non-negative duration, got %v", durInt)
	}
}

func TestE2E_ToolLifecycleEvents_NoEventBus(t *testing.T) {
	// Verify tool lifecycle works when EventBus is nil (no crash)
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Running...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "test_tool",
				Arguments: `{}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")

	h.Executor().AddToolResult("call_1", "ok")

	agent, err := core.NewAgent(core.Options{
		Provider:       h.Provider(),
		Executor:       h.Executor(),
		EventPublisher: nil, // No event bus
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "Done.")
}

func TestE2E_ToolLifecycleEvents_EventOrdering(t *testing.T) {
	// Verify the full event sequence: query_started -> tool_start -> tool_end -> query_completed
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Checking...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "check",
				Arguments: `{}`,
			},
		},
	)
	h.Provider().AddTextResponse("All good.")

	h.Executor().AddToolResult("call_1", "passed")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Check something")
	h.AssertNoError(err)

	// Collect events by type and verify ordering
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)

	if len(toolStartEvents) == 0 || len(toolEndEvents) == 0 {
		h.fail("missing tool lifecycle events")
	}

	startTime := toolStartEvents[0].Timestamp
	endTime := toolEndEvents[0].Timestamp

	// tool_start should be at or before tool_end
	if startTime.After(endTime) {
		h.fail("tool_start (%v) should not be after tool_end (%v)", startTime, endTime)
	}

	// Verify query_started and query_completed are also present
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)
}

func TestE2E_ToolLifecycleEvents_ResultTruncation(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Reading large file...",
		core.ToolCall{
			ID: "call_big",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"large.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("File read.")

	// Create a result larger than 2000 chars (the truncation threshold)
	largeContent := strings.Repeat("x", 3000)
	h.Executor().AddToolResult("call_big", largeContent)

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Read large file")
	h.AssertNoError(err)

	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 1 {
		h.fail("expected 1 tool_end event, got %d", len(toolEndEvents))
	}

	endData := toolEndEvents[0].Data.(map[string]interface{})
	result, ok := endData["result"].(string)
	if !ok {
		h.fail("expected result to be a string")
	}

	// Result should be truncated to ~2000 chars + truncation marker
	if len(result) > 2500 {
		h.fail("expected truncated result, got length %d", len(result))
	}
	if !strings.HasSuffix(result, "\n... (truncated)") {
		h.fail("expected result to end with truncation marker, got: %s", result[len(result)-30:])
	}

	// Verify truncation metadata
	truncated, ok := endData["result_truncated"].(bool)
	if !ok || !truncated {
		h.fail("expected result_truncated to be true")
	}
}

func TestE2E_ToolLifecycleEvents_MultiIteration(t *testing.T) {
	h := NewHarnessWithT(t)

	// Iteration 1: provider returns a tool call
	h.Provider().AddToolCallResponse(
		"First step.",
		core.ToolCall{
			ID: "call_iter1",
			Function: core.ToolCallFunction{
				Name:      "step_one",
				Arguments: `{"phase":"first"}`,
			},
		},
	)
	// Iteration 2: provider returns another tool call
	h.Provider().AddToolCallResponse(
		"Second step.",
		core.ToolCall{
			ID: "call_iter2",
			Function: core.ToolCallFunction{
				Name:      "step_two",
				Arguments: `{"phase":"second"}`,
			},
		},
	)
	// Iteration 3: final text response
	h.Provider().AddTextResponse("All steps complete.")

	h.Executor().AddToolResult("call_iter1", "step one done")
	h.Executor().AddToolResult("call_iter2", "step two done")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Run multi-step process")
	h.AssertNoError(err)
	h.AssertEquals(result, "All steps complete.")

	// Provider called 3 times (2 tool iterations + 1 final)
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(2)

	// --- Assert 2 tool_start events (one per iteration) ---
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 2 {
		h.fail("expected 2 tool_start events across iterations, got %d", len(toolStartEvents))
	}

	// First iteration tool start
	start1Data := toolStartEvents[0].Data.(map[string]interface{})
	if start1Data["tool_call_id"] != "call_iter1" {
		h.fail("expected first tool_call_id 'call_iter1', got %v", start1Data["tool_call_id"])
	}
	if start1Data["tool_name"] != "step_one" {
		h.fail("expected first tool_name 'step_one', got %v", start1Data["tool_name"])
	}

	// Second iteration tool start
	start2Data := toolStartEvents[1].Data.(map[string]interface{})
	if start2Data["tool_call_id"] != "call_iter2" {
		h.fail("expected second tool_call_id 'call_iter2', got %v", start2Data["tool_call_id"])
	}
	if start2Data["tool_name"] != "step_two" {
		h.fail("expected second tool_name 'step_two', got %v", start2Data["tool_name"])
	}

	// --- Assert 2 tool_end events (one per iteration) ---
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 2 {
		h.fail("expected 2 tool_end events across iterations, got %d", len(toolEndEvents))
	}

	// First iteration tool end
	end1Data := toolEndEvents[0].Data.(map[string]interface{})
	if end1Data["tool_call_id"] != "call_iter1" {
		h.fail("expected first end tool_call_id 'call_iter1', got %v", end1Data["tool_call_id"])
	}
	if end1Data["result"] != "step one done" {
		h.fail("expected first result 'step one done', got %v", end1Data["result"])
	}

	// Second iteration tool end
	end2Data := toolEndEvents[1].Data.(map[string]interface{})
	if end2Data["tool_call_id"] != "call_iter2" {
		h.fail("expected second end tool_call_id 'call_iter2', got %v", end2Data["tool_call_id"])
	}
	if end2Data["result"] != "step two done" {
		h.fail("expected second result 'step two done', got %v", end2Data["result"])
	}

	// Verify ordering: start1 <= end1 < start2 <= end2
	if toolStartEvents[0].Timestamp.After(toolEndEvents[0].Timestamp) {
		h.fail("first tool_start should not be after first tool_end")
	}
	if toolStartEvents[1].Timestamp.After(toolEndEvents[1].Timestamp) {
		h.fail("second tool_start should not be after second tool_end")
	}
	// First iteration should complete before second starts
	if toolEndEvents[0].Timestamp.After(toolStartEvents[1].Timestamp) {
		h.fail("first tool_end should not be after second tool_start")
	}
}
