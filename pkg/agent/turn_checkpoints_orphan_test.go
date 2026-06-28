package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestExpandCheckpointRangeForToolResults covers the orphan-elimination
// logic that grows a checkpoint's EndIndex to absorb any trailing tool
// messages whose tool_call_id references an in-range assistant block.
// Without this expansion, partial coverage of an assistant+tool_calls
// block leaves orphan tool messages in the conversation — strict-syntax
// providers (MiniMax, DeepSeek) reject the whole request as 2013
// "tool call result does not follow tool call".
func TestExpandCheckpointRangeForToolResults(t *testing.T) {
	asst := func(id string) api.Message {
		return api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{{
				ID:   id,
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      "echo",
					Arguments: "{}",
				},
			}},
		}
	}
	tool := func(id string) api.Message {
		return api.Message{Role: "tool", ToolCallID: id, Content: "ok"}
	}

	cases := []struct {
		name             string
		messages         []api.Message
		startIndex       int
		endIndex         int
		expectedEndIndex int
	}{
		{
			name: "no tool calls in range → no expansion",
			messages: []api.Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			},
			startIndex:       0,
			endIndex:         1,
			expectedEndIndex: 1,
		},
		{
			name: "assistant + matching tool all in range → no expansion needed",
			messages: []api.Message{
				{Role: "user", Content: "hi"},
				asst("c1"), tool("c1"),
			},
			startIndex:       1,
			endIndex:         2,
			expectedEndIndex: 2,
		},
		{
			name: "range ends at assistant, trailing tool orphaned → expand",
			messages: []api.Message{
				{Role: "user", Content: "hi"},
				asst("c1"), tool("c1"),
				{Role: "user", Content: "next"},
			},
			startIndex:       1,
			endIndex:         1,
			expectedEndIndex: 2,
		},
		{
			name: "multi-tool batch: expand absorbs all matching tool messages",
			messages: []api.Message{
				asst("c1"), tool("c1"), tool("c1"), tool("c1"),
			},
			startIndex:       0,
			endIndex:         0,
			expectedEndIndex: 3,
		},
		{
			name: "stops at non-tool boundary (does not eat the next user msg)",
			messages: []api.Message{
				asst("c1"), tool("c1"),
				{Role: "user", Content: "next"},
			},
			startIndex:       0,
			endIndex:         0,
			expectedEndIndex: 1,
		},
		{
			name: "stops at tool message with mismatched id",
			messages: []api.Message{
				asst("c1"), tool("c1"), tool("c2"),
			},
			startIndex:       0,
			endIndex:         0,
			expectedEndIndex: 1,
		},
		{
			name: "endIndex at slice end → no expansion (already maximal)",
			messages: []api.Message{
				asst("c1"), tool("c1"),
			},
			startIndex:       0,
			endIndex:         1,
			expectedEndIndex: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandCheckpointRangeForToolResults(tc.messages, tc.startIndex, tc.endIndex)
			if got != tc.expectedEndIndex {
				t.Errorf("got endIndex %d, want %d", got, tc.expectedEndIndex)
			}
		})
	}
}

// TestDropOrphanToolMessages exercises the final-pass orphan scrubber.
// This is the safety net that catches tool messages surviving any other
// path (manual edits, restored sessions, rollup edge cases) before the
// conversation reaches a provider.
func TestDropOrphanToolMessages(t *testing.T) {
	asst := func(id string) api.Message {
		return api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{{
				ID:   id,
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      "echo",
					Arguments: "{}",
				},
			}},
		}
	}
	tool := func(id string) api.Message {
		return api.Message{Role: "tool", ToolCallID: id, Content: "ok"}
	}

	cases := []struct {
		name     string
		in       []api.Message
		expected []api.Message
	}{
		{
			name:     "no orphans, nothing dropped",
			in:       []api.Message{asst("c1"), tool("c1")},
			expected: []api.Message{asst("c1"), tool("c1")},
		},
		{
			name:     "orphan tool with no parent assistant dropped",
			in:       []api.Message{tool("c1")},
			expected: []api.Message{},
		},
		{
			name:     "orphan tool with mismatched id dropped, parent kept",
			in:       []api.Message{asst("c1"), tool("c2")},
			expected: []api.Message{asst("c1")},
		},
		{
			name:     "empty slice unchanged",
			in:       nil,
			expected: nil,
		},
		{
			name:     "user messages unaffected by orphan scrub",
			in:       []api.Message{{Role: "user", Content: "hi"}},
			expected: []api.Message{{Role: "user", Content: "hi"}},
		},
		{
			name:     "orphan scrub respects parent anywhere upstream",
			in:       []api.Message{asst("c1"), tool("c1"), tool("c1"), tool("c1")},
			expected: []api.Message{asst("c1"), tool("c1"), tool("c1"), tool("c1")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dropOrphanToolMessages(tc.in, false)
			if len(got) != len(tc.expected) {
				t.Fatalf("len mismatch: got %d, want %d (got=%v)", len(got), len(tc.expected), got)
			}
			for i := range got {
				if got[i].Role != tc.expected[i].Role {
					t.Errorf("[%d] role = %q, want %q", i, got[i].Role, tc.expected[i].Role)
				}
				if got[i].ToolCallID != tc.expected[i].ToolCallID {
					t.Errorf("[%d] tool_call_id = %q, want %q", i, got[i].ToolCallID, tc.expected[i].ToolCallID)
				}
			}
		})
	}
}

// TestBuildCheckpointCompactedMessages_ExpandsForOrphanToolResults is
// the integration test for the fix: a checkpoint that ends at an
// assistant message with tool_calls will be expanded to cover the
// matching tool messages, so the resulting compacted conversation has
// no orphan tool results.
func TestBuildCheckpointCompactedMessages_ExpandsForOrphanToolResults(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()
	asst := api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{{
			ID:   "c1",
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "echo",
				Arguments: "{}",
			},
		}},
	}
	toolResult := api.Message{Role: "tool", ToolCallID: "c1", Content: "ok"}

	// Conversation: [user, assistant+tool_calls, tool_result, user_next]
	// Checkpoint covers [1..1] — the assistant only. Without the fix,
	// the tool_result becomes an orphan after compaction because the
	// assistant message has been replaced by a summary.
	messages := []api.Message{
		{Role: "user", Content: "first"},
		asst, toolResult,
		{Role: "user", Content: "next"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 1,
			EndIndex:   1,
			Summary:    "[first turn summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Verify no orphan tool message remains. The expected post-compaction
	// shape is: [user, summary_assistant, user_next]. The tool_result
	// should have been absorbed into the checkpoint range (not present
	// in the compacted slice because it was replaced by the summary).
	for i, m := range compacted {
		if m.Role == "tool" {
			t.Errorf("compacted conversation has a tool message at index %d: %+v (expected all tool results to be absorbed by the expanded checkpoint range)", i, m)
		}
	}

	// Verify the summary was inserted at index 1.
	if len(compacted) < 2 {
		t.Fatalf("compacted has %d messages, expected at least 2", len(compacted))
	}
	if compacted[1].Role != "assistant" {
		t.Errorf("expected summary at index 1, got role %q", compacted[1].Role)
	}
}
