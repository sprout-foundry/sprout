package agent

import (
	"encoding/json"
	"sync"
	"testing"
)

// --- normalizePositiveInt ---

func TestNormalizePositiveInt_AllIntTypes(t *testing.T) {
	tests := []struct {
		input any
		want  int
	}{
		{int(5), 5},
		{int(0), 0},
		{int(-3), 0},
		{int8(7), 7},
		{int8(0), 0},
		{int8(-1), 0},
		{int16(100), 100},
		{int16(0), 0},
		{int16(-1), 0},
		{int32(999), 999},
		{int32(0), 0},
		{int32(-1), 0},
		{int64(12345), 12345},
		{int64(0), 0},
		{int64(-1), 0},
	}
	for _, tc := range tests {
		got := normalizePositiveInt(tc.input)
		if got != tc.want {
			t.Errorf("normalizePositiveInt(%v(%T)) = %d, want %d", tc.input, tc.input, got, tc.want)
		}
	}
}

func TestNormalizePositiveInt_AllUintTypes(t *testing.T) {
	tests := []struct {
		input any
		want  int
	}{
		{uint(5), 5},
		{uint(0), 0},
		{uint8(7), 7},
		{uint8(0), 0},
		{uint16(100), 100},
		{uint16(0), 0},
		{uint32(999), 999},
		{uint32(0), 0},
		{uint64(12345), 12345},
		{uint64(0), 0},
	}
	for _, tc := range tests {
		got := normalizePositiveInt(tc.input)
		if got != tc.want {
			t.Errorf("normalizePositiveInt(%v(%T)) = %d, want %d", tc.input, tc.input, got, tc.want)
		}
	}
}

func TestNormalizePositiveInt_FloatTypes(t *testing.T) {
	tests := []struct {
		input any
		want  int
	}{
		{float32(3.7), 3},
		{float32(0.0), 0},
		{float32(-1.5), 0},
		{float64(9.9), 9},
		{float64(0.0), 0},
		{float64(-5.2), 0},
	}
	for _, tc := range tests {
		got := normalizePositiveInt(tc.input)
		if got != tc.want {
			t.Errorf("normalizePositiveInt(%v(%T)) = %d, want %d", tc.input, tc.input, got, tc.want)
		}
	}
}

func TestNormalizePositiveInt_JSONNumberAndString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{"json_number_42", json.Number("42"), 42},
		{"json_number_0", json.Number("0"), 0},
		{"json_number_neg", json.Number("-5"), 0},
		{"json_number_invalid", json.Number("invalid"), 0},
		{"string_42", "42", 42},
		{"string_padded", "  7  ", 7},
		{"string_0", "0", 0},
		{"string_neg", "-3", 0},
		{"string_abc", "abc", 0},
		{"string_empty", "", 0},
		{"string_float", "3.14", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePositiveInt(tc.input)
			if got != tc.want {
				t.Errorf("normalizePositiveInt(%v) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizePositiveInt_UnknownAndNilTypes(t *testing.T) {
	if normalizePositiveInt([]int{1, 2, 3}) != 0 {
		t.Error("slice type should return 0")
	}
	if normalizePositiveInt(nil) != 0 {
		t.Error("nil should return 0")
	}
}

func TestNormalizePositiveInt_OverflowGuard(t *testing.T) {
	maxInt := int64(int(^uint(0) >> 1))
	if normalizePositiveInt(maxInt) != int(maxInt) {
		t.Errorf("maxInt should be accepted")
	}
	if normalizePositiveInt(maxInt+1) != 0 {
		t.Errorf("maxInt+1 should be rejected as overflow")
	}
}

// --- shouldStopExecution ---

func TestShouldStopExecution_CriticalAndFatal(t *testing.T) {
	te := &ToolExecutor{agent: nil}

	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"empty", "", false},
		{"normal output", "Task completed successfully", false},
		{"critical error", "CRITICAL ERROR: something went wrong", true},
		{"fatal error", "FATAL ERROR: unrecoverable", true},
		{"critical in middle", "Something CRITICAL ERROR happened", true},
		{"fatal in middle", "Something FATAL ERROR happened", true},
		{"warning not critical", "WARNING: this is not critical", false},
		{"error but not critical", "ERROR: something failed", false},
		{"critical lowercase", "critical error", false},
		{"fatal lowercase", "fatal error", false},
		{"multi-line with critical", "line1\nCRITICAL ERROR\nline3", true},
		{"multi-line with fatal", "line1\nFATAL ERROR\nline3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := te.shouldStopExecution("test", tc.result)
			if got != tc.want {
				t.Errorf("shouldStopExecution(%q) = %v, want %v", tc.result, got, tc.want)
			}
		})
	}
}

// --- GenerateToolCallID ---

func TestGenerateToolCallID_ContainsToolName(t *testing.T) {
	te := &ToolExecutor{agent: nil}
	id := te.GenerateToolCallID("shell_command")
	if len(id) < 10 {
		t.Errorf("ID too short: %q", id)
	}
}

func TestGenerateToolCallID_Uniqueness(t *testing.T) {
	te := &ToolExecutor{agent: nil}
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := te.GenerateToolCallID("tool")
		if seen[id] {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestGenerateToolCallID_ConcurrentUniqueness(t *testing.T) {
	te := &ToolExecutor{agent: nil}
	results := make(chan string, 200)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- te.GenerateToolCallID("tool")
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]bool)
	for id := range results {
		if seen[id] {
			t.Fatalf("duplicate ID in concurrent generation: %q", id)
		}
		seen[id] = true
	}
}

// --- tryExecuteMCPTool ---

func TestTryExecuteMCPTool_NilAgent(t *testing.T) {
	te := &ToolExecutor{agent: nil}
	result, err, handled := te.tryExecuteMCPTool("mcp_test", nil)
	if !handled {
		t.Error("should be handled with nil agent")
	}
	if err == nil {
		t.Error("expected error with nil agent")
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestTryExecuteMCPTool_NonMCPTool(t *testing.T) {
	te := &ToolExecutor{agent: &Agent{}}
	result, err, handled := te.tryExecuteMCPTool("shell_command", nil)
	if handled {
		t.Error("non-MCP tool should not be handled")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestTryExecuteMCPTool_MCPPrefixWithoutManager(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	te := &ToolExecutor{agent: a}
	_, err, handled := te.tryExecuteMCPTool("mcp_test", map[string]interface{}{})
	if !handled {
		t.Error("MCP-prefixed tool should be handled")
	}
	if err == nil {
		t.Error("expected error with no MCP manager")
	}
}
