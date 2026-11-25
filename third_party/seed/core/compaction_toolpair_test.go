package core

import (
	"strings"
	"testing"
)

// When a tool pair is the ONLY content outside the recent tail, skipping both
// halves would leave the loop with nothing to drop and history still over
// target. The pair is removable as a unit, so Phase 3 must take it rather than
// give up.
func TestEmergencyTruncate_Phase3_DropsPairWhenItIsAllThatRemains(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "calling", ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
		}},
		{Role: "tool", Content: strings.Repeat("x", 3000), ToolCallID: "tc1"},
	}
	// Exactly the recent window follows, so indices 2 and 3 are the only
	// messages Phase 3 may consider.
	for i := 0; i < 12; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent u " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent a " + string(rune('a'+i))})
	}

	result, msgsDropped := emergencyTruncate(messages, 100)

	if !msgsDropped {
		t.Fatal("expected the tool pair to be dropped: it is the only content outside the recent tail")
	}

	var hasCall, hasResult bool
	for _, m := range result {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			hasCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "tc1" {
			hasResult = true
		}
	}
	if hasCall != hasResult {
		t.Errorf("tool pair split: call present=%v, result present=%v", hasCall, hasResult)
	}
}
