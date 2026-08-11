//go:build darwin && arm64 && cgo && mlx

package localmodel

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestParseLocalToolCalls_LFM2(t *testing.T) {
	text := `<|tool_call_start|>[read_file(path='/some/file.go')]<|tool_call_end|>`
	content, calls := parseLocalToolCalls("lfm2", text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("expected name 'read_file', got %q", calls[0].Function.Name)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestParseLocalToolCalls_LFM2_MultipleArgs(t *testing.T) {
	text := `<|tool_call_start|>[write_file(path='/tmp/test.go', content='hello world')]<|tool_call_end|>`
	_, calls := parseLocalToolCalls("lfm2", text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Errorf("expected 'write_file', got %q", calls[0].Function.Name)
	}
	if !contains(calls[0].Function.Arguments, "hello world") {
		t.Errorf("expected args to contain 'hello world', got %q", calls[0].Function.Arguments)
	}
}

func TestParseLocalToolCalls_LFM2_NoToolCalls(t *testing.T) {
	text := "Just a regular response with no tool calls."
	content, calls := parseLocalToolCalls("lfm2", text)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
	if content != text {
		t.Errorf("content should be unchanged")
	}
}

func TestParseLocalToolCalls_Qwen(t *testing.T) {
	text := "<tool_call>\n<function=read_file>\n<parameter=path>\n/some/file.go\n</parameter>\n</function>\n</tool_call>"
	content, calls := parseLocalToolCalls("qwen3_5_text", text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", calls[0].Function.Name)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestParseLocalToolCalls_Qwen_NoToolCalls(t *testing.T) {
	text := "Just a regular response."
	content, calls := parseLocalToolCalls("qwen3_5_text", text)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
	if content != text {
		t.Errorf("content should be unchanged")
	}
}

func TestFormatToolsPrompt_LFM2(t *testing.T) {
	tools := []api.Tool{
		{Type: "function", Function: api.ToolFunction{Name: "read_file", Description: "Read a file"}},
	}
	prompt := formatToolsPrompt("lfm2", tools)
	if prompt == "" {
		t.Fatal("LFM2 tool prompt should not be empty")
	}
	if !contains(prompt, "List of tools:") {
		t.Error("LFM2 tool prompt should contain 'List of tools:'")
	}
}

func TestFormatToolsPrompt_Qwen(t *testing.T) {
	tools := []api.Tool{
		{Type: "function", Function: api.ToolFunction{Name: "read_file", Description: "Read a file"}},
	}
	prompt := formatToolsPrompt("qwen3_5_text", tools)
	if prompt == "" {
		t.Fatal("Qwen tool prompt should not be empty")
	}
	if !contains(prompt, "<tool_call>") {
		t.Error("Qwen tool prompt should contain '<tool_call>'")
	}
}

func TestFormatAssistantToolCalls_LFM2(t *testing.T) {
	calls := []api.ToolCall{
		{
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/some/file.go"}`,
			},
		},
	}
	result := formatAssistantToolCalls("lfm2", calls)
	if !contains(result, "<|tool_call_start|>") {
		t.Error("LFM2 assistant tool calls should contain '<|tool_call_start|>'")
	}
	if !contains(result, "read_file(") {
		t.Error("LFM2 assistant tool calls should contain 'read_file('")
	}
}

func TestFormatAssistantToolCalls_Qwen(t *testing.T) {
	calls := []api.ToolCall{
		{
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/some/file.go"}`,
			},
		},
	}
	result := formatAssistantToolCalls("qwen3_5_text", calls)
	if !contains(result, "<tool_call>") {
		t.Error("Qwen assistant tool calls should contain '<tool_call>'")
	}
	if !contains(result, "read_file") {
		t.Error("Qwen assistant tool calls should contain function name")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
