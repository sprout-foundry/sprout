package agent

import "testing"

func TestFormatTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: `""`,
		},
		{
			name:     "short string",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "exactly 50 chars",
			input:    "12345678901234567890123456789012345678901234567890",
			expected: `"12345678901234567890123456789012345678901234567890"`,
		},
		{
			name:     "51 chars - truncated",
			input:    "123456789012345678901234567890123456789012345678901",
			expected: `"12345678901234567890123456789012345678901234567..."`,
		},
		{
			name:     "very long string",
			input:    "This is a very long string that should be truncated because it exceeds the maximum display length",
			expected: `"This is a very long string that should be trunc..."`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTruncateString(tt.input)
			if got != tt.expected {
				t.Errorf("formatTruncateString(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSummarizeTodoWriteArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "nil args",
			args:     nil,
			expected: "",
		},
		{
			name:     "empty args",
			args:     map[string]interface{}{},
			expected: "",
		},
		{
			name: "no todos key",
			args: map[string]interface{}{
				"other": "value",
			},
			expected: "",
		},
		{
			name: "todos key not slice",
			args: map[string]interface{}{
				"todos": "not a slice",
			},
			expected: "",
		},
		{
			name: "empty todos slice",
			args: map[string]interface{}{
				"todos": []interface{}{},
			},
			expected: "",
		},
		{
			name: "mixed statuses",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{"status": "pending"},
					map[string]interface{}{"status": "in_progress"},
					map[string]interface{}{"status": "completed"},
					map[string]interface{}{"status": "cancelled"},
				},
			},
			expected: "todos=4 pending=1 progress=1 done=1 cancelled=1",
		},
		{
			name: "all pending",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{"status": "pending"},
					map[string]interface{}{"status": "pending"},
				},
			},
			expected: "todos=2 pending=2 progress=0 done=0 cancelled=0",
		},
		{
			name: "item not a map",
			args: map[string]interface{}{
				"todos": []interface{}{
					"not a map",
					map[string]interface{}{"status": "pending"},
				},
			},
			expected: "todos=2 pending=1 progress=0 done=0 cancelled=0",
		},
		{
			name: "unknown status ignored",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{"status": "unknown"},
					map[string]interface{}{"status": "pending"},
				},
			},
			expected: "todos=2 pending=1 progress=0 done=0 cancelled=0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeTodoWriteArgs(tt.args)
			if got != tt.expected {
				t.Errorf("summarizeTodoWriteArgs() = %q, want %q", got, tt.expected)
			}
		})
	}
}
