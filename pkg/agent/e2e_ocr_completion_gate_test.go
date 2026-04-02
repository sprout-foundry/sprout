package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_OCRCompletionGateFlow verifies the full OCR completion gate flow
// through ProcessQuery. The scenario is:
//
//  1. Conversation history is pre-populated with OCR policy active:
//     - User message mentioning "OCR Trigger Policy"
//     - Assistant message with fetch_url tool call
//     - Tool result containing image hint ("image 1") and context hint ("menu")
//
//  2. Model tries to stop (finish_reason="stop") → OCR gate intercepts,
//     enqueues transient reminder, returns stop=false → conversation continues
//
//  3. Model calls analyze_image_content tool → tool executes → result appended
//     → processResponse continues (tool call handled)
//
//  4. Model returns stop again → OCR gate sees analyze_image_content in history,
//     shouldRequireOCRBeforeCompletion returns false → gate doesn't fire → normal stop
func TestE2E_OCRCompletionGateFlow(t *testing.T) {
	t.Parallel()

	// -- Build scripted responses --

	// Response 1: Model tries to finish without OCR → gate intercepts
	stopResp1 := NewScriptedResponseBuilder().
		Content("I've completed the analysis of the restaurant information.").
		FinishReason("stop").
		Build()

	// Response 2: Model calls analyze_image_content (after gate reminded it)
	// Use a fake URL that won't actually be fetched — the tool may succeed
	// or fail but either way the tool call appears in history.
	ocrToolCall := api.ToolCall{
		ID:   "call_ocr_001",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "analyze_image_content",
			Arguments: `{"image_path":"https://cdn.example.com/menu.jpg"}`,
		},
	}
	ocrToolResp := NewScriptedResponseBuilder().
		Content("Let me analyze the menu image using OCR.").
		ToolCall(ocrToolCall).
		FinishReason("tool_calls").
		Build()

	// Response 3: Model stops for real after OCR — gate is satisfied
	stopResp2 := NewScriptedResponseBuilder().
		Content("Based on the menu analysis, the restaurant offers several items.").
		FinishReason("stop").
		Build()

	// -- Build agent with scripted client (no vision support) --
	agent, _, client := buildE2EAgentWithClient(t, 10, stopResp1, ocrToolResp, stopResp2)

	// -- Pre-populate conversation history to activate OCR policy --
	agent.messages = []api.Message{
		{
			Role:    "user",
			Content: "OCR Trigger Policy (MANDATORY): use analyze_image_content for menu images/PDFs.",
		},
		{
			Role: "assistant",
			ToolCalls: []api.ToolCall{{
				ID:   "fetch_menu_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "fetch_url",
					Arguments: `{"url":"https://example.com/menu"}`,
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallId: "fetch_menu_1",
			Content:    "Menu page includes image 1: https://cdn.example.com/menu.jpg",
		},
	}

	// -- Execute ProcessQuery --
	result, err := agent.ProcessQuery("Summarize the menu")
	require.NoError(t, err, "ProcessQuery should succeed")

	// -- Assertions --

	// Should complete normally (not blocked by OCR gate)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected RunTerminationCompleted")

	// Should have taken exactly 3 iterations:
	// 1. stop → gate intercepts → transient reminder enqueued
	// 2. tool call (analyze_image_content) → tool executes → continue
	// 3. stop → gate satisfied → normal completion
	assert.Equal(t, 3, agent.GetCurrentIteration()+1,
		"expected 3 iterations (stop+gate → tool_call → stop)")

	// ProcessQuery returns the final assistant content (streaming disabled in scripted)
	assert.Contains(t, result, "Based on the menu analysis",
		"expected result to contain the final response content")

	// Verify analyze_image_content tool call appears in conversation history
	var foundOCRToolCall bool
	for _, msg := range agent.messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if tc.Function.Name == "analyze_image_content" {
					foundOCRToolCall = true
					break
				}
			}
		}
	}
	assert.True(t, foundOCRToolCall,
		"expected analyze_image_content tool call in conversation history")

	// Verify tool result for analyze_image_content exists
	var foundOCRToolResult bool
	for _, msg := range agent.messages {
		if msg.Role == "tool" && msg.ToolCallId == "call_ocr_001" {
			foundOCRToolResult = true
			break
		}
	}
	assert.True(t, foundOCRToolResult,
		"expected tool result for analyze_image_content in conversation history")

	// Verify message ordering for the ProcessQuery portion (last 5 messages):
	// user(query) → assistant(stop, gate caught) → assistant(ocr_tool_call) → tool → assistant(stop)
	// Note: the "assistant(stop, gate caught)" has no tool calls — it's just text
	// that the gate intercepted. The transient OCR reminder is injected by
	// prepareMessages into the API request but NOT persisted to agent.messages.
	assertMessageOrdering(t, agent.messages,
		[]string{"user", "assistant", "assistant", "tool", "assistant"})

	// -- Verify the transient OCR reminder was sent to the model --
	// On the 2nd request (index 1), prepareMessages should have included
	// the OCR transient reminder message enqueued during iteration 0.
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3,
		"expected at least 3 sent requests (initial + after gate + after tool)")

	// The 2nd request should contain the transient OCR reminder.
	// Assert against the known reminder text rather than using negative matching.
	const ocrReminderSubstring = "OCR policy requirement: before finishing"
	secondReqMsgs := sentReqs[1] // 0-based: request after first stop was caught by gate
	var ocrReminderFound bool
	for _, msg := range secondReqMsgs {
		if msg.Role == "user" && strings.Contains(msg.Content, ocrReminderSubstring) {
			ocrReminderFound = true
			break
		}
	}
	assert.True(t, ocrReminderFound,
		"expected the 2nd request to model to contain the OCR policy transient reminder")
}
