package core

import (
	"testing"
	"time"
)

// Must-fix #3: normalize deduplicates by name+args (not just name)
func TestVerify_NamePlusArgsDedupe(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks := []rawBlock{
		{
			start: 0, end: 10,
			parsed: []ToolCall{
				{ID: "1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "a"}`}},
				{ID: "2", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "b"}`}},
			},
		},
	}
	tcs := fp.normalize(blocks)
	if len(tcs) != 2 {
		t.Errorf("expected 2 tool calls (different args), got %d", len(tcs))
	}
	names := make(map[string]int)
	for _, tc := range tcs {
		names[tc.Function.Name]++
	}
	if names["search"] != 2 {
		t.Errorf("expected search called 2 times, got %d", names["search"])
	}
}

// Must-fix #4: normalize generates synthetic IDs
func TestVerify_SyntheticID(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks := []rawBlock{
		{
			start: 0, end: 10,
			parsed: []ToolCall{
				{ID: "", Type: "function", Function: ToolCallFunction{Name: "calc", Arguments: "{}"}},
			},
		},
	}
	tcs := fp.normalize(blocks)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	id := tcs[0].ID
	if id == "" {
		t.Error("expected non-empty synthetic ID")
	}
	if len(id) < 20 {
		t.Errorf("expected synthetic ID to be long enough, got length %d", len(id))
	}
	if len(id) >= 9 && id[:9] != "fallback_" {
		t.Errorf("expected ID to start with 'fallback_', got %q", id[:9])
	}
}

// Must-fix #4: Tool calls with existing IDs should keep them
func TestVerify_PreservesExistingID(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks := []rawBlock{
		{
			start: 0, end: 10,
			parsed: []ToolCall{
				{ID: "existing_id_123", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
			},
		},
	}
	tcs := fp.normalize(blocks)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if tcs[0].ID != "existing_id_123" {
		t.Errorf("expected ID to be preserved, got %q", tcs[0].ID)
	}
}

// Must-fix #2: dedupeBlocks no longer duplicates blocks
func TestVerify_NoDuplicateBlocks(t *testing.T) {
	blocks := []rawBlock{
		{parsed: []ToolCall{
			{Function: ToolCallFunction{Name: "a", Arguments: "{}"}},
			{Function: ToolCallFunction{Name: "b", Arguments: "{}"}},
		}},
		{parsed: []ToolCall{
			{Function: ToolCallFunction{Name: "a", Arguments: "{}"}},
		}},
	}
	result := dedupeBlocks(blocks)
	if len(result) != 1 {
		t.Errorf("expected 1 block after dedupe, got %d", len(result))
	}
	if len(result[0].parsed) != 2 {
		t.Errorf("expected 1st block to have 2 tool calls, got %d", len(result[0].parsed))
	}
}

// Must-fix #5: Pattern markers don't cause false positives
func TestVerify_NoFalsePositives(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	falsePositiveCases := []string{
		"Please enter your name",
		"The user input is required",
		"username and input field",
		"The name of the file is test.txt",
		"Input validation failed",
	}
	for _, content := range falsePositiveCases {
		if fp.containsToolCallPatterns(content) {
			t.Errorf("false positive: %q should not match tool call patterns", content)
		}
	}
}

// Verify: "arguments" marker still catches bare JSON
func TestVerify_ArgumentsMarker(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := `[{"id": "1", "type": "function", "function": {"name": "bare", "arguments": "{}"}}]`
	if !fp.containsToolCallPatterns(content) {
		t.Errorf("expected %q to match (contains 'arguments')", content)
	}
}

// Verify: normalize with knownToolNames still works
func TestVerify_KnownToolNamesWithDedupe(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(s string) bool { return s == "allowed" || s == "allowed2" },
	})
	blocks := []rawBlock{
		{
			start: 0, end: 10,
			parsed: []ToolCall{
				{Type: "function", Function: ToolCallFunction{Name: "allowed", Arguments: `{"x": 1}`}},
				{Type: "function", Function: ToolCallFunction{Name: "denied", Arguments: "{}"}},
				{Type: "function", Function: ToolCallFunction{Name: "allowed2", Arguments: `{"y": 2}`}},
			},
		},
	}
	tcs := fp.normalize(blocks)
	if len(tcs) != 2 {
		t.Errorf("expected 2 tool calls, got %d: %v", len(tcs), tcs)
	}
}

// Verify: normalize deduplicates by name+args (same name, same args = deduped)
func TestVerify_SameNameSameArgsDedup(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks := []rawBlock{
		{
			start: 0, end: 10,
			parsed: []ToolCall{
				{Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "hello"}`}},
				{Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "hello"}`}},
			},
		},
	}
	tcs := fp.normalize(blocks)
	if len(tcs) != 1 {
		t.Errorf("expected 1 tool call (deduped by name+args), got %d", len(tcs))
	}
}

// Verify: mergeAndDedupe works without overlap field
func TestVerify_NoOverlapField(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks := []rawBlock{
		{start: 0, end: 5, parsed: []ToolCall{{Function: ToolCallFunction{Name: "a", Arguments: "{}"}}}},
		{start: 3, end: 8, parsed: []ToolCall{{Function: ToolCallFunction{Name: "b", Arguments: "{}"}}}},
		{start: 10, end: 15, parsed: []ToolCall{{Function: ToolCallFunction{Name: "c", Arguments: "{}"}}}},
	}
	result := fp.mergeAndDedupe(blocks)
	if len(result) != 2 {
		t.Errorf("expected 2 merged blocks, got %d", len(result))
	}
}

// Verify: synthetic IDs are unique across calls
func TestVerify_UniqueSyntheticIDs(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	blocks1 := []rawBlock{{
		parsed: []ToolCall{{ID: "", Type: "function", Function: ToolCallFunction{Name: "test", Arguments: "{}"}}},
	}}
	blocks2 := []rawBlock{{
		parsed: []ToolCall{{ID: "", Type: "function", Function: ToolCallFunction{Name: "test", Arguments: "{}"}}},
	}}
	time.Sleep(1 * time.Millisecond)
	tcs1 := fp.normalize(blocks1)
	tcs2 := fp.normalize(blocks2)
	if tcs1[0].ID == tcs2[0].ID {
		t.Error("expected different synthetic IDs for separate calls")
	}
}

// Verify: cleanContent works after sort change
func TestVerify_CleanContentAfterSortChange(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := "start ```json\n{\"tool_calls\": [{\"id\": \"1\", \"type\": \"function\", \"function\": {\"name\": \"search\", \"arguments\": \"{}\"}}]}\n``` end"
	result := fp.Parse(content)
	if len(result.ToolCalls) == 0 {
		t.Fatal("expected tool calls")
	}
	if result.CleanedContent == content {
		t.Error("expected cleaned content to differ from original")
	}
}
