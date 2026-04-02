package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_RedundantShellCommandOptimization verifies that redundant shell
// command tool results are optimised through the full prepareMessages pipeline.
//
// When the same shell command appears as a tool result at two different
// positions (an older one and a newer one), the older one is replaced with a
// [STALE] summary, while the most recent execution is preserved as-is.
// Different shell commands must not be affected.
//
// NOTE: Shell commands have no minimum-gap threshold (unlike file reads which
// require ≥15 messages between occurrences). Even back-to-back duplicates are
// marked [STALE] — only the most recent execution survives.
//
// The test wires the ConversationOptimizer into an Agent via prepareMessages,
// exercising the full OptimizeConversation → sanitize → produce pipeline.
func TestE2E_RedundantShellCommandOptimization(t *testing.T) {
	t.Parallel()

	// ---------------------------------------------------------------------------
	// 1. Agent + handler setup
	// ---------------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false) // enabled, not debug

	const cmd = "go test ./..."
	const output = "PASS\nok  \tgithub.com/example/pkg\t0.003s"

	// ---------------------------------------------------------------------------
	// 2. Build message layout
	//
	//   Index  Role       Purpose
	//   0      user       initial request
	//   1      assistant  tool_call → call_old_1 (shell_command)
	//   2      tool       OLD shell result  ← should become [STALE]
	//   3      user       gap filler
	//   4      assistant  gap filler
	//   5      user       gap filler
	//   6      assistant  gap filler
	//   7      user       re-run request
	//   8      assistant  tool_call → call_new_1 (shell_command)
	//   9      tool       NEW shell result  ← should be preserved
	//  10      user       different command request
	//  11      assistant  tool_call → call_other (shell_command, different cmd)
	//  12      tool       different command result ← must NOT be optimised
	//  13      user       follow-up
	//  14      assistant  closing ack
	// ---------------------------------------------------------------------------
	messages := make([]api.Message, 0, 15)

	messages = append(messages, api.Message{Role: "user", Content: "Run the tests"})

	// -- Older execution of "go test ./..." --
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_old_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go test ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_old_1",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\n%s", cmd, output),
	})

	// -- Gap (4 messages) --
	messages = append(messages, api.Message{Role: "user", Content: "Continue analyzing"})
	messages = append(messages, api.Message{Role: "assistant", Content: "Working on analysis step 1"})
	messages = append(messages, api.Message{Role: "user", Content: "Continue step 2"})
	messages = append(messages, api.Message{Role: "assistant", Content: "Working on analysis step 2"})

	// -- Newer execution of "go test ./..." --
	messages = append(messages, api.Message{Role: "user", Content: "Run the tests again"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_new_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go test ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_new_1",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\n%s", cmd, output),
	})

	// -- Different command (must NOT be optimised) --
	messages = append(messages, api.Message{Role: "user", Content: "Check git status"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_other",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"git status"}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_other",
		Content:    "Tool call result for shell_command: git status\nOn branch main\nnothing to commit",
	})

	// -- Final messages --
	messages = append(messages, api.Message{Role: "user", Content: "What about error handling?"})
	messages = append(messages, api.Message{Role: "assistant", Content: "Fixed it."})

	agent.messages = messages

	// ---------------------------------------------------------------------------
	// 3. Call prepareMessages (the full pipeline)
	// ---------------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	// ---------------------------------------------------------------------------
	// 4. Assertions
	// ---------------------------------------------------------------------------

	// (a) System prompt is prepended and all messages survive the pipeline.
	//     No compaction fires since maxContextTokens defaults to 0.
	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, "system", prepared[0].Role, "first message should be the system prompt")
	assert.Equal(t, 16, len(prepared),
		"expected system prompt + all 15 messages (no compaction, no pruning)")

	// (b) The older shell command tool result was replaced with a [STALE] summary.
	var staleCount int
	for _, msg := range prepared {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[STALE]") &&
			strings.Contains(msg.Content, cmd) {
			staleCount++
		}
	}
	assert.Equal(t, 1, staleCount,
		"expected exactly 1 [STALE] summary for the older redundant shell command, got %d", staleCount)

	// (c) The most recent execution of "go test ./..." is preserved with
	//     original content (no [STALE] marker).
	var preservedCount int
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, fmt.Sprintf("Tool call result for shell_command: %s", cmd)) &&
			!strings.Contains(msg.Content, "[STALE]") {
			preservedCount++
		}
	}
	assert.Equal(t, 1, preservedCount,
		"expected exactly 1 preserved (non-stale) shell command result, got %d", preservedCount)

	// (d) The different command ("git status") is untouched — no [STALE] marker.
	var foundGitStatus bool
	for _, msg := range prepared {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Tool call result for shell_command: git status") {
			assert.NotContains(t, msg.Content, "[STALE]",
				"different shell command should never be marked [STALE]")
			foundGitStatus = true
		}
	}
	assert.True(t, foundGitStatus,
		"expected the different (git status) shell command to be present in prepared messages")

	// (e) Recent conversation messages (after the newer shell command) are preserved.
	var foundErrorHandling bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "What about error handling?") {
			foundErrorHandling = true
		}
	}
	assert.True(t, foundErrorHandling,
		"expected recent conversation messages to survive the pipeline")
}

// TestE2E_RedundantShellCommandWithDifferentOutputs verifies that the shell
// command redundancy detector operates on the command string and position
// alone — it does NOT compare output content. Even when the same command
// produces different output at two different times, the older execution is
// still marked [STALE].
func TestE2E_RedundantShellCommandWithDifferentOutputs(t *testing.T) {
	t.Parallel()

	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	const cmd = "go build ./..."

	messages := make([]api.Message, 0, 10)

	messages = append(messages, api.Message{Role: "user", Content: "Build the project"})

	// -- Older execution (different output) --
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_build_old",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go build ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_build_old",
		Content: fmt.Sprintf("Tool call result for shell_command: %s\n%s",
			cmd, "build error: undefined: MyFunc"),
	})

	// -- Gap --
	messages = append(messages, api.Message{Role: "user", Content: "Fix the error"})
	messages = append(messages, api.Message{Role: "assistant", Content: "Added the missing function"})

	// -- Newer execution (different output — now succeeds) --
	messages = append(messages, api.Message{Role: "user", Content: "Build again"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_build_new",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go build ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_build_new",
		Content: fmt.Sprintf("Tool call result for shell_command: %s\n%s",
			cmd, "Build succeeded."),
	})

	// -- Follow-up --
	messages = append(messages, api.Message{Role: "assistant", Content: "Build succeeded, ready to test."})

	agent.messages = messages

	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, 10, len(prepared),
		"expected system prompt + all 9 messages (no compaction, no pruning)")

	// (a) The older (failing) build result is marked [STALE].
	var staleCount int
	for _, msg := range prepared {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[STALE]") &&
			strings.Contains(msg.Content, cmd) {
			staleCount++
		}
	}
	assert.Equal(t, 1, staleCount,
		"expected exactly 1 [STALE] marker even though outputs differ, got %d", staleCount)

	// (b) The newer (successful) build result is preserved.
	var preservedCount int
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, fmt.Sprintf("Tool call result for shell_command: %s", cmd)) &&
			!strings.Contains(msg.Content, "[STALE]") {
			preservedCount++
		}
	}
	assert.Equal(t, 1, preservedCount,
		"expected exactly 1 preserved shell command result, got %d", preservedCount)
}

// TestE2E_RedundantShellCommandBackToBack verifies the doc-comment claim that
// "Even back-to-back duplicates are marked [STALE]". Two identical shell
// commands with no gap (adjacent tool results) should still result in the
// older being marked [STALE] and the newer being preserved.
func TestE2E_RedundantShellCommandBackToBack(t *testing.T) {
	t.Parallel()

	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	const cmd = "go vet ./..."

	messages := make([]api.Message, 0, 8)

	messages = append(messages, api.Message{Role: "user", Content: "Run vet"})

	// -- First execution (adjacent, no gap) --
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_vet_1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go vet ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_vet_1",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\nno issues found", cmd),
	})

	// -- User asks to run again immediately (back-to-back) --
	messages = append(messages, api.Message{Role: "user", Content: "Run it one more time"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_vet_2",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go vet ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_vet_2",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\nno issues found", cmd),
	})

	// -- Follow-up --
	messages = append(messages, api.Message{Role: "assistant", Content: "All clean."})

	agent.messages = messages

	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, 8, len(prepared),
		"expected system prompt + all 7 messages (no compaction)")

	// The first execution is marked STALE.
	var staleCount int
	for _, msg := range prepared {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[STALE]") &&
			strings.Contains(msg.Content, cmd) {
			staleCount++
		}
	}
	assert.Equal(t, 1, staleCount,
		"expected exactly 1 [STALE] for older back-to-back duplicate, got %d", staleCount)

	// The second execution is preserved.
	var preservedCount int
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, fmt.Sprintf("Tool call result for shell_command: %s", cmd)) &&
			!strings.Contains(msg.Content, "[STALE]") {
			preservedCount++
		}
	}
	assert.Equal(t, 1, preservedCount,
		"expected exactly 1 preserved result for newer back-to-back duplicate, got %d", preservedCount)
}

// TestE2E_RedundantShellCommandThreeOccurrences verifies that when the same
// shell command appears 3+ times, the map-overwrite semantics of trackShellCommand
// ensure that only the most recent execution survives and all older ones become
// [STALE]. With 3 occurrences, exactly 2 should be stale and 1 preserved.
func TestE2E_RedundantShellCommandThreeOccurrences(t *testing.T) {
	t.Parallel()

	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	const cmd = "go build ./..."

	messages := make([]api.Message, 0, 18)

	// Turn 1: build fails
	messages = append(messages, api.Message{Role: "user", Content: "Build it"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_b1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go build ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_b1",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\nundefined: Foo", cmd),
	})

	messages = append(messages, api.Message{Role: "assistant", Content: "Let me fix that."})

	// Turn 2: build fails again (different error)
	messages = append(messages, api.Message{Role: "user", Content: "Try again"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_b2",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go build ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_b2",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\ncannot find package", cmd),
	})

	messages = append(messages, api.Message{Role: "assistant", Content: "Fixed the import."})

	// Turn 3: build succeeds
	messages = append(messages, api.Message{Role: "user", Content: "Try once more"})
	messages = append(messages, api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "call_b3",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"go build ./..."}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		ToolCallId: "call_b3",
		Content:    fmt.Sprintf("Tool call result for shell_command: %s\nBuild succeeded.", cmd),
	})

	messages = append(messages, api.Message{Role: "assistant", Content: "Build is clean."})

	agent.messages = messages

	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, 13, len(prepared),
		"expected system prompt + all 12 messages (no compaction)")

	// All older executions become STALE.
	var staleCount int
	for _, msg := range prepared {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[STALE]") &&
			strings.Contains(msg.Content, cmd) {
			staleCount++
		}
	}
	assert.Equal(t, 2, staleCount,
		"expected exactly 2 [STALE] markers for 3 occurrences of the same command, got %d", staleCount)

	// Only the newest execution is preserved.
	var preservedCount int
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, fmt.Sprintf("Tool call result for shell_command: %s", cmd)) &&
			!strings.Contains(msg.Content, "[STALE]") {
			preservedCount++
		}
	}
	assert.Equal(t, 1, preservedCount,
		"expected exactly 1 preserved result (the most recent), got %d", preservedCount)
}
