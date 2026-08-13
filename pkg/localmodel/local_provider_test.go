//go:build darwin && arm64 && cgo

package localmodel

import (
	"encoding/json"
	"reflect"
	"strings"
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

// Models emit Qwen tool calls with wildly inconsistent whitespace. These cases
// are all real outputs observed from qwen3.5-4b inside the agent loop; the
// previous line-oriented parser silently dropped parameters or the entire call.
func TestParseQwenToolCalls_WhitespaceVariants(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantName string
		wantArgs string
	}{
		{
			name:     "canonical multiline",
			text:     "<tool_call>\n<function=shell_command>\n<parameter=command>go test ./...</parameter>\n</function>\n</tool_call>",
			wantName: "shell_command",
			wantArgs: `{"command":"go test ./..."}`,
		},
		{
			name:     "entirely on one line",
			text:     "<tool_call> <function=shell_command> <parameter=command> go test ./... </parameter> </function></tool_call>",
			wantName: "shell_command",
			wantArgs: `{"command":"go test ./..."}`,
		},
		{
			name:     "value shares line with closing tags",
			text:     "<tool_call>\n<function=shell_command>\n<parameter=command> go test ./... 2>&1 </parameter> </function></tool_call>",
			wantName: "shell_command",
			wantArgs: `{"command":"go test ./... 2>&1"}`,
		},
		{
			name:     "unterminated tool_call",
			text:     "<tool_call>\n<function=read_file>\n<parameter=path>stats.go</parameter>",
			wantName: "read_file",
			wantArgs: `{"path":"stats.go"}`,
		},
		{
			name:     "unclosed parameter before closing function",
			text:     "<tool_call><function=read_file><parameter=path>stats.go</function></tool_call>",
			wantName: "read_file",
			wantArgs: `{"path":"stats.go"}`,
		},
		{
			name:     "function without tool_call wrapper",
			text:     "<function=read_file><parameter=path>stats.go</parameter></function>",
			wantName: "read_file",
			wantArgs: `{"path":"stats.go"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := parseQwenToolCalls(tc.text)
			if len(calls) != 1 {
				t.Fatalf("got %d tool calls, want 1", len(calls))
			}
			if calls[0].Function.Name != tc.wantName {
				t.Errorf("name = %q, want %q", calls[0].Function.Name, tc.wantName)
			}
			// Compare decoded values: json.Marshal HTML-escapes >, &, < in
			// argument strings, so comparing raw JSON text is brittle.
			var got, want map[string]interface{}
			if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &got); err != nil {
				t.Fatalf("args not valid JSON: %v (%s)", err, calls[0].Function.Arguments)
			}
			if err := json.Unmarshal([]byte(tc.wantArgs), &want); err != nil {
				t.Fatalf("bad test fixture: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("args = %v, want %v", got, want)
			}
		})
	}
}

func TestParseQwenToolCalls_MultipleAndContent(t *testing.T) {
	text := "I'll check both files.\n" +
		"<tool_call><function=read_file><parameter=path>a.go</parameter></function></tool_call>\n" +
		"<tool_call><function=read_file><parameter=path>b.go</parameter></function></tool_call>"
	content, calls := parseQwenToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if content != "I'll check both files." {
		t.Errorf("content = %q, want %q", content, "I'll check both files.")
	}
	if calls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Errorf("call 0 args = %s", calls[0].Function.Arguments)
	}
	if calls[1].Function.Arguments != `{"path":"b.go"}` {
		t.Errorf("call 1 args = %s", calls[1].Function.Arguments)
	}
}

func TestParseQwenToolCalls_NoCalls(t *testing.T) {
	text := "Just a plain answer with no tool usage."
	content, calls := parseQwenToolCalls(text)
	if len(calls) != 0 {
		t.Fatalf("got %d tool calls, want 0", len(calls))
	}
	if content != text {
		t.Errorf("content = %q, want unchanged", content)
	}
}

func TestParseQwenToolCalls_JSONValuedParameter(t *testing.T) {
	text := `<tool_call><function=edit><parameter=lines>[1,2,3]</parameter></function></tool_call>`
	_, calls := parseQwenToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].Function.Arguments != `{"lines":[1,2,3]}` {
		t.Errorf("args = %s, want JSON array preserved", calls[0].Function.Arguments)
	}
}

var _ = api.ToolCall{}

// Gemma-family models emit <key=value> instead of <parameter=key>value.
func TestParseQwenToolCalls_GemmaInlineParams(t *testing.T) {
	text := "<tool_call>\n<function=write_file>\n<path=stats.go>\n<content=package modeltest\nfunc F() {}\n</content>\n</tool_call>"
	_, calls := parseQwenToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args not JSON: %v", err)
	}
	if args["path"] != "stats.go" {
		t.Errorf("path = %v, want stats.go", args["path"])
	}
	content, _ := args["content"].(string)
	if !strings.Contains(content, "package modeltest") || !strings.Contains(content, "func F() {}") {
		t.Errorf("content did not survive: %q", content)
	}
	if !strings.Contains(content, "\n") {
		t.Errorf("content lost its newlines: %q", content)
	}
}

// The inline fallback must not fire when standard parameters parse fine.
func TestParseQwenToolCalls_InlineFallbackDoesNotOverrideStandard(t *testing.T) {
	text := "<tool_call><function=shell_command><parameter=command>go test ./...</parameter></function></tool_call>"
	_, calls := parseQwenToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if len(args) != 1 || args["command"] != "go test ./..." {
		t.Errorf("args = %v, want only command", args)
	}
}
