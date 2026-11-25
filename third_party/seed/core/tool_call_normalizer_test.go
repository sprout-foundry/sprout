package core

import (
	"strings"
	"testing"
)

func TestNewToolCallNormalizer(t *testing.T) {
	n := NewToolCallNormalizer()
	if n == nil {
		t.Fatal("expected non-nil normalizer")
	}
}

// ---------------------------------------------------------------------------
// Normalize — empty / nil inputs
// ---------------------------------------------------------------------------

func TestNormalizeNilInput(t *testing.T) {
	n := NewToolCallNormalizer()
	result := n.Normalize(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
}

func TestNormalizeEmptyInput(t *testing.T) {
	n := NewToolCallNormalizer()
	result := n.Normalize([]ToolCall{})
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Channel suffix stripping
// ---------------------------------------------------------------------------

func TestNormalizeStripsChannelSuffix(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file<|channel|>0",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result))
	}
	if result[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", result[0].Function.Name)
	}
}

func TestNormalizeStripsVariousChannelSuffixes(t *testing.T) {
	n := NewToolCallNormalizer()
	tests := []struct {
		input    string
		expected string
	}{
		{"read_file<|channel|>0", "read_file"},
		{"read_file<|channel|>12", "read_file"},
		{"read_file<|channel|>", "read_file"},
		{"read_file", "read_file"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			calls := []ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: ToolCallFunction{
						Name:      tt.input,
						Arguments: `{}`,
					},
				},
			}
			result := n.Normalize(calls)
			if len(result) == 0 && tt.expected != "" {
				t.Fatalf("expected 1 call, got 0 (empty name dropped)")
			}
			if len(result) > 0 && result[0].Function.Name != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result[0].Function.Name)
			}
		})
	}
}

func TestNormalizeDropsChannelSuffixOnlyNames(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "<|channel|>0",
				Arguments: `{}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result != nil {
		t.Fatalf("expected nil (name became empty after strip), got %v", result)
	}
}

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

func TestNormalizeGeneratesMissingID(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].ID == "" {
		t.Error("expected non-empty ID for call with missing ID")
	}
	if !strings.HasPrefix(result[0].ID, "call_read_file_") {
		t.Errorf("expected ID prefix 'call_read_file_', got %q", result[0].ID)
	}
}

func TestNormalizePreservesExistingID(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "my_custom_id_123",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].ID != "my_custom_id_123" {
		t.Errorf("expected preserved ID, got %q", result[0].ID)
	}
}

func TestNormalizeGeneratesUniqueIDs(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/a.txt"}`,
			},
		},
		{
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/b.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].ID == result[1].ID {
		t.Errorf("expected unique IDs, got %q for both", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Type normalization
// ---------------------------------------------------------------------------

func TestNormalizeForcesTypeToFunction(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "tool",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{}`,
			},
		},
		{
			ID:   "call_2",
			Type: "code",
			Function: ToolCallFunction{
				Name:      "shell_command",
				Arguments: `{}`,
			},
		},
	}
	result := n.Normalize(calls)
	for i, tc := range result {
		if tc.Type != "function" {
			t.Errorf("call %d: expected Type 'function', got %q", i, tc.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON argument repair
// ---------------------------------------------------------------------------

func TestNormalizeRepairsTrailingComma(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt",}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].Function.Arguments != `{"path":"/tmp/test.txt"}` {
		t.Errorf("expected repaired JSON, got %q", result[0].Function.Arguments)
	}
}

func TestNormalizeRepairsTrailingCommaInArray(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "multi_read",
				Arguments: `["/tmp/a.txt", "/tmp/b.txt",]`,
			},
		},
	}
	result := n.Normalize(calls)
	expected := `["/tmp/a.txt","/tmp/b.txt"]`
	if result[0].Function.Arguments != expected {
		t.Errorf("expected %q, got %q", expected, result[0].Function.Arguments)
	}
}

func TestNormalizeRepairsBareKeyValues(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `"path": "/tmp/test.txt"`,
			},
		},
	}
	result := n.Normalize(calls)
	expected := `{"path":"/tmp/test.txt"}`
	if result[0].Function.Arguments != expected {
		t.Errorf("expected %q, got %q", expected, result[0].Function.Arguments)
	}
}

func TestNormalizeCanonicalizesJSON(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `  {  "path"  :  "/tmp/test.txt"  }  `,
			},
		},
	}
	result := n.Normalize(calls)
	expected := `{"path":"/tmp/test.txt"}`
	if result[0].Function.Arguments != expected {
		t.Errorf("expected %q, got %q", expected, result[0].Function.Arguments)
	}
}

func TestNormalizeEmptyArgumentsBecomesEmptyObject(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: "",
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].Function.Arguments != "{}" {
		t.Errorf("expected '{}', got %q", result[0].Function.Arguments)
	}
}

func TestNormalizeWhitespaceArgumentsBecomesEmptyObject(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: "   ",
			},
		},
	}
	result := n.Normalize(calls)
	if result[0].Function.Arguments != "{}" {
		t.Errorf("expected '{}', got %q", result[0].Function.Arguments)
	}
}

func TestNormalizeDropsUnrepairableJSON(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `not json at all`,
			},
		},
		{
			ID:   "call_2",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "shell_command",
				Arguments: `{"command": "ls"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 call (unrepairable dropped), got %d", len(result))
	}
	if result[0].Function.Name != "shell_command" {
		t.Errorf("expected 'shell_command', got %q", result[0].Function.Name)
	}
}

// ---------------------------------------------------------------------------
// Deduplication
// ---------------------------------------------------------------------------

func TestNormalizeDeduplicatesSameIDAndArgs(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 call after dedup, got %d", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("expected first occurrence (call_1), got %q", result[0].ID)
	}
}

func TestNormalizeDeduplicatesThreeOrMore(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/a"}`,
			},
		},
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/a"}`,
			},
		},
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/a"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 after dedup of 3 duplicates, got %d", len(result))
	}
}

func TestNormalizeDoesNotDeduplicateDifferentID(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "id_a",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
		{
			ID:   "id_b",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	// Different IDs → not deduplicated (dedup is by ID+args per spec)
	if len(result) != 2 {
		t.Fatalf("expected 2 calls (different IDs), got %d", len(result))
	}
}

func TestNormalizeDoesNotDeduplicateDifferentArgs(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/a.txt"}`,
			},
		},
		{
			ID:   "call_2",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/b.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2 calls (different args), got %d", len(result))
	}
}

func TestNormalizeDoesNotDeduplicateDifferentNames(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{}`,
			},
		},
		{
			ID:   "call_2",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "write_file",
				Arguments: `{}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2 calls (different names), got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Empty name filtering
// ---------------------------------------------------------------------------

func TestNormalizeDropsEmptyNames(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
		{
			ID:   "call_2",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 1 {
		t.Fatalf("expected 1 call (empty name dropped), got %d", len(result))
	}
	if result[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", result[0].Function.Name)
	}
}

func TestNormalizeDropsAllIfAllEmptyNames(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "",
				Arguments: `{}`,
			},
		},
	}
	result := n.Normalize(calls)
	if result != nil {
		t.Fatalf("expected nil (all dropped), got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Combined normalization
// ---------------------------------------------------------------------------

func TestNormalizeAllStepsCombined(t *testing.T) {
	n := NewToolCallNormalizer()
	calls := []ToolCall{
		{
			// No ID, channel suffix, non-standard type, trailing comma in JSON
			ID:   "",
			Type: "tool",
			Function: ToolCallFunction{
				Name:      "read_file<|channel|>0",
				Arguments: `{"path": "/tmp/test.txt",}`,
			},
		},
		{
			// Same tool, different args — gets unique ID
			ID:   "",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file<|channel|>0",
				Arguments: `{"path": "/tmp/other.txt"}`,
			},
		},
		{
			// Different tool
			ID:   "call_3",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "shell_command",
				Arguments: `{"command": "ls"}`,
			},
		},
	}
	result := n.Normalize(calls)
	if len(result) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(result))
	}

	// First call: ID generated, name stripped, type normalized, JSON repaired
	if result[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", result[0].Function.Name)
	}
	if result[0].Type != "function" {
		t.Errorf("expected Type 'function', got %q", result[0].Type)
	}
	if result[0].ID == "" {
		t.Error("expected generated ID")
	}
	if result[0].Function.Arguments != `{"path":"/tmp/test.txt"}` {
		t.Errorf("expected repaired JSON, got %q", result[0].Function.Arguments)
	}

	// Second call: also normalized, unique ID
	if result[1].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", result[1].Function.Name)
	}
	if result[1].ID == "" {
		t.Error("expected generated ID for second call")
	}
	if result[0].ID == result[1].ID {
		t.Error("expected unique IDs for different calls")
	}

	// Third call preserved
	if result[2].Function.Name != "shell_command" {
		t.Errorf("expected 'shell_command', got %q", result[2].Function.Name)
	}
}
