package agent

import (
	"strings"
	"testing"
	"time"
)

func TestGetToolTimeoutSubagent(t *testing.T) {
	timeout := getToolTimeout("run_subagent")
	if timeout != 30*time.Minute {
		t.Errorf("getToolTimeout(run_subagent) = %v, want %v", timeout, 30*time.Minute)
	}

	timeout = getToolTimeout("run_parallel_subagents")
	if timeout != 30*time.Minute {
		t.Errorf("getToolTimeout(run_parallel_subagents) = %v, want %v", timeout, 30*time.Minute)
	}
}

func TestGetToolTimeoutShellCommand(t *testing.T) {
	timeout := getToolTimeout("shell_command")
	if timeout != 2*time.Minute {
		t.Errorf("getToolTimeout(shell_command) = %v, want %v", timeout, 2*time.Minute)
	}
}

func TestGetToolTimeoutOther(t *testing.T) {
	timeout := getToolTimeout("read_file")
	if timeout != 5*time.Minute {
		t.Errorf("getToolTimeout(read_file) = %v, want %v", timeout, 5*time.Minute)
	}

	timeout = getToolTimeout("unknown_tool")
	if timeout != 5*time.Minute {
		t.Errorf("getToolTimeout(unknown_tool) = %v, want %v", timeout, 5*time.Minute)
	}
}

func TestGetToolTimeoutEnvOverride(t *testing.T) {
	t.Setenv("SPROUT_TOOL_TIMEOUT", "60")
	timeout := getToolTimeout("read_file")
	if timeout != 60*time.Second {
		t.Errorf("getToolTimeout with TOOL_TIMEOUT=60 = %v, want %v", timeout, 60*time.Second)
	}
	// Override applies regardless of tool name
	timeout = getToolTimeout("shell_command")
	if timeout != 60*time.Second {
		t.Errorf("getToolTimeout(shell_command) with TOOL_TIMEOUT=60 = %v, want %v", timeout, 60*time.Second)
	}
}

func TestGetToolTimeoutEnvOverrideInvalid(t *testing.T) {
	t.Setenv("SPROUT_TOOL_TIMEOUT", "not-a-number")
	// Invalid value falls through to defaults
	timeout := getToolTimeout("read_file")
	if timeout != 5*time.Minute {
		t.Errorf("getToolTimeout with invalid TOOL_TIMEOUT = %v, want %v (default)", timeout, 5*time.Minute)
	}
}

func TestIsSubagentTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"run_subagent", true},
		{"run_parallel_subagents", true},
		{"shell_command", false},
		{"read_file", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSubagentTool(tt.name); got != tt.want {
				t.Errorf("isSubagentTool(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestTruncateToolResultUnderLimit(t *testing.T) {
	input := "short result"
	got := truncateToolResult(input)
	if got != input {
		t.Errorf("truncateToolResult(short) = %q, want %q (unchanged)", got, input)
	}
}

func TestTruncateToolResultExactAtLimit(t *testing.T) {
	// Exactly defaultToolResultMaxChars (50000) — implementation uses <= so should NOT truncate
	input := strings.Repeat("X", defaultToolResultMaxChars)
	got := truncateToolResult(input)
	if got != input {
		t.Errorf("truncateToolResult(at-limit %d chars) should return unchanged", defaultToolResultMaxChars)
	}
}

func TestTruncateToolResultOverLimit(t *testing.T) {
	// Build a string that exceeds defaultToolResultMaxChars (50000)
	headPart := strings.Repeat("H", 45000)
	tailPart := strings.Repeat("T", 5000)
	input := headPart + tailPart // 50000 total — exactly at the limit, so should NOT truncate

	// Make it one char over to force truncation
	input = headPart + "X" + tailPart // 50001 chars

	got := truncateToolResult(input)

	// Should contain the truncation notice
	if !strings.Contains(got, "[... truncated:") {
		t.Fatal("truncateToolResult did not include truncation notice for over-limit input")
	}

	// Should contain "chars omitted"
	if !strings.Contains(got, "chars omitted") {
		t.Fatal("truncateToolResult truncation notice missing 'chars omitted'")
	}

	// Should contain "Total was"
	if !strings.Contains(got, "Total was") {
		t.Fatal("truncateToolResult truncation notice missing 'Total was'")
	}

	// Should start with the head chars
	if !strings.HasPrefix(got, headPart) {
		t.Fatal("truncateToolResult should preserve head content")
	}

	// Should end with the tail chars
	if !strings.HasSuffix(got, tailPart) {
		t.Fatal("truncateToolResult should preserve tail content")
	}
}
