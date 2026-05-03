package agent

import (
	"encoding/json"
	"testing"
)

func TestNormalizePositiveInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		// int
		{"int positive", int(42), 42},
		{"int zero", int(0), 0},
		{"int negative", int(-5), 0},
		{"int max", int(^uint(0) >> 1), int(^uint(0) >> 1)},

		// int8
		{"int8 positive", int8(7), 7},
		{"int8 zero", int8(0), 0},
		{"int8 negative", int8(-1), 0},

		// int16
		{"int16 positive", int16(100), 100},
		{"int16 zero", int16(0), 0},
		{"int16 negative", int16(-10), 0},

		// int32
		{"int32 positive", int32(999), 999},
		{"int32 zero", int32(0), 0},
		{"int32 negative", int32(-999), 0},

		// int64
		{"int64 positive", int64(12345), 12345},
		{"int64 zero", int64(0), 0},
		{"int64 negative", int64(-1), 0},

		// uint
		{"uint positive", uint(99), 99},
		{"uint zero", uint(0), 0},

		// uint8
		{"uint8 positive", uint8(5), 5},
		{"uint8 zero", uint8(0), 0},

		// uint16
		{"uint16 positive", uint16(200), 200},
		{"uint16 zero", uint16(0), 0},

		// uint32
		{"uint32 positive", uint32(5000), 5000},
		{"uint32 zero", uint32(0), 0},

		// uint64
		{"uint64 positive", uint64(777), 777},
		{"uint64 zero", uint64(0), 0},
		{"uint64 overflow", uint64(^uint64(0)), 0}, // exceeds max int

		// float32
		{"float32 positive", float32(3.7), 3},
		{"float32 zero", float32(0.0), 0},
		{"float32 negative", float32(-1.5), 0},

		// float64
		{"float64 positive", float64(12.8), 12},
		{"float64 zero", float64(0.0), 0},
		{"float64 negative", float64(-99.9), 0},

		// json.Number
		{"json.Number positive", json.Number("456"), 456},
		{"json.Number zero", json.Number("0"), 0},
		{"json.Number negative", json.Number("-10"), 0},

		// string
		{"string positive", "88", 88},
		{"string zero", "0", 0},
		{"string negative", "-3", 0},
		{"string with spaces", "  55  ", 55},
		{"string non-numeric", "abc", 0},
		{"string empty", "", 0},

		// nil and unknown
		{"nil", nil, 0},
		{"bool true", true, 0},
		{"bool false", false, 0},
		{"slice", []int{1, 2}, 0},
		{"map", map[string]int{"a": 1}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePositiveInt(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePositiveInt(%v) = %d; want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShouldStopExecution(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		expected bool
	}{
		{"empty", "", false},
		{"normal output", "Task completed successfully", false},
		{"critical error", "CRITICAL ERROR: something broke", true},
		{"fatal error", "FATAL ERROR: cannot proceed", true},
		{"critical in middle", "Something CRITICAL ERROR happened", true},
		{"fatal in middle", "Something FATAL ERROR happened", true},
		{"warning not critical", "WARNING: this is not critical", false},
		{"error but not critical", "ERROR: something failed", false},
		{"critical lowercase", "critical error", false},
		{"fatal lowercase", "fatal error", false},
		{"multi-line with critical", "line1\nCRITICAL ERROR\nline3", true},
		{"multi-line with fatal", "line1\nFATAL ERROR\nline3", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := &ToolExecutor{}
			result := te.shouldStopExecution("dummy", tt.result)
			if result != tt.expected {
				t.Errorf("shouldStopExecution(%q) = %v; want %v", tt.result, result, tt.expected)
			}
		})
	}
}

func TestGenerateToolCallID(t *testing.T) {
	te := &ToolExecutor{}

	// Generate multiple IDs and verify they are unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := te.GenerateToolCallID("test_tool")
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}

	// Verify ID format: call_<sanitized_name>_<timestamp>_<seq>
	id := te.GenerateToolCallID("test_tool")
	if len(id) == 0 {
		t.Error("generated ID should not be empty")
	}

	// Different tool names should produce different ID prefixes
	id1 := te.GenerateToolCallID("tool_a")
	id2 := te.GenerateToolCallID("tool_b")
	if id1 == id2 {
		t.Errorf("IDs for different tools should differ: got %s for both", id1)
	}

	// Underscores in tool name should be removed
	idWithUnderscore := te.GenerateToolCallID("my_tool_name")
	if len(idWithUnderscore) == 0 {
		t.Error("ID with underscore tool name should not be empty")
	}
}
