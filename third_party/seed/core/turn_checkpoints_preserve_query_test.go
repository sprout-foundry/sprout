package core

import (
	"testing"
)

// TestBuildCheckpointCompactedMessages_PreservesOriginalUserQuery covers SP-128:
// strict chat templates (Qwen3.5) treat a synthetic summary at position N as the
// new query, dropping the original user request. The compaction must keep the
// leading user message so the model sees: real query → summary of what happened.
func TestBuildCheckpointCompactedMessages_PreservesOriginalUserQuery(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "What is the capital of France?"},
		{Role: "assistant", Content: "Paris."},
		{Role: "user", Content: "And Italy?"},
		{Role: "assistant", Content: "Rome."},
	}
	checkpoints := []TurnCheckpoint{
		{StartIndex: 1, EndIndex: 2, Summary: "User asked about capital of France, assistant answered Paris.", ActionableSummary: "- Q: capital of France\n- A: Paris"},
	}

	outMsgs, outCps := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Expected: [system, original-user-Q1, summary, original-user-Q2, assistant-A2]
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(outMsgs), outMsgs)
	}
	if outMsgs[0].Role != "system" {
		t.Errorf("expected system at [0], got %s", outMsgs[0].Role)
	}
	if outMsgs[1].Role != "user" || outMsgs[1].Content != "What is the capital of France?" {
		t.Errorf("expected original user query at [1], got %+v", outMsgs[1])
	}
	if outMsgs[2].Role != "user" || outMsgs[2].Content == "What is the capital of France?" {
		t.Errorf("expected summary at [2] (not duplicate of original), got %+v", outMsgs[2])
	}
	if outMsgs[3].Role != "user" || outMsgs[3].Content != "And Italy?" {
		t.Errorf("expected second user query at [3], got %+v", outMsgs[3])
	}
	if outMsgs[4].Role != "assistant" || outMsgs[4].Content != "Rome." {
		t.Errorf("expected assistant A2 at [4], got %+v", outMsgs[4])
	}
	if len(outCps) != 1 {
		t.Errorf("expected 1 returned checkpoint, got %d", len(outCps))
	}
}

// TestBuildCheckpointCompactedMessages_SkipPreservationForSyntheticSummary proves
// idempotency: re-applying a checkpoint whose leading message is already a
// synthetic summary (from a prior compaction pass) must NOT duplicate the
// summary at the head of the new range.
func TestBuildCheckpointCompactedMessages_SkipPreservationForSyntheticSummary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "prior summary", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "4."},
	}
	checkpoints := []TurnCheckpoint{
		// Re-apply a checkpoint covering the prior summary AND the next turn.
		// The leading message (index 1) is a synthetic summary, not a real
		// user turn — preservation must be skipped to avoid duplication.
		{StartIndex: 1, EndIndex: 2, Summary: "Sum was 4.", ActionableSummary: "- Q: 2+2\n- A: 4"},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)
	// Expected: [system, new-summary, assistant("4.")]
	// - index 0 (system) preserved
	// - indices [1,2] replaced by 1 summary (leading synthetic summary
	//   at index 1 is skipped to avoid duplicating it on re-application;
	//   "What is 2+2?" at index 2 is also consumed by the new range)
	// - index 3 (assistant "4.") preserved
	// The synthetic prior summary at index 1 should NOT be preserved (duplicated).
	if outMsgs[1].Content != "- Q: 2+2\n- A: 4" {
		t.Errorf("expected new summary at [1], got %q", outMsgs[1].Content)
	}
	// The assistant response should survive.
	if outMsgs[2].Role != "assistant" || outMsgs[2].Content != "4." {
		t.Errorf("expected assistant response at [2], got %+v", outMsgs[2])
	}
}

// TestBuildCheckpointCompactedMessages_PreservesUserWithNoAssistantTail covers
// a checkpoint range that is just a lone user message (no following assistant).
// E.g., a user query that errored out and was never answered — the compactor
// still records a checkpoint for it, but the range contains only the user turn.
// The user message must still be preserved.
func TestBuildCheckpointCompactedMessages_PreservesUserWithNoAssistantTail(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Abandoned question"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Answer."},
	}
	checkpoints := []TurnCheckpoint{
		// Range [1,1] covers only the abandoned user message.
		{StartIndex: 1, EndIndex: 1, Summary: "User asked an abandoned question.", ActionableSummary: "- User asked: Abandoned question"},
	}

	outMsgs, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)

	// Expected: [system, abandoned-query, summary, second-query, assistant]
	if len(outMsgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(outMsgs), outMsgs)
	}
	if outMsgs[1].Content != "Abandoned question" {
		t.Errorf("expected abandoned query at [1], got %q", outMsgs[1].Content)
	}
	if outMsgs[2].Role != "user" || outMsgs[2].Content == "Abandoned question" {
		t.Errorf("expected summary at [2] (not duplicate), got %+v", outMsgs[2])
	}
}
