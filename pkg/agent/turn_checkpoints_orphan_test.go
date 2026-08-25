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

// SP-128 §tests: Compaction must preserve the original user task message.
// Strict-syntax chat templates (Qwen3.5, any provider with raise_exception)
// reject requests with zero role:user entries. These tests verify the fix.

// TestBuildCheckpointCompactedMessages_PreservesUserTaskWhenCheckpointCoversIt
// reproduces the exact SP-128 bug: a checkpoint covering [1..2] (asst+tool)
// should preserve messages[0] (the user task) in the compacted output.
func TestBuildCheckpointCompactedMessages_PreservesUserTaskWhenCheckpointCoversIt(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user, asst, tool, user_next]
	// Checkpoint covers [1..2] — the asst and tool messages only.
	messages := []api.Message{
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", Content: "I'll fix it"},
		{Role: "tool", ToolCallID: "c1", Content: "done"},
		{Role: "user", Content: "what about tests?"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 1,
			EndIndex:   2,
			Summary:    "[first turn summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Verify the original user task is preserved at index 0.
	if len(compacted) == 0 {
		t.Fatal("compacted is empty")
	}
	if compacted[0].Role != "user" {
		t.Errorf("compacted[0].Role = %q, want %q", compacted[0].Role, "user")
	}
	if compacted[0].Content != "fix the bug" {
		t.Errorf("compacted[0].Content = %q, want %q", compacted[0].Content, "fix the bug")
	}

	// Verify a summary assistant message exists after the preserved user.
	foundSummary := false
	for _, m := range compacted {
		if m.Role == "assistant" && m.Content == "[first turn summary]" {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Error("expected summary assistant message not found")
	}

	// Verify the final user message is still present.
	hasUser := false
	for _, m := range compacted {
		if m.Role == "user" && m.Content == "what about tests?" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("expected final user message not found")
	}
}

// TestBuildCheckpointCompactedMessages_PreservesUserTaskWhenCheckpointStartsAtZero
// verifies the critical case: a checkpoint covering [0..1] must preserve the
// original user task at messages[0] before inserting the summary.
func TestBuildCheckpointCompactedMessages_PreservesUserTaskWhenCheckpointStartsAtZero(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user, asst]
	// Checkpoint covers [0..1] — the entire conversation.
	messages := []api.Message{
		{Role: "user", Content: "write a test"},
		{Role: "assistant", Content: "here's the test"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   1,
			Summary:    "[turn summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// The compacted output should be: [user(original), summary_assistant]
	if len(compacted) < 2 {
		t.Fatalf("compacted has %d messages, expected at least 2", len(compacted))
	}
	if compacted[0].Role != "user" {
		t.Errorf("compacted[0].Role = %q, want %q", compacted[0].Role, "user")
	}
	if compacted[0].Content != "write a test" {
		t.Errorf("compacted[0].Content = %q, want %q", compacted[0].Content, "write a test")
	}
	if compacted[1].Role != "assistant" {
		t.Errorf("compacted[1].Role = %q, want %q", compacted[1].Role, "assistant")
	}
	if compacted[1].Content != "[turn summary]" {
		t.Errorf("compacted[1].Content = %q, want %q", compacted[1].Content, "[turn summary]")
	}
}

// TestBuildCheckpointCompactedMessages_NonZeroCheckpointPreservesFirstUser
// verifies that when a checkpoint does NOT cover index 0, the first user
// message is preserved naturally (no special handling needed).
func TestBuildCheckpointCompactedMessages_NonZeroCheckpointPreservesFirstUser(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user, asst, user2, asst2]
	// Checkpoint covers [2..3] only.
	messages := []api.Message{
		{Role: "user", Content: "first task"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "second task"},
		{Role: "assistant", Content: "done too"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 2,
			EndIndex:   3,
			Summary:    "[second turn summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Expected: [user1, asst1, summary]
	if len(compacted) != 3 {
		t.Fatalf("compacted has %d messages, expected 3", len(compacted))
	}
	if compacted[0].Role != "user" || compacted[0].Content != "first task" {
		t.Errorf("compacted[0] = %+v, want user/first task", compacted[0])
	}
	if compacted[1].Role != "assistant" || compacted[1].Content != "done" {
		t.Errorf("compacted[1] = %+v, want assistant/done", compacted[1])
	}
	if compacted[2].Role != "assistant" || compacted[2].Content != "[second turn summary]" {
		t.Errorf("compacted[2] = %+v, want assistant/[second turn summary]", compacted[2])
	}
}

// TestBuildCheckpointCompactedMessages_RollupPreservesUserTask
// verifies that rollup-style checkpoints (covering a large range from 0)
// preserve the original user task.
func TestBuildCheckpointCompactedMessages_RollupPreservesUserTask(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Simulate a rollup: checkpoint covers [0..10] in a 12-message conversation.
	messages := make([]api.Message, 0, 12)
	messages = append(messages, api.Message{Role: "user", Content: "original task"})
	for i := 1; i < 11; i++ {
		messages = append(messages, api.Message{Role: "assistant", Content: "response"})
		messages = append(messages, api.Message{Role: "tool", ToolCallID: "c1", Content: "ok"})
	}
	messages = append(messages, api.Message{Role: "user", Content: "new task"})

	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   10,
			Level:      1,
			Summary:    "[rollup summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Expected: [user(original), summary, user(new)]
	if len(compacted) < 2 {
		t.Fatalf("compacted has %d messages, expected at least 2", len(compacted))
	}
	if compacted[0].Role != "user" {
		t.Errorf("compacted[0].Role = %q, want %q", compacted[0].Role, "user")
	}
	if compacted[0].Content != "original task" {
		t.Errorf("compacted[0].Content = %q, want %q", compacted[0].Content, "original task")
	}
}

// TestBuildCheckpointCompactedMessages_MultipleCheckpointsFirstCoversUser
// verifies that when multiple checkpoints exist and the first one covers
// index 0, all user messages are preserved (both from the first checkpoint's
// preservation and from subsequent unconsumed messages).
func TestBuildCheckpointCompactedMessages_MultipleCheckpointsFirstCoversUser(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user, asst, user2, asst2, user3]
	// Checkpoints: [0..1] and [2..3]
	messages := []api.Message{
		{Role: "user", Content: "task 1"},
		{Role: "assistant", Content: "done 1"},
		{Role: "user", Content: "task 2"},
		{Role: "assistant", Content: "done 2"},
		{Role: "user", Content: "task 3"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   1,
			Summary:    "[turn 1 summary]",
		},
		{
			StartIndex: 2,
			EndIndex:   3,
			Summary:    "[turn 2 summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Expected: [user(task 1), summary1, user(task 2), summary2, user(task 3)]
	// The first user is preserved by the fix, task 2 is naturally preserved
	// (checkpoint [2..3] doesn't consume it), task 3 is naturally preserved.
	if len(compacted) != 5 {
		t.Fatalf("compacted has %d messages, expected 5", len(compacted))
	}

	userCount := 0
	for _, m := range compacted {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 3 {
		t.Errorf("found %d user messages, want 3", userCount)
	}

	// Verify the structure: user, summary, user, summary, user
	if compacted[0].Role != "user" || compacted[0].Content != "task 1" {
		t.Errorf("compacted[0] = %+v, want user/task 1", compacted[0])
	}
	if compacted[1].Role != "assistant" || compacted[1].Content != "[turn 1 summary]" {
		t.Errorf("compacted[1] = %+v, want assistant/[turn 1 summary]", compacted[1])
	}
	if compacted[2].Role != "user" || compacted[2].Content != "task 2" {
		t.Errorf("compacted[2] = %+v, want user/task 2", compacted[2])
	}
	if compacted[3].Role != "assistant" || compacted[3].Content != "[turn 2 summary]" {
		t.Errorf("compacted[3] = %+v, want assistant/[turn 2 summary]", compacted[3])
	}
	if compacted[4].Role != "user" || compacted[4].Content != "task 3" {
		t.Errorf("compacted[4] = %+v, want user/task 3", compacted[4])
	}
}

// TestBuildCheckpointCompactedMessages_AlwaysHasAtLeastOneUserMessage
// is the belt-and-suspenders test: even if all checkpoints somehow consume
// every user message, the function should inject a fallback user message.
func TestBuildCheckpointCompactedMessages_AlwaysHasAtLeastOneUserMessage(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user, asst, asst, asst] — single user at index 0.
	// Checkpoint covers [0..3] — everything.
	messages := []api.Message{
		{Role: "user", Content: "original task"},
		{Role: "assistant", Content: "response 1"},
		{Role: "assistant", Content: "response 2"},
		{Role: "assistant", Content: "response 3"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   3,
			Summary:    "[full conversation summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Verify at least one user message exists (belt-and-suspenders).
	hasUser := false
	for _, m := range compacted {
		if m.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("belt-and-suspenders: compacted has no user message — fallback injection failed")
	}

	// If the fix works, the original user message should be preserved.
	if compacted[0].Role != "user" {
		t.Errorf("compacted[0].Role = %q, want %q (original user should be preserved)", compacted[0].Role, "user")
	}
}

// TestBuildCheckpointCompactedMessages_EmptyContentUserPreserved verifies
// that an empty-content user message at the boundary is still preserved.
// Strict chat templates care about role presence, not content, so preserving
// a role="user" with empty content is still correct — but it ensures the
// inline fix doesn't drop boundary user messages based on content alone.
func TestBuildCheckpointCompactedMessages_EmptyContentUserPreserved(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [user(empty), asst] — user at index 0 has empty content.
	messages := []api.Message{
		{Role: "user", Content: ""},
		{Role: "assistant", Content: "ok"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   1,
			Summary:    "[empty-task summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	if len(compacted) < 2 {
		t.Fatalf("compacted has %d messages, expected at least 2", len(compacted))
	}
	if compacted[0].Role != "user" {
		t.Errorf("compacted[0].Role = %q, want %q (empty-content user should still be preserved)", compacted[0].Role, "user")
	}
}

// TestBuildCheckpointCompactedMessages_FirstMessageNotUserTriggersFallback
// covers the case where messages[0] is NOT a user message (e.g., a system
// prompt or restored session starting with assistant). The inline fix skips
// preservation (Role != "user"), and the belt-and-suspenders fallback should
// kick in to inject a synthetic user message so the conversation has at
// least one role:user entry.
func TestBuildCheckpointCompactedMessages_FirstMessageNotUserTriggersFallback(t *testing.T) {
	a := &Agent{debug: false}
	a.initSubManagers()

	// Conversation: [system, asst] — first message is system, not user.
	// Checkpoint covers [0..1] — everything.
	messages := []api.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "assistant", Content: "How can I help?"},
	}
	checkpoints := []TurnCheckpoint{
		{
			StartIndex: 0,
			EndIndex:   1,
			Summary:    "[summary]",
		},
	}
	a.state.SetTurnCheckpoints(checkpoints)

	compacted, _ := a.BuildCheckpointCompactedMessages(messages)

	// Belt-and-suspenders must inject a fallback user message.
	hasUser := false
	for _, m := range compacted {
		if m.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("belt-and-suspenders: expected fallback user message injection when no role:user exists post-compaction")
	}

	// The fallback should be at index 0 (prepended) — since messages[0] was
	// a system message (not user), the fallback content should be the
	// generic placeholder "Continue the task."
	if len(compacted) > 0 && compacted[0].Role == "user" && compacted[0].Content != "Continue the task." {
		t.Errorf("fallback message content = %q, want %q (messages[0] was system, so generic placeholder expected)",
			compacted[0].Content, "Continue the task.")
	}
}
