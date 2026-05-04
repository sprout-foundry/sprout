package agent

// zero_coverage_test.go covers previously untested pure functions across the agent package.
// Tests use table-driven patterns with t.Parallel() for safe concurrent execution.
// Note: test names in this file do not use the "_ZC" suffix used in other packages' zero coverage files.

import (
	"slices"
	"strings"
	"testing"
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
			name: "BUG_wrote_file_not_captured_starts_with_W",
			stdout: `Wrote pkg/agent/output.txt`,
			checkValue: func(t *testing.T, summary map[string]string) {
				// TODO: fix extractSubagentSummary to handle "Wrote" lines (case 'W' not in switch).
				// "Wrote" starts with 'W', but the source only enters the case for 'C'/'c' first
				// chars — so this line is NOT captured. This test documents the buggy behavior.
				if _, ok := summary["files"]; ok {
					t.Errorf("expected NO 'files' key (Wrote starts with W, not C/c), got: %q", summary["files"])
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
