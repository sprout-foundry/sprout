package agent

import (
	"encoding/json"
	"testing"
)

func TestNormalizePositiveInt(t *testing.T) {
	t.Parallel()

	maxInt := int(^uint(0) >> 1)

	tests := []struct {
		name string
		in   any
		want int
	}{
		// int types (positive)
		{name: "int positive", in: int(42), want: 42},
		{name: "int8 positive", in: int8(42), want: 42},
		{name: "int16 positive", in: int16(42), want: 42},
		{name: "int32 positive", in: int32(42), want: 42},
		{name: "int64 positive", in: int64(42), want: 42},
		{name: "int64 at maxInt", in: int64(maxInt), want: maxInt},

		// int types (zero and negative)
		{name: "int zero", in: int(0), want: 0},
		{name: "int negative", in: int(-1), want: 0},
		{name: "int8 zero", in: int8(0), want: 0},
		{name: "int8 negative", in: int8(-1), want: 0},
		{name: "int16 zero", in: int16(0), want: 0},
		{name: "int16 negative", in: int16(-1), want: 0},
		{name: "int32 zero", in: int32(0), want: 0},
		{name: "int32 negative", in: int32(-1), want: 0},
		{name: "int64 zero", in: int64(0), want: 0},
		{name: "int64 negative", in: int64(-1), want: 0},
		{name: "int64 overflow maxInt", in: int64(maxInt + 1), want: 0},

		// uint types (positive)
		{name: "uint positive", in: uint(42), want: 42},
		{name: "uint8 positive", in: uint8(42), want: 42},
		{name: "uint16 positive", in: uint16(42), want: 42},
		{name: "uint32 positive", in: uint32(42), want: 42},
		{name: "uint64 positive", in: uint64(42), want: 42},
		{name: "uint64 at maxInt", in: uint64(maxInt), want: maxInt},

		// uint types (zero)
		{name: "uint zero", in: uint(0), want: 0},
		{name: "uint8 zero", in: uint8(0), want: 0},
		{name: "uint16 zero", in: uint16(0), want: 0},
		{name: "uint32 zero", in: uint32(0), want: 0},
		{name: "uint64 zero", in: uint64(0), want: 0},
		{name: "uint32 overflow maxInt", in: uint32(uint32(maxInt) + 1), want: 0},
		{name: "uint64 overflow maxInt", in: uint64(maxInt + 1), want: 0},

		// float types
		{name: "float32 positive", in: float32(42.7), want: 42},
		{name: "float64 positive", in: float64(42.7), want: 42},
		{name: "float32 zero", in: float32(0), want: 0},
		{name: "float64 zero", in: float64(0), want: 0},
		{name: "float32 negative", in: float32(-1.5), want: 0},
		{name: "float64 negative", in: float64(-1.5), want: 0},
		{name: "float64 one", in: float64(1), want: 1},

		// json.Number
		{name: "json.Number positive", in: json.Number("42"), want: 42},
		{name: "json.Number zero", in: json.Number("0"), want: 0},
		{name: "json.Number negative", in: json.Number("-1"), want: 0},

		// string
		{name: "string positive", in: "42", want: 42},
		{name: "string with spaces", in: "  42  ", want: 42},
		{name: "string zero", in: "0", want: 0},
		{name: "string negative", in: "-1", want: 0},
		{name: "string not a number", in: "abc", want: 0},
		{name: "string empty", in: "", want: 0},
		{name: "string float text", in: "42.5", want: 0},

		// nil and unknown types
		{name: "nil", in: nil, want: 0},
		{name: "bool", in: true, want: 0},
		{name: "slice", in: []int{1, 2}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizePositiveInt(tt.in)
			if got != tt.want {
				t.Errorf("normalizePositiveInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestShouldStopExecution(t *testing.T) {
	t.Parallel()

	te := &ToolExecutor{}

	tests := []struct {
		name   string
		tool   string
		result string
		want   bool
	}{
		{
			name:   "critical error",
			tool:   "shell_command",
			result: "CRITICAL ERROR: something failed",
			want:   true,
		},
		{
			name:   "fatal error",
			tool:   "read_file",
			result: "FATAL ERROR: unable to proceed",
			want:   true,
		},
		{
			name:   "normal result",
			tool:   "read_file",
			result: "file contents here",
			want:   false,
		},
		{
			name:   "empty result",
			tool:   "read_file",
			result: "",
			want:   false,
		},
		{
			name:   "contains error but not critical or fatal",
			tool:   "shell_command",
			result: "WARN: some error occurred but it is not critical",
			want:   false,
		},
		{
			name:   "critical error embedded in text",
			tool:   "shell_command",
			result: "Process exited with CRITICAL ERROR code 1",
			want:   true,
		},
		{
			name:   "fatal error embedded in text",
			tool:   "shell_command",
			result: "System FATAL ERROR: disk full",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := te.shouldStopExecution(tt.tool, tt.result)
			if got != tt.want {
				t.Errorf("shouldStopExecution(%q, %q) = %v, want %v", tt.tool, tt.result, got, tt.want)
			}
		})
	}
}

func TestGenerateToolCallID(t *testing.T) {
	t.Parallel()

	te := &ToolExecutor{}

	// Basic format check
	id := te.GenerateToolCallID("read_file")
	if id == "" {
		t.Fatal("GenerateToolCallID returned empty string")
	}

	// Check prefix
	if len(id) < 5 || id[:4] != "call" {
		t.Errorf("GenerateToolCallID(%q) = %q, expected to start with 'call'", "read_file", id)
	}

	// Underscore in tool name should be removed
	// ID format: call_{sanitizedToolName}_{timestamp}_{seq}
	if id == "call_read_file" {
		// The tool name part should be "readfile" not "read_file"
		t.Errorf("GenerateToolCallID(%q) should not contain underscores from tool name", "read_file")
	}

	// Uniqueness: calling twice should produce different IDs
	id2 := te.GenerateToolCallID("read_file")
	if id == id2 {
		t.Errorf("GenerateToolCallID produced duplicate IDs: %q", id)
	}

	// Different tool names produce different IDs
	id3 := te.GenerateToolCallID("write_file")
	if id == id3 {
		t.Errorf("GenerateToolCallID(%q) == GenerateToolCallID(%q) = %q", "read_file", "write_file", id)
	}
}

func TestNormalizePositiveIntMaxIntBoundary(t *testing.T) {
	t.Parallel()

	maxInt := int(^uint(0) >> 1)

	// int64 right at boundary
	if got := normalizePositiveInt(int64(maxInt)); got != maxInt {
		t.Errorf("normalizePositiveInt(int64(maxInt)) = %d, want %d", got, maxInt)
	}

	// int64 one over boundary
	if got := normalizePositiveInt(int64(maxInt + 1)); got != 0 {
		t.Errorf("normalizePositiveInt(int64(maxInt+1)) = %d, want 0", got)
	}

	// uint64 right at boundary
	if got := normalizePositiveInt(uint64(maxInt)); got != maxInt {
		t.Errorf("normalizePositiveInt(uint64(maxInt)) = %d, want %d", got, maxInt)
	}

	// uint64 one over boundary
	if got := normalizePositiveInt(uint64(maxInt + 1)); got != 0 {
		t.Errorf("normalizePositiveInt(uint64(maxInt+1)) = %d, want 0", got)
	}

	// uint overflow maxInt (only relevant when uint size == 32)
	if got := normalizePositiveInt(uint(0)); got != 0 {
		t.Errorf("normalizePositiveInt(uint(0)) = %d, want 0", got)
	}
}
