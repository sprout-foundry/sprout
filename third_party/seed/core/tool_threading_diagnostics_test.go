package core

import (
	"testing"
)

// --- ValidateToolThreading ---

func TestValidateToolThreading_Nil(t *testing.T) {
	violations := ValidateToolThreading(nil)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for nil, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_Empty(t *testing.T) {
	violations := ValidateToolThreading([]Message{})
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for empty, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_NoToolCalls(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_SingleCallAndResult(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read a file"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-abc123", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "file content", ToolCallID: "call-abc123"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_MultipleCallsInOrder(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_OutOfOrder(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		// Results reversed
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Kind != ToolThreadingViolationOutOfOrder {
		t.Errorf("expected out_of_order, got %q", violations[0].Kind)
	}
}

func TestValidateToolThreading_MissingResult(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		// Only one result
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Kind != ToolThreadingViolationMissingResult {
		t.Errorf("expected missing_result, got %q", violations[0].Kind)
	}
	if violations[0].ToolCallID != "call-2" {
		t.Errorf("expected ToolCallID call-2, got %q", violations[0].ToolCallID)
	}
}

func TestValidateToolThreading_OrphanResult(t *testing.T) {
	// Place the tool result BEFORE the assistant message so it's not absorbed
	// into the contiguous results block. The top-level orphan check catches it.
	messages := []Message{
		{Role: "user", Content: "Read a file"},
		// Stray tool result with no preceding assistant tool-call message
		{Role: "tool", Content: "stray content", ToolCallID: "call-unknown"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (orphan + missing), got %d: %v", len(violations), violations)
	}

	// Find the orphan violation
	var foundOrphan, foundMissing bool
	for _, v := range violations {
		if v.Kind == ToolThreadingViolationOrphanResult {
			foundOrphan = true
			if v.ToolCallID != "call-unknown" {
				t.Errorf("orphan ToolCallID = %q, want call-unknown", v.ToolCallID)
			}
			if v.Index != 1 {
				t.Errorf("orphan index = %d, want 1", v.Index)
			}
		}
		if v.Kind == ToolThreadingViolationMissingResult {
			foundMissing = true
			if v.ToolCallID != "call-1" {
				t.Errorf("missing ToolCallID = %q, want call-1", v.ToolCallID)
			}
		}
	}
	if !foundOrphan {
		t.Error("expected orphan_result violation")
	}
	if !foundMissing {
		t.Error("expected missing_result violation")
	}
}

func TestValidateToolThreading_GapBeforeResult(t *testing.T) {
	// Use 2 calls so one has expected position > 0 (the gap detection code
	// checks expectedIDs[id] != 0 to avoid false positives on the first call).
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		// User message inserted between assistant tool-call and its results
		{Role: "user", Content: "Also check this"},
		// Only call-2's result appears after the gap (position 1, so != 0)
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) == 0 {
		t.Fatalf("expected at least 1 violation, got 0")
	}

	var foundGap bool
	for _, v := range violations {
		if v.Kind == ToolThreadingViolationGapBeforeResult {
			foundGap = true
			if v.Index != 2 {
				t.Errorf("gap index = %d, want 2 (the user message)", v.Index)
			}
		}
	}
	if !foundGap {
		t.Errorf("expected gap_before_result violation, got kinds: %v", func() []string {
			var kinds []string
			for _, v := range violations {
				kinds = append(kinds, v.Kind)
			}
			return kinds
		}())
	}
}

func TestValidateToolThreading_MultipleAssistantBlocks(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Do something"},
		// First assistant turn
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		// Results in correct order
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		// Second assistant turn
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-3", Function: ToolCallFunction{Name: "search", Arguments: `{}`}},
			{ID: "call-4", Function: ToolCallFunction{Name: "search", Arguments: `{}`}},
		}},
		// Results reversed in second block
		{Role: "tool", Content: "search 4", ToolCallID: "call-4"},
		{Role: "tool", Content: "search 3", ToolCallID: "call-3"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (second block out of order), got %d: %v", len(violations), violations)
	}
	if violations[0].Kind != ToolThreadingViolationOutOfOrder {
		t.Errorf("expected out_of_order, got %q", violations[0].Kind)
	}
	// The violation index is assistantIdx + 1. Second assistant is at index 4,
	// so the violation index is 5 (first tool result position in second block).
	if violations[0].Index != 5 {
		t.Errorf("expected index 5 (start of second block's results), got %d", violations[0].Index)
	}
}

func TestValidateToolThreading_CorrectMultiBlock(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Do something"},
		// First assistant turn
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		// Second assistant turn
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-3", Function: ToolCallFunction{Name: "search", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "search results", ToolCallID: "call-3"},
		// Final assistant response
		{Role: "assistant", Content: "Here are the results."},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_ToolResultBeforeAnyAssistant(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		// Tool result appears before any assistant tool-call message
		{Role: "tool", Content: "orphan content", ToolCallID: "call-noexist"},
		{Role: "user", Content: "Hello"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Kind != ToolThreadingViolationOrphanResult {
		t.Errorf("expected orphan_result, got %q", violations[0].Kind)
	}
	if violations[0].Index != 1 {
		t.Errorf("expected index 1, got %d", violations[0].Index)
	}
}

func TestValidateToolThreading_EmptyToolCallIDs(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "call-2"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (empty IDs should be skipped), got %d: %v", len(violations), violations)
	}
}

func TestValidateToolThreading_ThreeCallsReversed(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-3", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		// All three reversed
		{Role: "tool", Content: "c", ToolCallID: "call-3"},
		{Role: "tool", Content: "b", ToolCallID: "call-2"},
		{Role: "tool", Content: "a", ToolCallID: "call-1"},
	}

	violations := ValidateToolThreading(messages)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Kind != ToolThreadingViolationOutOfOrder {
		t.Errorf("expected out_of_order, got %q", violations[0].Kind)
	}
}

func TestValidateToolThreading_NoMutatesInput(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Read files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
	}

	// Save original order
	origIDs := []string{messages[2].ToolCallID, messages[3].ToolCallID}

	_ = ValidateToolThreading(messages)

	// Verify input was not mutated
	if messages[2].ToolCallID != origIDs[0] || messages[3].ToolCallID != origIDs[1] {
		t.Errorf("ValidateToolThreading mutated input messages")
	}
}

// --- sortViolations ---

func TestSortViolations_StableSort(t *testing.T) {
	v := []ToolThreadingViolation{
		{Kind: "missing_result", Index: 5, ToolCallID: "call-2", Detail: "d2"},
		{Kind: "orphan_result", Index: 2, ToolCallID: "call-99", Detail: "d1"},
		{Kind: "out_of_order", Index: 2, Detail: "d3"},
	}

	sortViolations(v)

	// Index 2 comes first, sorted by kind within same index
	if v[0].Index != 2 || v[0].Kind != "orphan_result" {
		t.Errorf("expected first: index=2, kind=orphan_result, got index=%d, kind=%s", v[0].Index, v[0].Kind)
	}
	if v[1].Index != 2 || v[1].Kind != "out_of_order" {
		t.Errorf("expected second: index=2, kind=out_of_order, got index=%d, kind=%s", v[1].Index, v[1].Kind)
	}
	if v[2].Index != 5 || v[2].Kind != "missing_result" {
		t.Errorf("expected third: index=5, kind=missing_result, got index=%d, kind=%s", v[2].Index, v[2].Kind)
	}
}

func TestSortViolations_AlreadySorted(t *testing.T) {
	v := []ToolThreadingViolation{
		{Kind: "orphan_result", Index: 1, Detail: "a"},
		{Kind: "missing_result", Index: 3, Detail: "b"},
	}

	sortViolations(v)

	if v[0].Index != 1 || v[1].Index != 3 {
		t.Errorf("sort changed already-sorted slice")
	}
}

func TestSortViolations_Empty(t *testing.T) {
	var v []ToolThreadingViolation
	sortViolations(v)
	if v != nil {
		t.Errorf("sortViolations(nil) returned non-nil")
	}
}

func TestSortViolations_SingleElement(t *testing.T) {
	v := []ToolThreadingViolation{{Kind: "orphan_result", Index: 0, Detail: "x"}}
	sortViolations(v)
	if len(v) != 1 || v[0].Kind != "orphan_result" {
		t.Errorf("sortViolations changed single element")
	}
}
