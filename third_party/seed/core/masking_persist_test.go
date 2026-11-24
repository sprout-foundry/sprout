package core

import (
	"fmt"
	"strings"
	"testing"
)

// toolResultContent returns the content of the first tool message matching
// callID, or "" if absent.
func toolResultContent(msgs []Message, callID string) string {
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	return ""
}

// maskSeedMessages builds a conversation with 8 large consumed tool results
// (the keep-last-5 window protects the newest 5, so exactly 3 are eligible for
// observation masking) followed by a final assistant message. Each result is
// ~6000 chars of distinct content so the dedup pass never collapses them.
func maskSeedMessages() []Message {
	msgs := []Message{{Role: "user", Content: "read all the files"}}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: strings.Repeat(fmt.Sprintf("%d", i), 6000)},
		)
	}
	return append(msgs, Message{Role: "assistant", Content: "done"})
}

func newMaskTestAgent(t *testing.T, provider Provider) *Agent {
	t.Helper()
	a, err := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
		Optimizer: NewConversationOptimizer(ConversationOptimizerOptions{
			Enabled: true,
			KnownToolFn: func(name string) ToolCategory {
				if name == "read_file" {
					return ToolCategoryFileRead
				}
				return ToolCategoryUnknown
			},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPrepareMessages_PersistsObservationMaskingToState(t *testing.T) {
	provider := &mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 9000, // above 0.85 × 10000 trigger → Phase 0b fires
	}
	a := newMaskTestAgent(t, provider)
	a.State().SetMessages(maskSeedMessages())

	ch := newConversationHandler(a)

	first := ch.prepareMessages()
	firstPlaceholder := toolResultContent(first, "call-0")
	if !strings.Contains(firstPlaceholder, "[PREVIOUS RESULT:") {
		t.Fatalf("expected masked placeholder in first prepareMessages, got %q", firstPlaceholder)
	}

	// Masking must be persisted to state so the next iteration doesn't redo it.
	stateTool := toolResultContent(a.State().Messages(), "call-0")
	if !strings.Contains(stateTool, "[PREVIOUS RESULT:") {
		t.Fatalf("expected masked content persisted to state, got %q", stateTool)
	}

	// The keep-last window must still be honored in state: newest results stay raw.
	stateNewest := toolResultContent(a.State().Messages(), "call-7")
	if stateNewest == "" || strings.Contains(stateNewest, "[PREVIOUS RESULT:") {
		t.Fatalf("expected newest tool result unmasked in state, got %q", stateNewest)
	}

	// Second prepareMessages must not re-mask: the placeholder is already there.
	second := ch.prepareMessages()
	secondPlaceholder := toolResultContent(second, "call-0")
	if !strings.Contains(secondPlaceholder, "[PREVIOUS RESULT:") {
		t.Fatalf("expected placeholder preserved on second prepareMessages, got %q", secondPlaceholder)
	}
	if secondPlaceholder != firstPlaceholder {
		t.Errorf("expected identical placeholder on second call, got %q vs %q", secondPlaceholder, firstPlaceholder)
	}
	// State still reflects the masked content (never restored).
	if got := toolResultContent(a.State().Messages(), "call-1"); !strings.Contains(got, "[PREVIOUS RESULT:") {
		t.Errorf("expected call-1 still masked in state, got %q", got)
	}
}

// TestPrepareMessages_PersistedMaskingKeepsCheckpointIndicesValid pins the
// invariant that persisted observation masking (Phase 0b) keeps checkpoint
// indices valid. Masking rewrites tool-result content in place without
// changing the message count, so TurnCheckpoint StartIndex/EndIndex
// references into the raw state slice survive it: after masking persists,
// BuildCheckpointCompactedMessages must still substitute the checkpointed
// early-turn ranges and place summaries at the expected positions.
func TestPrepareMessages_PersistedMaskingKeepsCheckpointIndicesValid(t *testing.T) {
	// Early plain turns (indices 0-3), a transition query (index 4), then 8
	// big consumed tool results (indices 5-20), then a final assistant (21).
	msgs := []Message{
		{Role: "user", Content: "turn 1 query"},
		{Role: "assistant", Content: "turn 1 answer"},
		{Role: "user", Content: "turn 2 query"},
		{Role: "assistant", Content: "turn 2 answer"},
		{Role: "user", Content: "read all the files"},
	}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: strings.Repeat(fmt.Sprintf("%d", i), 6000)},
		)
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "done"})

	provider := &mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 9000, // above 0.85 × 10000 trigger → Phase 0b fires
	}
	a := newMaskTestAgent(t, provider)
	a.State().SetMessages(msgs)

	// Trigger masking persistence: no checkpoints yet, so Phase 0b runs and
	// persists the index-stable masked content back to state.
	ch := newConversationHandler(a)
	ch.prepareMessages()
	if got := toolResultContent(a.State().Messages(), "call-0"); !strings.Contains(got, "[PREVIOUS RESULT:") {
		t.Fatalf("expected masking persisted to state, got %q", got)
	}
	stateMsgs := a.State().Messages()
	if len(stateMsgs) != len(msgs) {
		t.Fatalf("masking must not change the message count, got %d vs %d", len(stateMsgs), len(msgs))
	}

	// Now record turn checkpoints covering the early turns. Their indices
	// reference the raw state slice, which masking did not change.
	checkpoints := []TurnCheckpoint{
		{StartIndex: 0, EndIndex: 1, Summary: "Turn 1: user asked, assistant answered.", ActionableSummary: "- q1\n- a1"},
		{StartIndex: 2, EndIndex: 4, Summary: "Turn 2: user asked, assistant read files.", ActionableSummary: "- q2\n- read all the files"},
	}
	a.State().SetCheckpoints(checkpoints)

	compacted, _ := BuildCheckpointCompactedMessages(stateMsgs, checkpoints)

	// Message count shrinks by exactly the consumed range sizes minus the
	// inserted messages (summary + preserved leading user query where the
	// range starts with a real user turn).
	expectedShrink := 0
	for _, cp := range checkpoints {
		r := cp.EndIndex - cp.StartIndex + 1
		inserted := 1
		if head := stateMsgs[cp.StartIndex]; head.Role == "user" && !isCheckpointSummary(head) {
			inserted = 2
		}
		expectedShrink += r - inserted
	}
	if len(compacted) != len(stateMsgs)-expectedShrink {
		t.Fatalf("expected %d messages after compaction, got %d", len(stateMsgs)-expectedShrink, len(compacted))
	}

	// Summaries land at the expected positions with the checkpoint marker,
	// ahead of their preserved leading user queries.
	if compacted[1].Role != "user" || compacted[1].Meta[MetaKeyCheckpoint] != "true" {
		t.Errorf("expected checkpoint summary at [1], got %+v", compacted[1])
	}
	if compacted[3].Role != "user" || compacted[3].Meta[MetaKeyCheckpoint] != "true" {
		t.Errorf("expected checkpoint summary at [3], got %+v", compacted[3])
	}
	if compacted[0].Content != "turn 1 query" || compacted[2].Content != "turn 2 query" {
		t.Errorf("expected preserved leading queries at [0] and [2], got %q / %q", compacted[0].Content, compacted[2].Content)
	}
	// The persisted masked result still flows through at its shifted position.
	if got := toolResultContent(compacted, "call-0"); !strings.Contains(got, "[PREVIOUS RESULT:") {
		t.Errorf("expected masked call-0 preserved through checkpoint compaction, got %q", got)
	}
}

func TestPrepareMessages_NoMaskingWhenUnderTrigger(t *testing.T) {
	provider := &mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 1000, // under 0.85 × 10000 trigger → Phase 0 is a no-op
	}
	a := newMaskTestAgent(t, provider)
	seed := maskSeedMessages()
	a.State().SetMessages(seed)

	ch := newConversationHandler(a)
	prepared := ch.prepareMessages()

	// Raw content must flow through unchanged below the trigger.
	if got := toolResultContent(prepared, "call-0"); !strings.Contains(got, "000000") {
		t.Errorf("expected raw tool content under the trigger, got %q", got)
	}
	// And nothing may be persisted to state.
	if got := toolResultContent(a.State().Messages(), "call-0"); got != seed[2].Content {
		t.Errorf("expected state unchanged under the trigger, got content length %d", len(got))
	}
}
