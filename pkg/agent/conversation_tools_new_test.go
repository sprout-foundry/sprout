package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// makeTestToolCall creates a ToolCall with the given ID and function name/arguments.
func makeTestToolCall(id, name, args string) api.ToolCall {
	tc := api.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// newTestHandlerForProvider creates a minimal ConversationHandler with an agent
// that reports the given provider for sanitization testing.
func newTestHandlerForProvider(provider string) *ConversationHandler {
	state := NewAgentStateManager(false)
	state.SetSessionProvider(api.ClientType(provider))
	agent := &Agent{
		debug: true,
		state: state,
	}
	return &ConversationHandler{
		agent: agent,
	}
}

func TestSanitizeToolMessagesEmpty(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	tests := []struct {
		name     string
		input    []api.Message
		expected int // expected message count
	}{
		{"nil input", nil, 0},
		{"empty slice", []api.Message{}, 0},
		{"single user message", []api.Message{{Role: "user", Content: "hello"}}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ch.sanitizeToolMessages(tt.input)
			if len(out) != tt.expected {
				t.Errorf("got %d messages, want %d", len(out), tt.expected)
			}
		})
	}
}

func TestSanitizeToolMessagesHappyPath(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "do something"},
		{
			Role:    "assistant",
			Content: "running tool",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_1", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "file content"},
		{Role: "assistant", Content: "done"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "assistant" || out[2].Role != "tool" || out[3].Role != "assistant" {
		t.Errorf("roles not preserved: %v", func() []string {
			var r []string
			for _, m := range out {
				r = append(r, m.Role)
			}
			return r
		}())
	}
}

func TestSanitizeToolMessagesOrphanedToolResult(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", ToolCallID: "call_orphan", Content: "no matching assistant"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 1 {
		t.Fatalf("expected 1 message (orphaned tool dropped), got %d", len(out))
	}
	if out[0].Role != "user" {
		t.Errorf("expected user message, got %s", out[0].Role)
	}
}

func TestSanitizeToolMessagesMissingToolCallId(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "doing thing",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_1", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "", Content: "missing id"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (tool with empty id dropped), got %d", len(out))
	}
}

func TestSanitizeToolMessagesInterleavedUserMessages(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "first prompt"},
		{
			Role:    "assistant",
			Content: "ok",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_a", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_a", Content: "content"},
		{Role: "user", Content: "interjected user message"},
		{Role: "assistant", Content: "received"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 5 {
		t.Fatalf("expected 5 messages (user messages preserved), got %d", len(out))
	}
	if out[3].Role != "user" || out[3].Content != "interjected user message" {
		t.Error("interjected user message should be preserved in position")
	}
}

func TestSanitizeToolMessagesMultipleToolCallsOneMissingResult(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "do two things"},
		{
			Role:    "assistant",
			Content: "running tools",
			ToolCalls: []api.ToolCall{
				makeTestToolCall("call_1", "read_file", ""),
				makeTestToolCall("call_2", "write_file", ""),
			},
		},
		// Only call_1 has a result; call_2 result is missing
		{Role: "tool", ToolCallID: "call_1", Content: "file content"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	// Assistant with tool calls is preserved even though one result is missing
	if len(out[1].ToolCalls) != 2 {
		t.Errorf("assistant should still have 2 tool calls, got %d", len(out[1].ToolCalls))
	}
}

func TestSanitizeToolMessagesMinimaxSecondPass(t *testing.T) {
	t.Parallel()
	// Minimax does a second pass to catch orphaned tool results that the
	// first pass might leave behind (e.g., if a tool result appears before
	// its assistant message in a corrupted history).
	ch := newTestHandlerForProvider("minimax")

	messages := []api.Message{
		{Role: "user", Content: "start"},
		// Tool result before any assistant — orphan that first pass would also drop,
		// but second pass double-checks the final list.
		{Role: "tool", ToolCallID: "call_orphan", Content: "orphan result 1"},
		{
			Role:    "assistant",
			Content: "doing work",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_real", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_real", Content: "valid result"},
		// Another orphan after the assistant — first pass catches this since
		// it wasn't in seenToolCalls. Second pass verifies the final list is clean.
		{Role: "tool", ToolCallID: "call_orphan2", Content: "orphan result 2"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (user + assistant + valid tool), got %d", len(out))
	}
	if out[0].Role != "user" {
		t.Errorf("first message should be user, got %s", out[0].Role)
	}
	if out[1].Role != "assistant" {
		t.Errorf("second message should be assistant, got %s", out[1].Role)
	}
	if out[2].Role != "tool" || out[2].ToolCallID != "call_real" {
		t.Errorf("third message should be valid tool result for call_real, got role=%s id=%s", out[2].Role, out[2].ToolCallID)
	}
}

func TestSanitizeToolMessagesMinimaxDoubleOrphanSecondPass(t *testing.T) {
	t.Parallel()
	// Construct a pathological case where a tool result with an ID that
	// happens to match a tool call seen in a *later* assistant message
	// could pass the first pass but fail the second pass (since in the
	// second pass we only scan forward, not backward).
	ch := newTestHandlerForProvider("minimax")

	messages := []api.Message{
		{Role: "user", Content: "go"},
		// This tool result has ID "call_a" which is emitted by the assistant below.
		// First pass: call_a is NOT in seenToolCalls yet, so it's dropped.
		{Role: "tool", ToolCallID: "call_a", Content: "premature result"},
		{
			Role:    "assistant",
			Content: "running",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_a", "read_file", "")},
		},
	}

	out := ch.sanitizeToolMessages(messages)
	// First pass drops the premature tool result. Second pass should not re-introduce it.
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(out))
	}
}

func TestSanitizeToolMessagesNonMinimaxNoSecondPass(t *testing.T) {
	t.Parallel()
	// Non-minimax providers should not run the second pass.
	// The first pass behavior should be the final output.
	ch := newTestHandlerForProvider("openai")

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "running",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_1", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

func TestSanitizeToolMessagesAgentNil(t *testing.T) {
	t.Parallel()
	// When agent is nil, sanitization should still work (no debug logging).
	ch := &ConversationHandler{agent: nil}

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "doing",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_1", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "result"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

func TestSanitizeToolMessagesToolCallIdEmptyInAssistant(t *testing.T) {
	t.Parallel()
	// Tool calls with empty IDs in assistant should not be tracked.
	ch := newTestHandlerForProvider("")

	tc := makeTestToolCall("", "bad_tool", "")
	messages := []api.Message{
		{Role: "user", Content: "go"},
		{
			Role:    "assistant",
			Content: "running",
			ToolCalls: []api.ToolCall{tc},
		},
		// This tool result references an ID that was never tracked (empty).
		{Role: "tool", ToolCallID: "call_ghost", Content: "ghost result"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), orphan dropped; got %d", len(out))
	}
}

func TestSanitizeToolMessagesMultipleToolResultsPerCall(t *testing.T) {
	t.Parallel()
	// Edge case: same tool_call_id used multiple times in tool results.
	// Each result consumes one entry from seenToolCalls, so only the first
	// result should be kept (since we delete on match).
	ch := newTestHandlerForProvider("")

	messages := []api.Message{
		{Role: "user", Content: "go"},
		{
			Role:    "assistant",
			Content: "running",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_1", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "first result"},
		{Role: "tool", ToolCallID: "call_1", Content: "duplicate result"},
	}

	out := ch.sanitizeToolMessages(messages)
	// First result is kept (matches and deletes from seen), second is dropped.
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

func TestSanitizeToolMessagesComplexConversation(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("deepseek")

	messages := []api.Message{
		{Role: "user", Content: "write a file"},
		{
			Role:    "assistant",
			Content: "I'll write the file",
			ToolCalls: []api.ToolCall{makeTestToolCall("tc1", "write_file", "")},
		},
		{Role: "tool", ToolCallID: "tc1", Content: "file written"},
		{
			Role:    "assistant",
			Content: "now read it back",
			ToolCalls: []api.ToolCall{makeTestToolCall("tc2", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "tc2", Content: "file content read"},
		{Role: "assistant", Content: "all done"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(out))
	}
	roles := []string{out[0].Role, out[1].Role, out[2].Role, out[3].Role, out[4].Role, out[5].Role}
	expected := []string{"user", "assistant", "tool", "assistant", "tool", "assistant"}
	for i, r := range roles {
		if r != expected[i] {
			t.Errorf("position %d: got %s, want %s", i, r, expected[i])
		}
	}
}

func TestSanitizeToolMessagesDeepseekProvider(t *testing.T) {
	t.Parallel()
	ch := newTestHandlerForProvider("deepseek")

	// DeepSeek does not get the second pass — only first pass sanitization.
	messages := []api.Message{
		{Role: "user", Content: "go"},
		{Role: "tool", ToolCallID: "no_assistant", Content: "orphan"},
		{
			Role:    "assistant",
			Content: "ok",
			ToolCalls: []api.ToolCall{makeTestToolCall("call_ok", "read_file", "")},
		},
		{Role: "tool", ToolCallID: "call_ok", Content: "result"},
	}

	out := ch.sanitizeToolMessages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (orphan dropped), got %d", len(out))
	}
}
