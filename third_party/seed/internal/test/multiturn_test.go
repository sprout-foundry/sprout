package test

import (
	"context"
	"testing"

	"github.com/sprout-foundry/seed/core"
)

func TestE2E_MultiTurnWithToolCalls(t *testing.T) {
	h := NewHarnessWithT(t)

	// First turn: model calls a tool, then responds
	h.Provider().AddToolCallResponse("Let me check", core.ToolCall{
		ID:   "tc1",
		Type: "function",
		Function: core.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"path": "test.txt"}`,
		},
	})
	h.Provider().AddTextResponse("The file contains hello world")

	agent := h.NewAgent()
	h.Executor().AddToolResult("tc1", "hello world")

	// First turn
	result1, err := agent.Run(context.Background(), "Read test.txt")
	h.AssertNoError(err)
	h.AssertEquals(result1, "The file contains hello world")

	t.Logf("After turn 1: %d messages in state", agent.State().Len())

	// Second turn: simple question about the file
	h.Provider().AddTextResponse("The file says hello world")
	result2, err := agent.Run(context.Background(), "What did the file say?")
	h.AssertNoError(err)
	h.AssertEquals(result2, "The file says hello world")

	t.Logf("After turn 2: %d messages in state", agent.State().Len())
}

func TestE2E_MultiTurn_ValidatorFalsePositive(t *testing.T) {
	h := NewHarnessWithT(t)

	// First turn: simple response
	h.Provider().AddTextResponse("I found 3 issues in the code.")

	agent := h.NewAgent()

	result1, err := agent.Run(context.Background(), "Check the code")
	h.AssertNoError(err)
	h.AssertEquals(result1, "I found 3 issues in the code.")

	// Second turn: model responds with short "I'll..." prefix
	// With the old guardless tentative check, this would be rejected as tentative.
	// With the fix, it's accepted because there are no recent tool results.
	h.Provider().AddTextResponseWithFinish("I'll explain the approach: adapters that convert between types.", "stop")
	result2, err := agent.Run(context.Background(), "How should we integrate?")
	h.AssertNoError(err)
	h.AssertEquals(result2, "I'll explain the approach: adapters that convert between types.")
}

func TestE2E_MultiTurn_TwoToolCallsAcrossTurns(t *testing.T) {
	h := NewHarnessWithT(t)

	// Turn 1: tool call
	h.Provider().AddToolCallResponse("", core.ToolCall{
		ID:   "tc1",
		Type: "function",
		Function: core.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"path": "a.txt"}`,
		},
	})
	h.Provider().AddTextResponse("File A contents: hello")

	agent := h.NewAgent()
	h.Executor().AddToolResult("tc1", "hello")

	result1, err := agent.Run(context.Background(), "Read a.txt")
	h.AssertNoError(err)
	h.AssertEquals(result1, "File A contents: hello")

	// Turn 2: another tool call
	h.Provider().AddToolCallResponse("", core.ToolCall{
		ID:   "tc2",
		Type: "function",
		Function: core.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"path": "b.txt"}`,
		},
	})
	h.Provider().AddTextResponse("File B contents: world")
	h.Executor().AddToolResult("tc2", "world")

	result2, err := agent.Run(context.Background(), "Read b.txt")
	h.AssertNoError(err)
	h.AssertEquals(result2, "File B contents: world")

	// Verify state has both turns
	msgs := agent.State().Messages()
	t.Logf("Messages after 2 turns: %d", len(msgs))

	// Should have: user1, assistant(tool_call), tool(result), assistant(text), user2, assistant(tool_call), tool(result), assistant(text) = 8
	h.AssertStateHasNMessages(agent, 8)
}
