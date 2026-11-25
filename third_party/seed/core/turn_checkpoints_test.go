package core

import (
	"strings"
	"testing"
	"time"
)

// --- BuildCheckpointCompactedMessages tests ---

func TestBuildCheckpointCompactedMessages_EmptyMessages(t *testing.T) {
	msgs, cps := BuildCheckpointCompactedMessages(nil, []TurnCheckpoint{})
	if len(msgs) != 0 {
		t.Errorf("expected empty messages, got %d", len(msgs))
	}
	if len(cps) != 0 {
		t.Errorf("expected empty checkpoints, got %d", len(cps))
	}
}

func TestBuildCheckpointCompactedMessages_EmptyCheckpoints(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	cp := []TurnCheckpoint{}
	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, cp)

	// Should return copies unchanged.
	if len(outMsgs) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(outMsgs))
	}
	if len(outCps) != 0 {
		t.Errorf("expected 0 checkpoints, got %d", len(outCps))
	}

	// Verify originals were not modified.
	if msgs[0].Content != "hi" {
		t.Error("original messages were mutated")
	}
}

func TestBuildCheckpointCompactedMessages_NoConsumable(t *testing.T) {
	// Checkpoint with empty summary is not consumable.
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "", ActionableSummary: ""},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Messages should be unchanged.
	if len(outMsgs) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(outMsgs))
	}
	// Checkpoint should be kept as-is (not consumed).
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(outCps))
	}
	if outCps[0].Summary != "" {
		t.Error("checkpoint should be unchanged")
	}
}

func TestBuildCheckpointCompactedMessages_SingleCheckpoint(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "The answer is 4."},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 2, Summary: "User asked about 2+2, assistant answered 4.", ActionableSummary: "- Question: What is 2+2?\n- Result: 4"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Should have 3 messages: system + original user query + summary.
	if len(outMsgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(outMsgs))
	}

	if outMsgs[0].Role != "system" || outMsgs[0].Content != "You are helpful." {
		t.Errorf("system message mismatch: %+v", outMsgs[0])
	}
	// Original user query is preserved before the summary (SP-128 fix).
	if outMsgs[1].Role != "user" || outMsgs[1].Content != "What is 2+2?" {
		t.Errorf("expected original user query at [1], got: %+v", outMsgs[1])
	}
	if outMsgs[2].Role != "user" {
		t.Errorf("expected summary to be user role, got %s", outMsgs[2].Role)
	}
	if outMsgs[2].Content != "- Question: What is 2+2?\n- Result: 4" {
		t.Errorf("expected actionable summary content, got: %s", outMsgs[2].Content)
	}

	// Consumed checkpoint is kept in the returned list so future calls
	// can re-apply its summary against the growing message history.
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (kept for future re-application), got %d", len(outCps))
	}
	// Checkpoint indices are unchanged (reference raw state.Messages()).
	if outCps[0].StartIndex != 1 || outCps[0].EndIndex != 2 {
		t.Errorf("expected checkpoint [1,2], got [%d,%d]", outCps[0].StartIndex, outCps[0].EndIndex)
	}
}

func TestBuildCheckpointCompactedMessages_MultipleCheckpoints(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Question 1"},
		{Role: "assistant", Content: "Answer 1"},
		{Role: "user", Content: "Question 2"},
		{Role: "assistant", Content: "Answer 2"},
		{Role: "user", Content: "Question 3"},
		{Role: "assistant", Content: "Answer 3"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 2, Summary: "Summary 1"},
		{StartIndex: 3, EndIndex: 4, Summary: "Summary 2"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 system + 2 preserved users + 2 summaries + 1 last turn + 1 assistant = 7 messages.
	if len(outMsgs) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(outMsgs))
	}

	// Both consumed checkpoints are kept for future re-application.
	if len(outCps) != 2 {
		t.Errorf("expected 2 checkpoints (kept for future re-application), got %d", len(outCps))
	}
	// Indices unchanged (reference raw state.Messages()).
	if outCps[0].StartIndex != 1 || outCps[0].EndIndex != 2 {
		t.Errorf("expected checkpoint [1,2], got [%d,%d]", outCps[0].StartIndex, outCps[0].EndIndex)
	}
	if outCps[1].StartIndex != 3 || outCps[1].EndIndex != 4 {
		t.Errorf("expected checkpoint [3,4], got [%d,%d]", outCps[1].StartIndex, outCps[1].EndIndex)
	}

	// Verify structure: system, user1, summary1, user2, summary2, user3, assistant3
	expectedRoles := []string{"system", "user", "user", "user", "user", "user", "assistant"}
	for i, role := range expectedRoles {
		if outMsgs[i].Role != role {
			t.Errorf("expected role %s at index %d, got %s", role, i, outMsgs[i].Role)
		}
	}
}

func TestBuildCheckpointCompactedMessages_MixedConsumable(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Question 1"},
		{Role: "assistant", Content: "Answer 1"},
		{Role: "user", Content: "Question 2"},
		{Role: "assistant", Content: "Answer 2"},
		{Role: "user", Content: "Question 3"},
		{Role: "assistant", Content: "Answer 3"},
	}
	// Checkpoint 1: consumable (has summary).
	// Checkpoint 2: not consumable (empty summary).
	// Checkpoint 3: consumable.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "Summary 1"},
		{StartIndex: 2, EndIndex: 3, Summary: ""},
		{StartIndex: 4, EndIndex: 5, Summary: "Summary 3"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Checkpoint 1 consumed: [0,1] -> preserved Q1 + summary = 2 messages.
	// Checkpoint 2 unconsumable: kept as-is = Q2, A2 = 2 messages.
	// Checkpoint 3 consumed: [4,5] -> preserved Q3 + summary = 2 messages.
	// Result: preserved Q1 + summary1 + Q2 + A2 + preserved Q3 + summary3 = 6 messages.
	if len(outMsgs) != 6 {
		t.Errorf("expected 6 messages, got %d", len(outMsgs))
	}

	// All checkpoints returned (consumed ones kept for future re-application).
	if len(outCps) != 3 {
		t.Errorf("expected 3 checkpoints (all kept), got %d", len(outCps))
	}
	// Checkpoint indices unchanged (reference raw state.Messages()).
	if outCps[0].StartIndex != 0 || outCps[0].EndIndex != 1 {
		t.Errorf("expected checkpoint [0,1], got [%d,%d]", outCps[0].StartIndex, outCps[0].EndIndex)
	}
	if outCps[1].StartIndex != 2 || outCps[1].EndIndex != 3 {
		t.Errorf("expected checkpoint [2,3], got [%d,%d]", outCps[1].StartIndex, outCps[1].EndIndex)
	}
	if outCps[2].StartIndex != 4 || outCps[2].EndIndex != 5 {
		t.Errorf("expected checkpoint [4,5], got [%d,%d]", outCps[2].StartIndex, outCps[2].EndIndex)
	}
}

func TestBuildCheckpointCompactedMessages_InvalidRange(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	// StartIndex > EndIndex is invalid.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 0, Summary: "invalid range"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Should be unchanged.
	if len(outMsgs) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(outMsgs))
	}
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (not consumed), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_OutOfRangeEndIndex(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 100, Summary: "out of range"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Not consumable — EndIndex out of bounds.
	if len(outMsgs) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(outMsgs))
	}
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (not consumed), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_OutOfRangeStartIndex(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: -1, EndIndex: 0, Summary: "negative start"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Not consumable — negative StartIndex.
	if len(outMsgs) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(outMsgs))
	}
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (not consumed), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_OriginalNotModified(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "original 1"},
		{Role: "assistant", Content: "original 2"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "replaced"},
	}

	// Copy originals for comparison.
	origMsgs := make([]Message, len(msgs))
	copy(origMsgs, msgs)

	_, _ = BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Verify originals unchanged.
	for i := range msgs {
		if msgs[i].Content != origMsgs[i].Content {
			t.Errorf("original message %d was mutated: %q -> %q", i, origMsgs[i].Content, msgs[i].Content)
		}
	}
}

func TestBuildCheckpointCompactedMessages_AllButLastConsumed(t *testing.T) {
	// Three consumed checkpoints covering [0,1], [2,3], [4,5],
	// but 8 messages total — indices 6,7 are not covered.
	msgs := []Message{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "Q2"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "Q3"},
		{Role: "assistant", Content: "A3"},
		{Role: "user", Content: "Q4"},
		{Role: "assistant", Content: "A4"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "S1"},
		{StartIndex: 2, EndIndex: 3, Summary: "S2"},
		{StartIndex: 4, EndIndex: 5, Summary: "S3"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 3 preserved users + 3 summaries + Q4 + A4 = 8 messages (indices 6,7 not covered)
	if len(outMsgs) != 8 {
		t.Errorf("expected 8 messages, got %d", len(outMsgs))
	}
	// All consumed checkpoints are kept for future re-application.
	if len(outCps) != 3 {
		t.Errorf("expected 3 checkpoints (kept for future re-application), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_CheckpointSummariesOnly(t *testing.T) {
	// Test with only summaries (no tool calls, just user/assistant pairs).
	msgs := []Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "First question with lots of content"},
		{Role: "assistant", Content: "First answer with lots of content"},
		{Role: "user", Content: "Second question with lots of content"},
		{Role: "assistant", Content: "Second answer with lots of content"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 2, Summary: "User asked about topic A."},
		{StartIndex: 3, EndIndex: 4, Summary: "User asked about topic B."},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 system + 2 preserved users + 2 summaries = 5 messages.
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Role != "system" {
		t.Errorf("first message should be system, got %s", outMsgs[0].Role)
	}
	if outMsgs[1].Role != "user" || outMsgs[1].Content != "First question with lots of content" {
		t.Errorf("second message wrong: %+v", outMsgs[1])
	}
	if outMsgs[2].Role != "user" || outMsgs[2].Content != "User asked about topic A." {
		t.Errorf("third message wrong: %+v", outMsgs[2])
	}
	if outMsgs[3].Role != "user" || outMsgs[3].Content != "Second question with lots of content" {
		t.Errorf("fourth message wrong: %+v", outMsgs[3])
	}
	if outMsgs[4].Role != "user" || outMsgs[4].Content != "User asked about topic B." {
		t.Errorf("fifth message wrong: %+v", outMsgs[4])
	}
	// Both consumed checkpoints are kept for future re-application.
	if len(outCps) != 2 {
		t.Errorf("expected 2 checkpoints (kept for future re-application), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_CheckpointAfterConsecutiveAssistant(t *testing.T) {
	// Edge case: the checkpoint range is [1, 1] — only the assistant message.
	// Message 0 is also assistant. After replacement, message at index 0 (assistant)
	// and index 1 (summary, "user") are consecutive — but since summary is "user",
	// this is fine. The consecutive-assistant guard shouldn't trigger.
	msgs := []Message{
		{Role: "user", Content: "Query"},
		{Role: "assistant", Content: "First thought"},
		{Role: "user", Content: "Follow-up"},
		{Role: "assistant", Content: "Second response"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 1, Summary: "Assistant had a thought."},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Checkpoint [1,1] is consumed → 1 summary replaces 1 msg, delta=0.
	// Result: user, summary(user), user, assistant = 4 messages.
	if len(outMsgs) != 4 {
		t.Errorf("expected 4 messages, got %d", len(outMsgs))
	}
	// Consumed checkpoint is kept for future re-application.
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (kept for future re-application), got %d", len(outCps))
	}
	// Verify structure: user(summary), summary, user, assistant
	if outMsgs[0].Role != "user" || outMsgs[0].Content != "Query" {
		t.Errorf("first message wrong: %+v", outMsgs[0])
	}
	if outMsgs[1].Role != "user" || outMsgs[1].Content != "Assistant had a thought." {
		t.Errorf("second message wrong: %+v", outMsgs[1])
	}
}

func TestBuildCheckpointCompactedMessages_ToolCallTurn(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "List files"},
		{Role: "assistant", Content: "I'll list files.", ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "list_files", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "file1.txt, file2.txt", ToolCallID: "call_1"},
		{Role: "assistant", Content: "Here are the files."},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 4, Summary: "User asked to list files. Assistant listed file1.txt and file2.txt."},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 system + 1 preserved user + 1 summary = 3 messages.
	if len(outMsgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Role != "system" {
		t.Errorf("expected system first, got %s", outMsgs[0].Role)
	}
	if outMsgs[1].Role != "user" || outMsgs[1].Content != "List files" {
		t.Errorf("expected preserved user query at [1], got %+v", outMsgs[1])
	}
	if outMsgs[2].Content != "User asked to list files. Assistant listed file1.txt and file2.txt." {
		t.Errorf("unexpected summary: %s", outMsgs[2].Content)
	}
}

func TestBuildCheckpointCompactedMessages_AllMessagesConsumed(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	// Single checkpoint covering all messages — should produce one preserved user + one summary.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "consumed"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Both messages consumed: 1 preserved user + 1 summary = 2 messages.
	if len(outMsgs) != 2 {
		t.Errorf("expected 2 messages (user + summary), got %d", len(outMsgs))
	}
	// Consumed checkpoint is kept for future re-application.
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (kept for future re-application), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_ThreeCheckpointsWithOneUnconsumed(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "Q2"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "Q3"},
		{Role: "assistant", Content: "A3"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "S1"}, // consumed
		{StartIndex: 2, EndIndex: 3, Summary: ""},   // unconsumable (empty)
		{StartIndex: 4, EndIndex: 5, Summary: "S3"}, // consumed
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// First consumed: [0,1] -> preserved user + summary (2 msgs). 1 message removed.
	// Third consumed: [4,5] -> preserved user + summary (2 msgs). 1 message removed.
	// Result: 1 preserved user + 1 summary + user2 + assistant2 + 1 preserved user + 1 summary = 6 messages.
	if len(outMsgs) != 6 {
		t.Errorf("expected 6 messages, got %d", len(outMsgs))
	}

	// All checkpoints returned (consumed ones kept for future re-application).
	if len(outCps) != 3 {
		t.Errorf("expected 3 checkpoints (all kept), got %d", len(outCps))
	}
	// Checkpoint indices unchanged (reference raw state.Messages()).
	if outCps[0].StartIndex != 0 || outCps[0].EndIndex != 1 {
		t.Errorf("expected checkpoint [0,1], got [%d,%d]", outCps[0].StartIndex, outCps[0].EndIndex)
	}
	if outCps[1].StartIndex != 2 || outCps[1].EndIndex != 3 {
		t.Errorf("expected checkpoint [2,3], got [%d,%d]", outCps[1].StartIndex, outCps[1].EndIndex)
	}
	if outCps[2].StartIndex != 4 || outCps[2].EndIndex != 5 {
		t.Errorf("expected checkpoint [4,5], got [%d,%d]", outCps[2].StartIndex, outCps[2].EndIndex)
	}
}

// --- Overlapping checkpoints test ---

func TestBuildCheckpointCompactedMessages_OverlappingCheckpoints(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "Q2"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "Q3"},
		{Role: "assistant", Content: "A3"},
	}
	// Two overlapping checkpoints: [0,2] and [2,4] — message 2 is shared.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 2, Summary: "S1"},
		{StartIndex: 2, EndIndex: 4, Summary: "S2"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// First consumed [0,2]: preserved user Q1 + summary S1.
	// Second overlaps so it's not consumed.
	// Result: preserved Q1 + summary S1 + msgs[3] + msgs[4] + msgs[5] = 5 messages.
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(outMsgs))
	}
	// Original user Q1 is preserved before the summary.
	if outMsgs[0].Content != "Q1" {
		t.Errorf("expected preserved Q1 at [0], got %q", outMsgs[0].Content)
	}
	if outMsgs[1].Content != "S1" {
		t.Errorf("expected S1 summary at [1], got %q", outMsgs[1].Content)
	}

	// Both checkpoints returned (first consumed, second overlaps so unconsumed).
	if len(outCps) != 2 {
		t.Errorf("expected 2 checkpoints (all kept), got %d", len(outCps))
	}
	// Checkpoint indices unchanged (reference raw state.Messages()).
	if outCps[0].StartIndex != 0 || outCps[0].EndIndex != 2 {
		t.Errorf("expected checkpoint [0,2], got [%d,%d]", outCps[0].StartIndex, outCps[0].EndIndex)
	}
	if outCps[1].StartIndex != 2 || outCps[1].EndIndex != 4 {
		t.Errorf("expected checkpoint [2,4], got [%d,%d]", outCps[1].StartIndex, outCps[1].EndIndex)
	}
}

func TestBuildCheckpointCompactedMessages_ConsecutiveAssistantMerge(t *testing.T) {
	// Two assistant messages in the original array that become adjacent
	// after compaction (no checkpoint covers them). Phase 3 should merge them.
	msgs := []Message{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "assistant", Content: "A2"}, // consecutive assistant — unusual but possible
		{Role: "user", Content: "Q3"},
		{Role: "assistant", Content: "A3"},
	}
	// Consume only [0,0], leaving [1,2] as consecutive assistants.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "S1"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// After fix: preserved Q1 + summary S1 replaces only Q1.
	// Result: preserved Q1 + summary S1 + A1 + A2 + Q3 + A3 = 6 messages.
	// Then resolveConsecutiveAssistantMessages merges A1 and A2 = 5 messages.
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(outMsgs))
	}
	// The preserved Q1 should be at index 0.
	if outMsgs[0].Content != "Q1" {
		t.Errorf("expected preserved Q1 at index 0, got %q", outMsgs[0].Content)
	}
	// The two consecutive assistants should be merged.
	if outMsgs[2].Role != "assistant" {
		t.Errorf("expected assistant at index 2, got %s", outMsgs[2].Role)
	}
	if !strings.Contains(outMsgs[2].Content, "A1") || !strings.Contains(outMsgs[2].Content, "A2") {
		t.Errorf("expected merged content, got %q", outMsgs[2].Content)
	}
	// Consumed checkpoint is kept for future re-application.
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (kept for future re-application), got %d", len(outCps))
	}
}

// --- ActionableSummary selection tests ---

func TestBuildCheckpointCompactedMessages_UseActionableSummaryWhenShort(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "Done."},
	}
	actionable := "- Modified: core/main.go\n- Command: go build ./..."
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "Brief summary.", ActionableSummary: actionable},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 preserved user + 1 summary = 2 messages.
	if len(outMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Content != "Do something" {
		t.Errorf("expected preserved user query at [0], got: %s", outMsgs[0].Content)
	}
	if outMsgs[1].Content != actionable {
		t.Errorf("expected actionable summary at [1], got: %s", outMsgs[1].Content)
	}
}

func TestBuildCheckpointCompactedMessages_FallbackToSummaryWhenActionableTooLong(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "Done."},
	}
	// 501 characters — exceeds the 500-char guard.
	longActionable := strings.Repeat("x", 501)
	fallbackSummary := "Short summary."
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: fallbackSummary, ActionableSummary: longActionable},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 preserved user + 1 summary = 2 messages.
	if len(outMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Content != "Do something" {
		t.Errorf("expected preserved user query at [0], got: %s", outMsgs[0].Content)
	}
	if outMsgs[1].Content != fallbackSummary {
		t.Errorf("expected fallback to summary at [1], got: %s", outMsgs[1].Content)
	}
}

func TestBuildCheckpointCompactedMessages_UseActionableSummaryAtExactly500Chars(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "Done."},
	}
	// Exactly 500 characters — should be accepted.
	exactly500 := strings.Repeat("y", 500)
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "Fallback.", ActionableSummary: exactly500},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 preserved user + 1 summary = 2 messages.
	if len(outMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Content != "Do something" {
		t.Errorf("expected preserved user query at [0], got: %s", outMsgs[0].Content)
	}
	if outMsgs[1].Content != exactly500 {
		t.Errorf("expected actionable summary at exactly 500 chars at [1], got len %d", len(outMsgs[1].Content))
	}
}

func TestBuildCheckpointCompactedMessages_EmptyActionableFallsBackToSummary(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "Done."},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "Fallback summary.", ActionableSummary: ""},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// 1 preserved user + 1 summary = 2 messages.
	if len(outMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(outMsgs))
	}
	if outMsgs[0].Content != "Do something" {
		t.Errorf("expected preserved user query at [0], got: %s", outMsgs[0].Content)
	}
	if outMsgs[1].Content != "Fallback summary." {
		t.Errorf("expected fallback to summary at [1] when actionable is empty, got: %s", outMsgs[1].Content)
	}
}

func TestIsConsumableCheckpoint_Valid(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 0, EndIndex: 1, Summary: "test"}
	if !isConsumableCheckpoint(cp, 5) {
		t.Error("expected consumable")
	}
}

func TestIsConsumableCheckpoint_EmptySummary(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 0, EndIndex: 1, Summary: ""}
	if isConsumableCheckpoint(cp, 5) {
		t.Error("expected not consumable due to empty summary")
	}
}

func TestIsConsumableCheckpoint_NegativeStart(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: -1, EndIndex: 1, Summary: "test"}
	if isConsumableCheckpoint(cp, 5) {
		t.Error("expected not consumable due to negative start")
	}
}

func TestIsConsumableCheckpoint_NegativeEnd(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 0, EndIndex: -1, Summary: "test"}
	if isConsumableCheckpoint(cp, 5) {
		t.Error("expected not consumable due to negative end")
	}
}

func TestIsConsumableCheckpoint_StartGreaterThanEnd(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 2, EndIndex: 1, Summary: "test"}
	if isConsumableCheckpoint(cp, 5) {
		t.Error("expected not consumable due to start > end")
	}
}

func TestIsConsumableCheckpoint_EndOutOfBounds(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 0, EndIndex: 5, Summary: "test"}
	if isConsumableCheckpoint(cp, 5) {
		t.Error("expected not consumable due to end out of bounds")
	}
}

func TestIsConsumableCheckpoint_EndEqualsLenMinus1(t *testing.T) {
	cp := TurnCheckpoint{StartIndex: 4, EndIndex: 4, Summary: "test"}
	if !isConsumableCheckpoint(cp, 5) {
		t.Error("expected consumable — end is last valid index")
	}
}

// --- Restored tests from original_test.go ---

func TestBuildCheckpointSummary_SimpleTurn(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "What is the capital of France?"},
		{Role: "assistant", Content: "The capital of France is Paris."},
	}

	cp := BuildCheckpointSummary(messages)

	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex != 1 {
		t.Errorf("expected EndIndex 1, got %d", cp.EndIndex)
	}
	if !strings.Contains(cp.Summary, "User asked") {
		t.Errorf("summary should mention user question: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "Paris") {
		t.Errorf("summary should mention response content: %s", cp.Summary)
	}
	if !strings.Contains(cp.ActionableSummary, "Question") {
		t.Errorf("actionable summary should mention question: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_WithToolCalls(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read config.yaml"},
		{Role: "assistant", Content: "Let me read that file.", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			}},
		}},
		{Role: "tool", Content: "host: localhost\nport: 5432", ToolCallID: "call_1"},
		{Role: "assistant", Content: "The config has host localhost on port 5432."},
	}

	cp := BuildCheckpointSummary(messages)

	if !strings.Contains(cp.Summary, "read_file") {
		t.Errorf("summary should mention read_file tool: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "config.yaml") {
		t.Errorf("summary should mention config.yaml: %s", cp.Summary)
	}
	if !strings.Contains(cp.ActionableSummary, "Read: config.yaml") {
		t.Errorf("actionable summary should list file read: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_WithFileWrites(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Create a new file"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"path":"output.txt","content":"hello"}`,
			}},
		}},
		{Role: "tool", Content: "File written successfully", ToolCallID: "call_1"},
		{Role: "assistant", Content: "File created."},
	}

	cp := BuildCheckpointSummary(messages)

	if !strings.Contains(cp.Summary, "Modified files") {
		t.Errorf("summary should mention modified files: %s", cp.Summary)
	}
	if !strings.Contains(cp.ActionableSummary, "Modified: output.txt") {
		t.Errorf("actionable summary should list modified file: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_WithShellCommands(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "List files in directory"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"ls -la /tmp"}`,
			}},
		}},
		{Role: "tool", Content: "total 8\ndrwxrwxrwt 2 root root 4096 Jan 1 00:00 .", ToolCallID: "call_1"},
		{Role: "assistant", Content: "Directory listing shows 8 total items."},
	}

	cp := BuildCheckpointSummary(messages)

	if !strings.Contains(cp.Summary, "shell") {
		t.Errorf("summary should mention shell tool: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "ls -la /tmp") {
		t.Errorf("summary should mention command: %s", cp.Summary)
	}
	if !strings.Contains(cp.ActionableSummary, "Command: ls -la /tmp") {
		t.Errorf("actionable summary should list command: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_WithError(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read missing file"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"nonexistent.txt"}`,
			}},
		}},
		{Role: "tool", Content: "error: file not found", ToolCallID: "call_1"},
		{Role: "assistant", Content: "The file doesn't exist."},
	}

	cp := BuildCheckpointSummary(messages)

	if !strings.Contains(cp.Summary, "error") {
		t.Errorf("summary should mention errors: %s", cp.Summary)
	}
	if !strings.Contains(cp.ActionableSummary, "Error") {
		t.Errorf("actionable summary should mention error: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_MultipleToolCalls(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read two files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"a.txt"}`,
			}},
			{ID: "call_2", Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"b.txt"}`,
			}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call_1"},
		{Role: "tool", Content: "content b", ToolCallID: "call_2"},
		{Role: "assistant", Content: "Both files read successfully."},
	}

	cp := BuildCheckpointSummary(messages)

	// Should mention read_file (2x)
	if !strings.Contains(cp.Summary, "read_file") {
		t.Errorf("summary should mention read_file: %s", cp.Summary)
	}
	// Should list both files in actionable summary
	if !strings.Contains(cp.ActionableSummary, "Read: a.txt") {
		t.Errorf("actionable summary should list a.txt: %s", cp.ActionableSummary)
	}
	if !strings.Contains(cp.ActionableSummary, "Read: b.txt") {
		t.Errorf("actionable summary should list b.txt: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_TruncatedResponse(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Explain something"},
		{Role: "assistant", Content: "Here is the explanation..."},
	}

	cp := BuildCheckpointSummary(messages)

	// Partial status due to trailing "..."
	if !strings.Contains(cp.Summary, "partial") {
		t.Errorf("summary should indicate partial status: %s", cp.Summary)
	}
}

func TestBuildCheckpointSummary_LongResponse(t *testing.T) {
	longContent := strings.Repeat("word ", 200) + "end"
	messages := []Message{
		{Role: "user", Content: "Long question"},
		{Role: "assistant", Content: longContent},
	}

	cp := BuildCheckpointSummary(messages)

	// Summary should be truncated
	if len(cp.Summary) > 500 {
		t.Errorf("summary should be reasonably short, got %d chars", len(cp.Summary))
	}
	// Actionable summary response should be truncated
	if !strings.Contains(cp.ActionableSummary, "...") {
		t.Errorf("actionable summary should truncate long response: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_MixedOperations(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Refactor the code"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"src/main.go"}`,
			}},
		}},
		{Role: "tool", Content: "package main\nfunc main() {}", ToolCallID: "call_1"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_2", Function: ToolCallFunction{
				Name:      "edit_file",
				Arguments: `{"path":"src/main.go","old_str":"func main() {}","new_str":"func main() { println(\"hi\") }"}`,
			}},
		}},
		{Role: "tool", Content: "File edited successfully", ToolCallID: "call_2"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_3", Function: ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"go build ./..."}`,
			}},
		}},
		{Role: "tool", Content: "Build succeeded", ToolCallID: "call_3"},
		{Role: "assistant", Content: "Refactored and verified the build."},
	}

	cp := BuildCheckpointSummary(messages)

	// Should mention reading and modifying files
	if !strings.Contains(cp.Summary, "Read files") {
		t.Errorf("summary should mention file reads: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "Modified files") {
		t.Errorf("summary should mention file modifications: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "shell") {
		t.Errorf("summary should mention shell command: %s", cp.Summary)
	}

	// Actionable summary should list operations
	if !strings.Contains(cp.ActionableSummary, "Read: src/main.go") {
		t.Errorf("actionable summary should list file read: %s", cp.ActionableSummary)
	}
	if !strings.Contains(cp.ActionableSummary, "Modified: src/main.go") {
		t.Errorf("actionable summary should list file modification: %s", cp.ActionableSummary)
	}
	if !strings.Contains(cp.ActionableSummary, "Command: go build ./...") {
		t.Errorf("actionable summary should list command: %s", cp.ActionableSummary)
	}
}

func TestBuildCheckpointSummary_EmptyMessages(t *testing.T) {
	cp := BuildCheckpointSummary([]Message{})

	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex != -1 {
		t.Errorf("expected EndIndex -1 for empty, got %d", cp.EndIndex)
	}
}

func TestBuildCheckpointSummary_OnlyUserMessage(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	cp := BuildCheckpointSummary(messages)

	if !strings.Contains(cp.Summary, "User asked") {
		t.Errorf("summary should mention user question: %s", cp.Summary)
	}
}

func TestTurnSummaryBuilder_CustomFileTools(t *testing.T) {
	builder := NewTurnSummaryBuilder()
	builder.KnownFileTools = map[string]bool{
		"custom_read": true,
	}

	messages := []Message{
		{Role: "user", Content: "Read with custom tool"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{
				Name:      "custom_read",
				Arguments: `{"path":"custom.txt"}`,
			}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "call_1"},
		{Role: "assistant", Content: "Done."},
	}

	cp := builder.Build(messages)

	if !strings.Contains(cp.Summary, "custom.txt") {
		t.Errorf("summary should mention custom file: %s", cp.Summary)
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		expected string
	}{
		{"path key", `{"path":"src/main.go"}`, "src/main.go"},
		{"file key", `{"file":"data.json"}`, "data.json"},
		{"filename key", `{"filename":"test.txt"}`, "test.txt"},
		{"no path", `{"cmd":"ls"}`, ""},
		{"empty", `{}`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFilePath(tt.args)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractShellCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		expected string
	}{
		{"cmd key", `{"cmd":"ls -la"}`, "ls -la"},
		{"command key", `{"command":"go build"}`, "go build"},
		{"no command", `{"path":"file.txt"}`, ""},
		{"empty", `{}`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractShellCommand(tt.args)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world foo bar", 10, "hello..."},
		{"truncated mid-word", "abcdefghij", 5, "abcde..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestUniqueStrings(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	expected := []string{"a", "b", "c"}

	result := uniqueStrings(input)
	if len(result) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %q, got %q", i, expected[i], v)
		}
	}
}

func TestRecordTurnCheckpointAsync(t *testing.T) {
	state := NewState()

	messages := []Message{
		{Role: "user", Content: "Test query"},
		{Role: "assistant", Content: "Test response."},
	}

	RecordTurnCheckpointAsync(state, messages, 0, 1, 5*time.Second, nil)

	// Wait for async completion
	time.Sleep(100 * time.Millisecond)

	checkpoints := state.GetCheckpoints()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex != 1 {
		t.Errorf("expected EndIndex 1, got %d", cp.EndIndex)
	}
	if !strings.Contains(cp.Summary, "Test query") {
		t.Errorf("summary should mention query: %s", cp.Summary)
	}
}

func TestRecordTurnCheckpointAsync_Timeout(t *testing.T) {
	state := NewState()

	// Very large message set to test timeout path
	messages := make([]Message, 0, 1000)
	for i := 0; i < 500; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 1000)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 1000)})
	}

	RecordTurnCheckpointAsync(state, messages, 0, len(messages)-1, 1*time.Millisecond, nil)

	// Wait for async completion
	time.Sleep(100 * time.Millisecond)

	checkpoints := state.GetCheckpoints()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	// Even on timeout, checkpoint should be stored
	cp := checkpoints[0]
	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
}

func TestRecordTurnCheckpointAsync_SnapshotIsolation(t *testing.T) {
	state := NewState()

	messages := []Message{
		{Role: "user", Content: "Original query"},
		{Role: "assistant", Content: "Original response."},
	}

	RecordTurnCheckpointAsync(state, messages, 0, 1, 5*time.Second, nil)

	// Mutate the original slice immediately after calling.
	// The async goroutine should see the snapshot, not these mutations.
	messages[0].Content = "Mutated query"
	messages[1].Content = "Mutated response."
	messages = append(messages, Message{Role: "user", Content: "Extra"})

	// Wait for async completion.
	time.Sleep(200 * time.Millisecond)

	checkpoints := state.GetCheckpoints()
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	cp := checkpoints[0]
	// Summary should reference the original content, not the mutated content.
	if strings.Contains(cp.Summary, "Mutated") {
		t.Errorf("summary should use snapshot, not mutated data: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "Original") {
		t.Errorf("summary should contain original content: %s", cp.Summary)
	}
}

func TestIsFileWriteTool(t *testing.T) {
	writeTools := []string{"write_file", "edit_file", "create_file", "delete_file", "patch_file", "append_file", "write_structured", "patch_structured"}
	readTools := []string{"read_file", "list_files", "search_files", "glob_files"}

	for _, tool := range writeTools {
		if !isFileWriteTool(tool) {
			t.Errorf("expected %s to be a write tool", tool)
		}
	}
	for _, tool := range readTools {
		if isFileWriteTool(tool) {
			t.Errorf("expected %s to NOT be a write tool", tool)
		}
	}
}

func TestDetermineStatus(t *testing.T) {
	builder := NewTurnSummaryBuilder()

	// Completed status
	completedData := turnData{
		userQuestion:  "What is 2+2?",
		finalResponse: "The answer is 4.",
	}
	if builder.determineStatus(completedData) != statusCompleted {
		t.Errorf("expected completed status, got %v", builder.determineStatus(completedData))
	}

	// Partial status (trailing ...)
	partialData := turnData{
		userQuestion:  "Explain",
		finalResponse: "Here is the explanation...",
	}
	if builder.determineStatus(partialData) != statusPartial {
		t.Errorf("expected partial status, got %v", builder.determineStatus(partialData))
	}

	// Interrupted status (no final response but tool calls)
	interruptedData := turnData{
		userQuestion: "Do something",
		toolCalls:    []toolCallInfo{{name: "shell", arguments: `{"cmd":"ls"}`}},
	}
	if builder.determineStatus(interruptedData) != statusInterrupted {
		t.Errorf("expected interrupted status, got %v", builder.determineStatus(interruptedData))
	}

	// Error status (errors but no final response)
	errorData := turnData{
		userQuestion: "Fail",
		errors:       []string{"error: something failed"},
	}
	if builder.determineStatus(errorData) != statusError {
		t.Errorf("expected error status, got %v", builder.determineStatus(errorData))
	}
}

// --- resolveConsecutiveAssistantMessages tests ---

func TestResolveConsecutiveAssistantMessages_both_plain_merge_content(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "first thought"},
		{Role: "assistant", Content: "second thought"},
		{Role: "user", Content: "next question"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// Should have 3 messages: user, merged assistant, user.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != "user" || result[0].Content != "hello" {
		t.Errorf("first message wrong: %+v", result[0])
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected assistant at index 1, got %s", result[1].Role)
	}
	if !strings.Contains(result[1].Content, "first thought") {
		t.Errorf("merged content missing 'first thought': %q", result[1].Content)
	}
	if !strings.Contains(result[1].Content, "second thought") {
		t.Errorf("merged content missing 'second thought': %q", result[1].Content)
	}
	if result[2].Role != "user" || result[2].Content != "next question" {
		t.Errorf("third message wrong: %+v", result[2])
	}
}

func TestResolveConsecutiveAssistantMessages_first_has_tools_second_plain_drop_second(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "let me do something", ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "shell", Arguments: `{}`}},
		}},
		{Role: "assistant", Content: "just a plain note"},
		{Role: "user", Content: "next"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// Should have 3 messages: user, assistant(with tools), user. Second assistant dropped.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected assistant at index 1, got %s", result[1].Role)
	}
	// First assistant retains its tool calls.
	if len(result[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call on first assistant, got %d", len(result[1].ToolCalls))
	}
	// Second assistant content IS merged (preserved for context).
	if !strings.Contains(result[1].Content, "just a plain note") {
		t.Error("second assistant content was not merged")
	}
	// Input slice was not mutated.
	if msgs[1].Content != "let me do something" {
		t.Error("input slice was mutated")
	}
}

func TestResolveConsecutiveAssistantMessages_first_plain_second_has_tools_merge_and_transfer(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "initial response"},
		{Role: "assistant", Content: "follow-up with action", ToolCalls: []ToolCall{
			{ID: "call_2", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "user", Content: "next"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// Should have 3 messages: user, merged assistant, user.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected assistant at index 1, got %s", result[1].Role)
	}
	// Content from both messages should be merged.
	if !strings.Contains(result[1].Content, "initial response") {
		t.Errorf("merged content missing 'initial response': %q", result[1].Content)
	}
	if !strings.Contains(result[1].Content, "follow-up with action") {
		t.Errorf("merged content missing 'follow-up with action': %q", result[1].Content)
	}
	// Tool calls from the second message should be transferred to the first.
	if len(result[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call transferred, got %d", len(result[1].ToolCalls))
	}
	if result[1].ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected tool call name 'read_file', got %q", result[1].ToolCalls[0].Function.Name)
	}
}

func TestResolveConsecutiveAssistantMessages_both_have_tools_drop_second(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "first with tools", ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "shell", Arguments: `{}`}},
		}},
		{Role: "assistant", Content: "second with tools", ToolCalls: []ToolCall{
			{ID: "call_2", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "user", Content: "next"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// Should have 3 messages: user, first assistant (with tools), user. Second dropped.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected assistant at index 1, got %s", result[1].Role)
	}
	// First assistant's tool calls should be preserved.
	if len(result[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result[1].ToolCalls))
	}
	if result[1].ToolCalls[0].Function.Name != "shell" {
		t.Errorf("expected tool call name 'shell', got %q", result[1].ToolCalls[0].Function.Name)
	}
	// Second assistant's content IS merged (preserved for context).
	if !strings.Contains(result[1].Content, "second with tools") {
		t.Error("second assistant content was not merged")
	}
	// Second assistant's tool calls should NOT be present (only first's are kept).
	if len(result[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result[1].ToolCalls))
	}
	if result[1].ToolCalls[0].Function.Name != "shell" {
		t.Errorf("expected tool call name 'shell', got %q", result[1].ToolCalls[0].Function.Name)
	}
	// Input slice was not mutated.
	if msgs[1].Content != "first with tools" {
		t.Error("input slice was mutated")
	}
}

func TestResolveConsecutiveAssistantMessages_no_consecutive_unchanged(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow-up"},
		{Role: "assistant", Content: "another answer"},
		{Role: "user", Content: "final"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// No consecutive assistant messages, so result should be identical.
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
	for i := range msgs {
		if result[i].Role != msgs[i].Role || result[i].Content != msgs[i].Content {
			t.Errorf("message %d changed: expected %+v, got %+v", i, msgs[i], result[i])
		}
	}
}

func TestResolveConsecutiveAssistantMessages_three_consecutive_merged_pairwise(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "thought 1"},
		{Role: "assistant", Content: "thought 2"},
		{Role: "assistant", Content: "thought 3"},
		{Role: "user", Content: "next"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// All three consecutive assistants should be merged into one.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected assistant at index 1, got %s", result[1].Role)
	}
	// All three contents should be merged.
	for _, expected := range []string{"thought 1", "thought 2", "thought 3"} {
		if !strings.Contains(result[1].Content, expected) {
			t.Errorf("merged content missing %q: %q", expected, result[1].Content)
		}
	}
}

func TestResolveConsecutiveAssistantMessages_mixed_roles_unchanged(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow-up"},
		{Role: "assistant", Content: "another"},
		{Role: "assistant", Content: "more"}, // consecutive
		{Role: "assistant", Content: "last"}, // consecutive
		{Role: "user", Content: "final"},
	}

	result := resolveConsecutiveAssistantMessages(msgs)

	// Should have: user, assistant, user, merged assistant, user = 5 messages.
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}
	// Verify no consecutive assistant messages remain.
	for i := 1; i < len(result); i++ {
		if result[i-1].Role == "assistant" && result[i].Role == "assistant" {
			t.Errorf("consecutive assistants at indices %d and %d: %+v, %+v", i-1, i, result[i-1], result[i])
		}
	}
	// Verify merged content has both messages.
	if !strings.Contains(result[3].Content, "more") || !strings.Contains(result[3].Content, "last") {
		t.Errorf("merged assistant content wrong: %q", result[3].Content)
	}
}

func TestResolveConsecutiveAssistantMessages_empty_input(t *testing.T) {
	result := resolveConsecutiveAssistantMessages([]Message{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d messages", len(result))
	}
}

func TestResolveConsecutiveAssistantMessages_single_message(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "only message"},
	}
	result := resolveConsecutiveAssistantMessages(msgs)

	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "only message" {
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

// --- BuildCheckpointCompactedMessages consecutive-assistant boundary tests ---

func TestBuildCheckpointCompactedMessages_consecutive_assistant_boundary(t *testing.T) {
	// Scenario: checkpoint [0,0] consumes the user message, leaving two consecutive
	// assistant messages (indices 1 and 2) that must be merged.
	msgs := []Message{
		{Role: "user", Content: "initial query"},
		{Role: "assistant", Content: "first assistant response"},
		{Role: "assistant", Content: "second assistant response"},
		{Role: "user", Content: "follow-up query"},
		{Role: "assistant", Content: "final answer"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "User asked: initial query."},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// After fix: preserved "initial query" + summary replaces only the user message.
	// Result before merge: preserved user, summary, A1, A2, user, assistant = 6 messages.
	// resolveConsecutiveAssistantMessages merges A1 and A2 = 5 messages.
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(outMsgs), outMsgs)
	}
	// No consecutive assistants should remain.
	for i := 1; i < len(outMsgs); i++ {
		if outMsgs[i-1].Role == "assistant" && outMsgs[i].Role == "assistant" {
			t.Errorf("consecutive assistants at indices %d and %d", i-1, i)
		}
	}
	// The merged assistant should contain both responses at index 2.
	if !strings.Contains(outMsgs[2].Content, "first assistant response") {
		t.Errorf("merged content missing 'first assistant response': %q", outMsgs[2].Content)
	}
	if !strings.Contains(outMsgs[2].Content, "second assistant response") {
		t.Errorf("merged content missing 'second assistant response': %q", outMsgs[2].Content)
	}
	// Consumed checkpoint is kept for future re-application.
	if len(outCps) != 1 {
		t.Errorf("expected 1 checkpoint (kept for future re-application), got %d", len(outCps))
	}
}

func TestBuildCheckpointCompactedMessages_normal_compaction_no_consecutive(t *testing.T) {
	// Scenario: normal checkpoint compaction with alternating user/assistant
	// turns — this should NOT produce consecutive assistant messages.
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Question 1"},
		{Role: "assistant", Content: "Answer 1"},
		{Role: "user", Content: "Question 2"},
		{Role: "assistant", Content: "Answer 2"},
		{Role: "user", Content: "Question 3"},
		{Role: "assistant", Content: "Answer 3"},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 2, Summary: "Turn 1 summary"},
		{StartIndex: 3, EndIndex: 4, Summary: "Turn 2 summary"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// System + 2 preserved users + 2 summaries + last turn = 7 messages.
	if len(outMsgs) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(outMsgs))
	}
	// Verify the structure: system, preserved-Q1, summary1, preserved-Q2, summary2, user3, assistant3
	expectedRoles := []string{"system", "user", "user", "user", "user", "user", "assistant"}
	for i, role := range expectedRoles {
		if outMsgs[i].Role != role {
			t.Errorf("expected role %s at index %d, got %s", role, i, outMsgs[i].Role)
		}
	}
	// No consecutive assistant messages.
	for i := 1; i < len(outMsgs); i++ {
		if outMsgs[i-1].Role == "assistant" && outMsgs[i].Role == "assistant" {
			t.Errorf("unexpected consecutive assistants at indices %d and %d", i-1, i)
		}
	}
	// Both checkpoints consumed, kept for future re-application.
	if len(outCps) != 2 {
		t.Errorf("expected 2 checkpoints (kept for future re-application), got %d", len(outCps))
	}
}
