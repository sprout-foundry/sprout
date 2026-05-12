package agent

// zero_coverage_test.go covers previously untested pure functions across the agent package.
// Tests use table-driven patterns with t.Parallel() for safe concurrent execution.
// Note: test names in this file do not use the "_ZC" suffix used in other packages' zero coverage files.

import (
	"slices"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// formatRiskType (tool_definitions.go)
// ---------------------------------------------------------------------------

func TestFormatRiskType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mass_deletion",
			input:    "mass_deletion",
			expected: "Mass deletion — may delete all files in current directory or home",
		},
		{
			name:     "source_code_destruction",
			input:    "source_code_destruction",
			expected: "Source code destruction — may delete project source files",
		},
		{
			name:     "privilege_escalation",
			input:    "privilege_escalation",
			expected: "Privilege escalation — running with elevated permissions",
		},
		{
			name:     "remote_code_execution",
			input:    "remote_code_execution",
			expected: "Remote code execution — downloading and executing untrusted code",
		},
		{
			name:     "arbitrary_code_execution",
			input:    "arbitrary_code_execution",
			expected: "Arbitrary code execution — executing arbitrary shell commands",
		},
		{
			name:     "destructive_git_operation",
			input:    "destructive_git_operation",
			expected: "Destructive git operation — may rewrite published history",
		},
		{
			name:     "disk_destruction",
			input:    "disk_destruction",
			expected: "Disk destruction — may destroy disk data or partition tables",
		},
		{
			name:     "critical_system_operation",
			input:    "critical_system_operation",
			expected: "Critical system operation — may cause irreversible system damage",
		},
		{
			name:     "system_instability",
			input:    "system_instability",
			expected: "System instability — may crash the system or kill all processes",
		},
		{
			name:     "insecure_permissions",
			input:    "insecure_permissions",
			expected: "Insecure permissions — setting overly permissive file access",
		},
		{
			name:     "system_integrity",
			input:    "system_integrity",
			expected: "System integrity — writing to critical system files",
		},
		{
			name:     "empty_string_returns_empty",
			input:    "",
			expected: "",
		},
		{
			name:     "unknown_type_passthrough",
			input:    "some_unknown_type",
			expected: "some_unknown_type",
		},
		{
			name:     "mixed_case_passthrough",
			input:    "MASS_DELETION",
			expected: "MASS_DELETION",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatRiskType(tc.input)
			if got != tc.expected {
				t.Errorf("formatRiskType(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getMapKeys (tool_definitions.go)
// ---------------------------------------------------------------------------

func TestGetMapKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected []string // sorted
	}{
		{
			name:     "nil_map_returns_empty_slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "empty_map_returns_empty_slice",
			input:    map[string]interface{}{},
			expected: []string{},
		},
		{
			name:     "single_key",
			input:    map[string]interface{}{"a": 1},
			expected: []string{"a"},
		},
		{
			name:     "multiple_keys",
			input:    map[string]interface{}{"b": 2, "a": 1, "c": 3},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "keys_with_empty_string",
			input:    map[string]interface{}{"": nil, "foo": "bar"},
			expected: []string{"", "foo"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := getMapKeys(tc.input)
			slices.Sort(got)
			if len(got) != len(tc.expected) {
				t.Fatalf("len(getMapKeys(...)) = %d; want %d", len(got), len(tc.expected))
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("getMapKeys()[%d] = %q; want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isNumberValue (tool_handlers_structured.go)
// ---------------------------------------------------------------------------

func TestIsNumberValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		// Numeric types — true
		{name: "float64_zero", input: float64(0), expected: true},
		{name: "float64_positive", input: float64(3.14), expected: true},
		{name: "float64_negative", input: float64(-2.5), expected: true},
		{name: "float32_zero", input: float32(0), expected: true},
		{name: "float32_value", input: float32(1.5), expected: true},
		{name: "int_zero", input: int(0), expected: true},
		{name: "int_positive", input: int(42), expected: true},
		{name: "int_negative", input: int(-10), expected: true},
		{name: "int8_value", input: int8(127), expected: true},
		{name: "int16_value", input: int16(32767), expected: true},
		{name: "int32_value", input: int32(2147483647), expected: true},
		{name: "int64_value", input: int64(-9223372036854775808), expected: true},
		{name: "uint_zero", input: uint(0), expected: true},
		{name: "uint_value", input: uint(100), expected: true},
		{name: "uint8_value", input: uint8(255), expected: true},
		{name: "uint16_value", input: uint16(65535), expected: true},
		{name: "uint32_value", input: uint32(4294967295), expected: true},
		{name: "uint64_value", input: uint64(18446744073709551615), expected: true},
		// Non-numeric types — false
		{name: "string", input: "123", expected: false},
		{name: "bool_true", input: true, expected: false},
		{name: "bool_false", input: false, expected: false},
		{name: "nil_interface", input: nil, expected: false},
		{name: "slice", input: []int{1, 2}, expected: false},
		{name: "map", input: map[string]int{"a": 1}, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isNumberValue(tc.input)
			if got != tc.expected {
				t.Errorf("isNumberValue(%v) = %v; want %v", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isIntegerValue (tool_handlers_structured.go)
// ---------------------------------------------------------------------------

func TestIsIntegerValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		// Integer types — true
		{name: "int_zero", input: int(0), expected: true},
		{name: "int_positive", input: int(42), expected: true},
		{name: "int_negative", input: int(-10), expected: true},
		{name: "int8_value", input: int8(127), expected: true},
		{name: "int16_value", input: int16(-1), expected: true},
		{name: "int32_value", input: int32(2147483647), expected: true},
		{name: "int64_value", input: int64(-9223372036854775808), expected: true},
		{name: "uint_value", input: uint(100), expected: true},
		{name: "uint8_value", input: uint8(0), expected: true},
		{name: "uint16_value", input: uint16(65535), expected: true},
		{name: "uint32_value", input: uint32(1), expected: true},
		{name: "uint64_value", input: uint64(18446744073709551615), expected: true},
		// float64 with integer value — true
		{name: "float64_integer_value", input: float64(42.0), expected: true},
		{name: "float64_zero", input: float64(0.0), expected: true},
		{name: "float64_negative_integer", input: float64(-5.0), expected: true},
		// float64 with fractional value — false
		{name: "float64_fractional", input: float64(3.14), expected: false},
		{name: "float64_fractional_zero_decimal", input: float64(0.1), expected: false},
		// float32 (not matched by isIntegerValue) — false
		{name: "float32_integer_value", input: float32(42), expected: false},
		// Non-numeric types — false
		{name: "string", input: "42", expected: false},
		{name: "bool", input: true, expected: false},
		{name: "nil_interface", input: nil, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isIntegerValue(tc.input)
			if got != tc.expected {
				t.Errorf("isIntegerValue(%v) = %v; want %v", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toSchemaMap (tool_handlers_structured.go)
// ---------------------------------------------------------------------------

func TestToSchemaMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    interface{}
		wantErr  bool
		wantKeys []string // sorted keys expected when no error
	}{
		{
			name:     "valid_map",
			input:    map[string]interface{}{"type": "object", "required": []interface{}{"name"}},
			wantErr:  false,
			wantKeys: []string{"required", "type"},
		},
		{
			name:     "empty_map",
			input:    map[string]interface{}{},
			wantErr:  false,
			wantKeys: []string{},
		},
		{
			name:    "nil_returns_error",
			input:   nil,
			wantErr: true,
		},
		{
			name:    "string_returns_error",
			input:   "not a map",
			wantErr: true,
		},
		{
			name:    "int_returns_error",
			input:   42,
			wantErr: true,
		},
		{
			name:    "bool_returns_error",
			input:   true,
			wantErr: true,
		},
		{
			name:    "slice_returns_error",
			input:   []interface{}{"a", "b"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := toSchemaMap(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantKeys != nil {
				keys := getMapKeys(got)
				slices.Sort(keys)
				if len(keys) != len(tc.wantKeys) {
					t.Fatalf("got %d keys; want %d", len(keys), len(tc.wantKeys))
				}
				for i := range keys {
					if keys[i] != tc.wantKeys[i] {
						t.Errorf("key[%d] = %q; want %q", i, keys[i], tc.wantKeys[i])
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// stripAnsiCodes (tool_handlers_subagent.go)
// ---------------------------------------------------------------------------

func TestStripAnsiCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no_codes_returns_unchanged",
			input:    "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "single_color_code",
			input:    "\033[31mRed\033[0m",
			expected: "Red",
		},
		{
			name:     "multiple_color_codes",
			input:    "\033[31mRed\033[32mGreen\033[33mYellow\033[0m",
			expected: "RedGreenYellow",
		},
		{
			name:     "bold_code",
			input:    "\033[1mBold\033[0m",
			expected: "Bold",
		},
		{
			name:     "multi_param_code",
			input:    "\033[38;5;244mGray\033[0m",
			expected: "Gray",
		},
		{
			name:     "cursor_movement",
			input:    "\033[2J\033[H",
			expected: "",
		},
		{
			name:     "mixed_content",
			input:    "\033[31mError:\033[0m Something went wrong\n\033[32mOK\033[0m",
			expected: "Error: Something went wrong\nOK",
		},
		{
			name:     "only_ansi_codes",
			input:    "\033[0m\033[1m\033[31m",
			expected: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripAnsiCodes(tc.input)
			if got != tc.expected {
				t.Errorf("stripAnsiCodes(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractSubagentSummary (tool_handlers_subagent.go)
// ---------------------------------------------------------------------------

func TestExtractSubagentSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		stdout     string
		expectKeys []string // sorted keys we expect in the map
		expectNot  []string // keys we must NOT be present
		checkValue func(t *testing.T, summary map[string]string)
	}{
		{
			name:       "empty_stdout",
			stdout:     "",
			expectKeys: nil,
		},
		{
			name: "file_creation",
			stdout: `Created: pkg/agent/new_file.go
Created: pkg/agent/another.go`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["files"]
				if !ok {
					t.Fatal("expected 'files' key in summary")
				}
				if !strings.Contains(v, "Created:") {
					t.Errorf("files value should contain 'Created:', got: %s", v)
				}
			},
		},
		{
			name: "file_modification",
			stdout: `Modified: pkg/agent/existing.go`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["files"]
				if !ok {
					t.Fatal("expected 'files' key in summary")
				}
				if !strings.Contains(v, "Modified:") {
					t.Errorf("files value should contain 'Modified:', got: %s", v)
				}
			},
		},
		{
			name: "file_deletion",
			stdout: `Deleted: pkg/agent/old.go`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["files"]
				if !ok {
					t.Fatal("expected 'files' key in summary")
				}
				if !strings.Contains(v, "Deleted:") {
					t.Errorf("files value should contain 'Deleted:', got: %s", v)
				}
			},
		},
		{
			name: "file_update",
			stdout: `Updated: pkg/agent/config.go`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["files"]
				if !ok {
					t.Fatal("expected 'files' key in summary")
				}
				if !strings.Contains(v, "Updated:") {
					t.Errorf("files value should contain 'Updated:', got: %s", v)
				}
			},
		},
		{
			name:  "wrote_file_not_captured_starts_with_W_uppercase",
			stdout: `Wrote pkg/agent/output.txt`,
			checkValue: func(t *testing.T, summary map[string]string) {
				// "Wrote" starts with uppercase 'W'. The fast-path switch only enters
				// the 'C'/'c' branch, so uppercase "Wrote" falls through all cases and
				// is NOT captured as a file change. This test documents the known gap.
				_, ok := summary["files"]
				if ok {
					t.Errorf("expected NO 'files' key for \"Wrote\" (starts with W, no matching case), got values")
				}
			},
		},
		{
			name: "build_passed",
			stdout: `Build: [OK] Passed`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["build_status"]
				if !ok {
					t.Fatal("expected 'build_status' key in summary")
				}
				if v != "passed" {
					t.Errorf("build_status = %q; want 'passed'", v)
				}
			},
		},
		{
			name: "build_failed",
			stdout: `Build: [FAIL] Failed`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["build_status"]
				if !ok {
					t.Fatal("expected 'build_status' key in summary")
				}
				if v != "failed" {
					t.Errorf("build_status = %q; want 'failed'", v)
				}
			},
		},
		{
			name: "test_passed_with_counts",
			stdout: `Test: [OK] Passed 5 passed 0 failed`,
			checkValue: func(t *testing.T, summary map[string]string) {
				if summary["test_status"] != "passed" {
					t.Errorf("test_status = %q; want 'passed'", summary["test_status"])
				}
				if summary["test_counts"] != "5 passed, 0 failed" {
					t.Errorf("test_counts = %q; want '5 passed, 0 failed'", summary["test_counts"])
				}
			},
		},
		{
			name: "test_failed_with_counts",
			stdout: `Tests: [FAIL] Failed 3 passed 2 failed`,
			checkValue: func(t *testing.T, summary map[string]string) {
				if summary["test_status"] != "failed" {
					t.Errorf("test_status = %q; want 'failed'", summary["test_status"])
				}
				if summary["test_counts"] != "3 passed, 2 failed" {
					t.Errorf("test_counts = %q; want '3 passed, 2 failed'", summary["test_counts"])
				}
			},
		},
		{
			name: "error_extraction",
			stdout: `Error: something went wrong
error: another error`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["errors"]
				if !ok {
					t.Fatal("expected 'errors' key in summary")
				}
				if !strings.Contains(v, "Error:") {
					t.Errorf("errors should contain 'Error:', got: %s", v)
				}
			},
		},
		{
			name: "shell_command_extraction",
			stdout: `$ go build ./...
$ go test ./...`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["commands"]
				if !ok {
					t.Fatal("expected 'commands' key in summary")
				}
				if !strings.Contains(v, "go build") {
					t.Errorf("commands should contain 'go build', got: %s", v)
				}
			},
		},
		{
			name: "todo_list_extraction",
			stdout: `Added 3 todos to TodoWrite`,
			checkValue: func(t *testing.T, summary map[string]string) {
				v, ok := summary["todos"]
				if !ok {
					t.Fatal("expected 'todos' key in summary")
				}
				if v != "Added 3 todos" {
					t.Errorf("todos = %q; want 'Added 3 todos'", v)
				}
			},
		},
		{
			name: "metrics_extraction",
			stdout: `SUBAGENT_METRICS: total_tokens=5000 prompt_tokens=3000 completion_tokens=2000 total_cost=0.15 cached_tokens=1000`,
			checkValue: func(t *testing.T, summary map[string]string) {
				if summary["subagent_total_tokens"] != "5000" {
					t.Errorf("subagent_total_tokens = %q; want '5000'", summary["subagent_total_tokens"])
				}
				if summary["subagent_prompt_tokens"] != "3000" {
					t.Errorf("subagent_prompt_tokens = %q; want '3000'", summary["subagent_prompt_tokens"])
				}
				if summary["subagent_completion_tokens"] != "2000" {
					t.Errorf("subagent_completion_tokens = %q; want '2000'", summary["subagent_completion_tokens"])
				}
				if summary["subagent_total_cost"] != "0.15" {
					t.Errorf("subagent_total_cost = %q; want '0.15'", summary["subagent_total_cost"])
				}
				if summary["subagent_cached_tokens"] != "1000" {
					t.Errorf("subagent_cached_tokens = %q; want '1000'", summary["subagent_cached_tokens"])
				}
			},
		},
		{
			name:       "no_relevant_content",
			stdout:     "Just some random output line",
			expectNot:  []string{"files", "build_status", "test_status", "errors", "commands", "todos"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary := extractSubagentSummary(tc.stdout)
			if tc.checkValue != nil {
				tc.checkValue(t, summary)
			}
			for _, key := range tc.expectNot {
				if _, ok := summary[key]; ok {
					t.Errorf("expected key %q to NOT be present, but it was: %q", key, summary[key])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extensionFromContentType (resource_capture.go)
// ---------------------------------------------------------------------------

func TestExtensionFromContentType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "pdf",
			input:    "application/pdf",
			expected: ".pdf",
		},
		{
			name:     "png",
			input:    "image/png",
			expected: ".png",
		},
		{
			name:     "jpeg",
			input:    "image/jpeg",
			expected: ".jpg",
		},
		{
			name:     "jpg",
			input:    "image/jpg",
			expected: ".jpg",
		},
		{
			name:     "webp",
			input:    "image/webp",
			expected: ".webp",
		},
		{
			name:     "gif",
			input:    "image/gif",
			expected: ".gif",
		},
		{
			name:     "text_plain",
			input:    "text/plain",
			expected: ".txt",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "unknown_type",
			input:    "application/octet-stream",
			expected: "",
		},
		{
			name:     "uppercase",
			input:    "IMAGE/PNG",
			expected: ".png",
		},
		{
			name:     "with_whitespace",
			input:    "  image/png  ",
			expected: ".png",
		},
		{
			name:     "text_plain_with_charset",
			input:    "text/plain; charset=utf-8",
			expected: ".txt",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extensionFromContentType(tc.input)
			if got != tc.expected {
				t.Errorf("extensionFromContentType(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// min (persistence.go)
// ---------------------------------------------------------------------------

func TestMin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{name: "first_smaller", a: 1, b: 5, expected: 1},
		{name: "second_smaller", a: 10, b: 3, expected: 3},
		{name: "equal_values", a: 7, b: 7, expected: 7},
		{name: "zero_and_positive", a: 0, b: 1, expected: 0},
		{name: "zero_and_zero", a: 0, b: 0, expected: 0},
		{name: "both_negative", a: -5, b: -10, expected: -10},
		{name: "negative_and_positive", a: -1, b: 1, expected: -1},
		{name: "large_values", a: 1000000, b: 500000, expected: 500000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := min(tc.a, tc.b)
			if got != tc.expected {
				t.Errorf("min(%d, %d) = %d; want %d", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateDeepSeekToolCalls (conversation_messaging.go)
// ---------------------------------------------------------------------------

func TestValidateDeepSeekToolCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupMessages func() []api.Message
	}{
		{
			name:         "empty messages",
			setupMessages: func() []api.Message { return []api.Message{} },
		},
		{
			name: "assistant with tool calls followed by matching tool results",
			setupMessages: func() []api.Message {
				tc := api.ToolCall{ID: "call_1", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search"}}
				return []api.Message{
					{Role: "user", Content: "search for something"},
					{Role: "assistant", ToolCalls: []api.ToolCall{tc}},
					{Role: "tool", ToolCallID: "call_1", Content: "search results"},
					{Role: "assistant", Content: "response"},
				}
			},
		},
		{
			name: "assistant with multiple tool calls",
			setupMessages: func() []api.Message {
				tc1 := api.ToolCall{ID: "call_1", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search"}}
				tc2 := api.ToolCall{ID: "call_2", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "read_file"}}
				return []api.Message{
					{Role: "user", Content: "do two things"},
					{Role: "assistant", ToolCalls: []api.ToolCall{tc1, tc2}},
					{Role: "tool", ToolCallID: "call_1", Content: "results 1"},
					{Role: "tool", ToolCallID: "call_2", Content: "results 2"},
				}
			},
		},
		{
			name: "assistant with tool calls missing some tool results",
			setupMessages: func() []api.Message {
				tc1 := api.ToolCall{ID: "call_1", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search"}}
				tc2 := api.ToolCall{ID: "call_2", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "read_file"}}
				return []api.Message{
					{Role: "user", Content: "search"},
					{Role: "assistant", ToolCalls: []api.ToolCall{tc1, tc2}},
					{Role: "tool", ToolCallID: "call_1", Content: "results 1"},
				}
			},
		},
		{
			name: "assistant without tool calls",
			setupMessages: func() []api.Message {
				return []api.Message{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "hi there"},
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConversationHandler{agent: &Agent{}}
			handler.validateDeepSeekToolCalls(tc.setupMessages())
		})
	}
}

// ---------------------------------------------------------------------------
// validateMinimaxToolCalls (conversation_messaging.go)
// ---------------------------------------------------------------------------

func TestValidateMinimaxToolCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupMessages func() []api.Message
	}{
		{
			name:         "empty messages",
			setupMessages: func() []api.Message { return []api.Message{} },
		},
		{
			name: "assistant with tool calls followed by matching tool results",
			setupMessages: func() []api.Message {
				tc := api.ToolCall{ID: "call_123", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search"}}
				return []api.Message{
					{Role: "user", Content: "search"},
					{Role: "assistant", ToolCalls: []api.ToolCall{tc}},
					{Role: "tool", ToolCallID: "call_123", Content: "results"},
				}
			},
		},
		{
			name: "assistant with multiple tool calls and matching results",
			setupMessages: func() []api.Message {
				tc1 := api.ToolCall{ID: "call_a", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search"}}
				tc2 := api.ToolCall{ID: "call_b", Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "read_file"}}
				return []api.Message{
					{Role: "user", Content: "do things"},
					{Role: "assistant", ToolCalls: []api.ToolCall{tc1, tc2}},
					{Role: "tool", ToolCallID: "call_a", Content: "results A"},
					{Role: "tool", ToolCallID: "call_b", Content: "results B"},
				}
			},
		},
		{
			name: "orphaned tool result before any assistant",
			setupMessages: func() []api.Message {
				return []api.Message{
					{Role: "tool", ToolCallID: "orphan_call", Content: "orphan result"},
					{Role: "user", Content: "search"},
				}
			},
		},
		{
			name: "assistant without tool calls",
			setupMessages: func() []api.Message {
				return []api.Message{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "hi there"},
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConversationHandler{agent: &Agent{}}
			handler.validateMinimaxToolCalls(tc.setupMessages())
		})
	}
}

// ---------------------------------------------------------------------------
// appendTransientMessages (conversation_messaging.go)
// ---------------------------------------------------------------------------

func TestAppendTransientMessages(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name          string
		messages      []api.Message
		transientMsgs []api.Message
		wantCount     int
	}{
		{
			name:          "no transient messages",
			messages:      []api.Message{{Role: "user", Content: "hello"}},
			transientMsgs: []api.Message{},
			wantCount:     1,
		},
		{
			name:          "one transient message",
			messages:      []api.Message{{Role: "user", Content: "hello"}},
			transientMsgs: []api.Message{{Role: "system", Content: "system supplement"}},
			wantCount:     2,
		},
		{
			name: "multiple transient messages",
			messages: []api.Message{{Role: "user", Content: "hello"}},
			transientMsgs: []api.Message{
				{Role: "system", Content: "system supplement"},
				{Role: "user", Content: "additional context"},
			},
			wantCount: 3,
		},
		{
			name:          "empty base messages",
			messages:      []api.Message{},
			transientMsgs: []api.Message{{Role: "system", Content: "system"}},
			wantCount:     1,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConversationHandler{}
			handler.transientMessages = tt.transientMsgs
			
			got := handler.appendTransientMessages(tt.messages)
			
			if len(got) != tt.wantCount {
				t.Errorf("appendTransientMessages() returned %d messages, want %d", len(got), tt.wantCount)
			}
			
			// Verify transient messages are appended
			if len(tt.transientMsgs) > 0 {
				startTransient := len(tt.messages)
				if startTransient+len(tt.transientMsgs) > len(got) {
					t.Fatalf("unexpected message count")
				}
				
				for i, tm := range tt.transientMsgs {
					idx := startTransient + i
					if got[idx].Role != tm.Role || got[idx].Content != tm.Content {
						t.Errorf("transient message %d mismatch", i)
					}
				}
			}
			
			// Verify transient buffer is cleared (set to nil)
			if len(tt.transientMsgs) > 0 && handler.transientMessages != nil {
				t.Errorf("transient messages buffer not cleared after append")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// estimateTokens (conversation_messaging.go)
// ---------------------------------------------------------------------------

func TestEstimateTokensCoverage(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name     string
		messages []api.Message
		min      int
		max      int
	}{
		{
			name:     "empty messages",
			messages: []api.Message{},
			min:      0,
			max:      10,
		},
		{
			name: "simple user message",
			messages: []api.Message{
				{Role: "user", Content: "hello world"},
			},
			min: 10,
			max: 50,
		},
		{
			name: "multiple messages",
			messages: []api.Message{
				{Role: "user", Content: "first message"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "second message"},
			},
			min: 30,
			max: 100,
		},
		{
			name: "message with tool calls",
			messages: []api.Message{
				{
					Role: "assistant",
					ToolCalls: []api.ToolCall{
						{Function: struct{ Name string `json:"name"`; Arguments string `json:"arguments"` }{Name: "search", Arguments: `{"query":"test"}`}},
					},
				},
			},
			min: 20,
			max: 100,
		},
		{
			name: "message with reasoning content",
			messages: []api.Message{
				{
					Role:             "assistant",
					Content:          "final answer",
					ReasoningContent: "thinking about answer",
				},
			},
			min: 15,
			max: 80,
		},
		{
			name: "long content message",
			messages: []api.Message{
				{
					Role:    "user",
					Content: "This is a much longer message that should generate more tokens " +
						"when estimated. The token estimation is approximate and uses " +
						"a simple character-based heuristic that divides by 4 to get rough tokens.",
				},
			},
			min: 50,
			max: 150,
		},
		{
			name: "messages with images (no token impact)",
			messages: []api.Message{
				{
					Role:    "user",
					Content: "test",
					Images:  []api.ImageData{{Type: "image/png", Base64: "abc"}},
				},
			},
			min: 10,
			max: 50,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConversationHandler{}
			got := handler.estimateTokens(tt.messages)
			
			if got < tt.min {
				t.Errorf("estimateTokens() = %d below minimum %d", got, tt.min)
			}
			if got > tt.max {
				t.Errorf("estimateTokens() = %d above maximum %d", got, tt.max)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractShellCommand (conversation_optimizer.go)
// ---------------------------------------------------------------------------

func TestExtractShellCommand(t *testing.T) {
	t.Parallel()
	
	co := &ConversationOptimizer{}
	
	tests := []struct {
		name    string
		content string
		wantCmd string
	}{
		{
			name:    "simple command",
			content: "Tool call result for shell_command: ls -la",
			wantCmd: "ls -la",
		},
		{
			name:    "command with arguments",
			content: "Tool call result for shell_command: git status --short",
			wantCmd: "git status --short",
		},
		{
			name:    "command with pipes",
			content: "Tool call result for shell_command: cat file.txt | grep pattern",
			wantCmd: "cat file.txt | grep pattern",
		},
		{
			name:    "command with quotes",
			content: `Tool call result for shell_command: echo "hello world"`,
			wantCmd: `echo "hello world"`,
		},
		{
			name:    "command with newlines after colon",
			content: "Tool call result for shell_command:\nls -la\noutput",
			wantCmd: "ls -la",
		},
		{
			name:    "command with multiple spaces",
			content: "Tool call result for shell_command:  ls    -la   ",
			wantCmd: "ls    -la",
		},
		{
			name:    "complex shell command",
			content: `Tool call result for shell_command: find . -name "*.go" -type f | head -10`,
			wantCmd: `find . -name "*.go" -type f | head -10`,
		},
		{
			name:    "no command (missing prefix)",
			content: "ls -la",
			wantCmd: "",
		},
		{
			name:    "empty content",
			content: "",
			wantCmd: "",
		},
		{
			name:    "prefix with no command",
			content: "Tool call result for shell_command:",
			wantCmd: "",
		},
		{
			name:    "different tool result",
			content: "Tool call result for read_file: file.txt",
			wantCmd: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotCmd := co.extractShellCommand(tt.content)
			
			if gotCmd != tt.wantCmd {
				t.Errorf("extractShellCommand() = %q, want %q", gotCmd, tt.wantCmd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractFilePath (conversation_optimizer.go)
// ---------------------------------------------------------------------------

func TestExtractFilePathCoverage(t *testing.T) {
	t.Parallel()
	
	co := &ConversationOptimizer{}
	
	tests := []struct {
		name     string
		content  string
		wantPath string
	}{
		{
			name:     "simple file path",
			content:  "Tool call result for read_file: test.txt",
			wantPath: "test.txt",
		},
		{
			name:     "relative path with directory",
			content:  "Tool call result for read_file: src/main.go",
			wantPath: "src/main.go",
		},
		{
			name:     "absolute path",
			content:  "Tool call result for read_file: /home/user/project/file.go",
			wantPath: "/home/user/project/file.go",
		},
		{
			name:     "path stops at whitespace (actual behavior)",
			content:  "Tool call result for read_file: path/to/my file.txt",
			wantPath: "path/to/my",
		},
		{
			name:     "file extension",
			content:  "Tool call result for read_file: README.md",
			wantPath: "README.md",
		},
		{
			name:     "hidden file",
			content:  "Tool call result for read_file: .gitignore",
			wantPath: ".gitignore",
		},
		{
			name:     "no prefix - tool result",
			content:  "ls -la",
			wantPath: "",
		},
		{
			name:     "empty content",
			content:  "",
			wantPath: "",
		},
		{
			name:     "prefix with no path",
			content:  "Tool call result for read_file:",
			wantPath: "",
		},
		{
			name:     "different tool",
			content:  "Tool call result for write_file: test.txt",
			wantPath: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotPath := co.extractFilePath(tt.content)
			
			if gotPath != tt.wantPath {
				t.Errorf("extractFilePath() = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeSummaryEntry (conversation_optimizer.go)
// ---------------------------------------------------------------------------

func TestNormalizeSummaryEntryCoverage(t *testing.T) {
	t.Parallel()
	
	co := &ConversationOptimizer{}
	
	tests := []struct {
		name  string
		entry string
		want  string
	}{
		{
			name:  "simple entry",
			entry: "User request: hello",
			want:  "User request: hello",
		},
		{
			name:  "entry with leading/trailing whitespace",
			entry: "  User request: hello  ",
			want:  "User request: hello",
		},
		{
			name:  "entry with internal multiple spaces",
			entry: "User  request:    hello",
			want:  "User request: hello",
		},
		{
			name:  "entry with tabs and newlines",
			entry: "\tUser\trequest:\thello\n",
			want:  "User request: hello",
		},
		{
			name:  "empty entry",
			entry: "",
			want:  "",
		},
		{
			name:  "whitespace only entry",
			entry: "   \t\n  ",
			want:  "",
		},
		{
			name:  "entry with special characters",
			entry: "User request: test!@#$%^&*()",
			want:  "User request: test!@#$%^&*()",
		},
		{
			name:  "entry with unicode",
			entry: "User request: 你好世界",
			want:  "User request: 你好世界",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := co.normalizeSummaryEntry(tt.entry)
			if got != tt.want {
				t.Errorf("normalizeSummaryEntry() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// wrapCompactionSummaryWithLevel (conversation_optimizer.go)
// ---------------------------------------------------------------------------

func TestWrapCompactionSummaryWithLevelCoverage(t *testing.T) {
	t.Parallel()
	
	co := &ConversationOptimizer{}
	
	tests := []struct {
		name       string
		messages   []api.Message
		body       string
		context    compactionContext
		level      string
		wantHeader string
	}{
		{
			name:     "brief level",
			messages: []api.Message{{Role: "user", Content: "test"}},
			body:     "Summary text",
			context:  compactionContext{},
			level:    "brief",
			wantHeader: "Compacted earlier conversation state (brief):\n" +
				"- Summarized 1 earlier messages to preserve context headroom.\n",
		},
		{
			name:     "summary level",
			messages: []api.Message{{Role: "user", Content: "test"}, {Role: "assistant", Content: "response"}},
			body:     "Summary text",
			context:  compactionContext{},
			level:    "summary",
			wantHeader: "Compacted earlier conversation state (summary):\n" +
				"- Summarized 2 earlier messages to preserve context headroom.\n",
		},
		{
			name:     "detailed level",
			messages: []api.Message{{Role: "user", Content: "test"}},
			body:     "Detailed summary text",
			context:  compactionContext{},
			level:    "detailed",
			wantHeader: "Compacted earlier conversation state (detailed):\n" +
				"- Summarized 1 earlier messages to preserve context headroom.\n",
		},
		{
			name:     "default level (unknown)",
			messages: []api.Message{{Role: "user", Content: "test"}},
			body:     "Summary text",
			context:  compactionContext{},
			level:    "unknown",
			wantHeader: "Compacted earlier conversation state:\n" +
				"- Summarized 1 earlier messages to preserve context headroom.\n",
		},
		{
			name:     "empty body",
			messages: []api.Message{{Role: "user", Content: "test"}},
			body:     "",
			context:  compactionContext{},
			level:    "brief",
			wantHeader: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := co.wrapCompactionSummaryWithLevel(tt.messages, tt.body, tt.context, tt.level)
			
			if tt.wantHeader == "" {
				if got != "" {
					t.Errorf("wrapCompactionSummaryWithLevel() = %q, want empty string", got)
				}
				return
			}
			
			if !strings.Contains(got, tt.wantHeader) {
				t.Errorf("wrapCompactionSummaryWithLevel() does not contain expected header\n"+
					"got:\n%s\n\nwant header:\n%s", got, tt.wantHeader)
			}
			
			// Verify body is included
			if tt.body != "" && !strings.Contains(got, tt.body) {
				t.Errorf("wrapCompactionSummaryWithLevel() does not contain body")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// filterToolsByName (conversation.go)
// ---------------------------------------------------------------------------

func TestFilterToolsByNameCoverage(t *testing.T) {
	t.Parallel()
	
	makeTool := func(name string) api.Tool {
		return api.Tool{Type: "function", Function: struct{ Name string `json:"name"`; Description string `json:"description"`; Parameters interface{} `json:"parameters"` }{Name: name}}
	}
	
	tests := []struct {
		name    string
		tools   []api.Tool
		allowed map[string]struct{}
		want    []string
	}{
		{
			name:    "filters tools by allowed set",
			tools:   []api.Tool{makeTool("read_file"), makeTool("write_file"), makeTool("shell_command")},
			allowed: map[string]struct{}{"read_file": {}, "shell_command": {}},
			want:    []string{"read_file", "shell_command"},
		},
		{
			name:    "empty allowed set returns empty",
			tools:   []api.Tool{makeTool("read_file"), makeTool("write_file")},
			allowed: map[string]struct{}{},
			want:    []string{},
		},
		{
			name:    "all tools allowed",
			tools:   []api.Tool{makeTool("tool1"), makeTool("tool2"), makeTool("tool3")},
			allowed: map[string]struct{}{"tool1": {}, "tool2": {}, "tool3": {}},
			want:    []string{"tool1", "tool2", "tool3"},
		},
		{
			name:    "no tools",
			tools:   []api.Tool{},
			allowed: map[string]struct{}{"tool1": {}},
			want:    []string{},
		},
		{
			name:    "nil tools",
			tools:   nil,
			allowed: map[string]struct{}{"tool1": {}},
			want:    []string{},
		},
		{
			name:    "partial match preserves order",
			tools:   []api.Tool{makeTool("a"), makeTool("b"), makeTool("c"), makeTool("d")},
			allowed: map[string]struct{}{"b": {}, "d": {}},
			want:    []string{"b", "d"},
		},
		{
			name:    "case sensitive matching",
			tools:   []api.Tool{makeTool("Read_File"), makeTool("read_file")},
			allowed: map[string]struct{}{"read_file": {}},
			want:    []string{"read_file"},
		},
		{
			name:    "single tool",
			tools:   []api.Tool{makeTool("single")},
			allowed: map[string]struct{}{"single": {}},
			want:    []string{"single"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterToolsByName(tt.tools, tt.allowed)
			
			if len(got) != len(tt.want) {
				t.Errorf("filterToolsByName() returned %d tools, want %d", len(got), len(tt.want))
			}
			
			for i, wantName := range tt.want {
				if i >= len(got) {
					t.Errorf("missing tool %d: %s", i, wantName)
					continue
				}
				if got[i].Function.Name != wantName {
					t.Errorf("tool %d: got %q, want %q", i, got[i].Function.Name, wantName)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// makeAllowedToolSet (conversation.go)
// ---------------------------------------------------------------------------

func TestMakeAllowedToolSetFromZeroCoverage(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name      string
		toolNames []string
		wantSize  int
		contains  []string
		excludes  []string
	}{
		{
			name:      "creates map from tool names",
			toolNames: []string{"read_file", "write_file", "shell_command"},
			wantSize:  3,
			contains:  []string{"read_file", "write_file", "shell_command"},
		},
		{
			name:      "empty slice returns empty map",
			toolNames: []string{},
			wantSize:  0,
			contains:  []string{},
		},
		{
			name:      "nil slice returns empty map",
			toolNames: nil,
			wantSize:  0,
			contains:  []string{},
		},
		{
			name:      "filters empty strings",
			toolNames: []string{"read_file", "", "write_file", "  ", "\t"},
			wantSize:  2,
			contains:  []string{"read_file", "write_file"},
			excludes:  []string{"", "  ", "\t"},
		},
		{
			name:      "trims whitespace",
			toolNames: []string{"  read_file  ", "\twrite_file\t", " shell_command "},
			wantSize:  3,
			contains:  []string{"read_file", "write_file", "shell_command"},
		},
		{
			name:      "deduplicates",
			toolNames: []string{"read_file", "read_file", "write_file", "read_file"},
			wantSize:  2,
			contains:  []string{"read_file", "write_file"},
		},
		{
			name:      "single tool",
			toolNames: []string{"single_tool"},
			wantSize:  1,
			contains:  []string{"single_tool"},
		},
		{
			name:      "all empty strings",
			toolNames: []string{"", "  ", "\t", "\n"},
			wantSize:  0,
			contains:  []string{},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := makeAllowedToolSet(tt.toolNames)
			
			if len(got) != tt.wantSize {
				t.Errorf("makeAllowedToolSet() returned map size %d, want %d", len(got), tt.wantSize)
			}
			
			for _, name := range tt.contains {
				if _, ok := got[name]; !ok {
					t.Errorf("makeAllowedToolSet() map missing expected key %q", name)
				}
			}
			
			for _, name := range tt.excludes {
				if _, ok := got[name]; ok {
					t.Errorf("makeAllowedToolSet() map contains unexpected key %q", name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildNonVisionImageToolPrompt (conversation.go)
// ---------------------------------------------------------------------------

func TestBuildNonVisionImageToolPromptCoverage(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name           string
		query          string
		paths          []string
		wantContains   []string
	}{
		{
			name:  "simple prompt with image path",
			query: "analyze this image",
			paths: []string{"/tmp/image.png"},
			wantContains: []string{
				"OCR Trigger Policy (MANDATORY)",
				"active model is non-multimodal",
				"analyze_image_content",
				"analysis_mode=\"ocr\"",
				"/tmp/image.png",
				"Original user request:",
				"analyze this image",
			},
		},
		{
			name:  "multiple image paths",
			query: "look at these images",
			paths: []string{"/tmp/img1.png", "/tmp/img2.jpg"},
			wantContains: []string{
				"analyze_image_content",
				"/tmp/img1.png",
				"/tmp/img2.jpg",
			},
		},
		{
			name:  "empty query",
			query: "",
			paths: []string{"/tmp/image.png"},
			wantContains: []string{
				"OCR Trigger Policy",
				"/tmp/image.png",
			},
		},
		{
			name:  "empty paths",
			query: "do something",
			paths: []string{},
			wantContains: []string{
				"OCR Trigger Policy",
				"Pasted image paths:",
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{}
			got := a.buildNonVisionImageToolPrompt(tt.query, tt.paths)
			
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("buildNonVisionImageToolPrompt() does not contain %q\nGot:\n%s", want, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getCurrentCustomProvider (conversation.go)
// ---------------------------------------------------------------------------

func TestGetCurrentCustomProviderCoverage(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name          string
		configManager *configuration.Manager
		clientType    api.ClientType
		wantFound     bool
	}{
		{
			name:          "nil config manager",
			configManager: nil,
			clientType:    "openai",
			wantFound:     false,
		},
		{
			name:          "empty config manager returns false",
			configManager: &configuration.Manager{},
			clientType:    "openai",
			wantFound:     false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{configManager: tt.configManager, clientType: tt.clientType}
			got, found := a.getCurrentCustomProvider()
			
			if found != tt.wantFound {
				t.Errorf("getCurrentCustomProvider() found = %v, want %v", found, tt.wantFound)
			}
			
			if tt.wantFound && found && got == nil {
				t.Errorf("getCurrentCustomProvider() returned nil when expected to be found")
			}
		})
	}
}
