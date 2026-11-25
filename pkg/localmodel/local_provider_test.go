//go:build darwin && arm64 && cgo

package localmodel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sprout-foundry/sinter/llm"
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

// TestFormatQwenAssistantToolCalls_DeterministicParamOrder guards against
// nondeterministic map iteration reordering a tool call's parameters on
// every replay. When the reconstructed text isn't byte-identical across
// calls, it can't stay an exact prefix match of what was actually cached,
// which silently defeats the KV prefix cache for any turn after a
// multi-parameter tool call.
func TestFormatQwenAssistantToolCalls_DeterministicParamOrder(t *testing.T) {
	calls := []api.ToolCall{{
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      "edit_file",
			Arguments: `{"path":"stats.go","new_str":"x","old_str":"y","zzz":"z","aaa":"a"}`,
		},
	}}
	first := formatQwenAssistantToolCalls(calls)
	for range 20 {
		if got := formatQwenAssistantToolCalls(calls); got != first {
			t.Fatalf("formatQwenAssistantToolCalls is nondeterministic across calls:\nfirst=%q\ngot=%q", first, got)
		}
	}
}

func TestFormatLFM2AssistantToolCalls_DeterministicParamOrder(t *testing.T) {
	calls := []api.ToolCall{{
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      "edit_file",
			Arguments: `{"path":"stats.go","new_str":"x","old_str":"y","zzz":"z","aaa":"a"}`,
		},
	}}
	first := formatLFM2AssistantToolCalls(calls)
	for range 20 {
		if got := formatLFM2AssistantToolCalls(calls); got != first {
			t.Fatalf("formatLFM2AssistantToolCalls is nondeterministic across calls:\nfirst=%q\ngot=%q", first, got)
		}
	}
}

func TestLeadingSystemMessages(t *testing.T) {
	cases := []struct {
		name string
		in   []api.Message
		want int
	}{
		{"single system", []api.Message{{Role: "system", Content: "x"}, {Role: "user", Content: "y"}}, 1},
		{"multiple leading system", []api.Message{{Role: "system", Content: "a"}, {Role: "system", Content: "b"}, {Role: "user", Content: "c"}}, 2},
		{"no system", []api.Message{{Role: "user", Content: "y"}}, 0},
		{"empty", nil, 0},
		{"system not leading is ignored", []api.Message{{Role: "user", Content: "y"}, {Role: "system", Content: "x"}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := leadingSystemMessages(tc.in)
			if len(got) != tc.want {
				t.Fatalf("leadingSystemMessages(%v) = %d messages, want %d", tc.in, len(got), tc.want)
			}
		})
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

func TestParseMiniCPM5ToolCalls(t *testing.T) {
	text := `<function name="read_file"><param name="path">/some/file.go</param></function>`
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", calls[0].Function.Name)
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["path"] != "/some/file.go" {
		t.Errorf("args[path] = %v, want /some/file.go", args["path"])
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestParseMiniCPM5ToolCalls_CDATA(t *testing.T) {
	text := `<function name="write_file"><param name="content"><![CDATA[hello <world> & friends\nline2]]></param></function>`
	_, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	content, _ := args["content"].(string)
	if !strings.Contains(content, "hello <world> & friends") || !strings.Contains(content, "line2") {
		t.Errorf("CDATA content lost its inner markup: %q", content)
	}
}

// An empty CDATA body must parse as an empty string, not fall through to
// the plain branch where the regex can swallow the ]]> tail as the value.
func TestParseMiniCPM5ToolCalls_EmptyCDATA(t *testing.T) {
	text := `<function name="write_file"><param name="content"><![CDATA[]]></param></function>`
	_, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	content, _ := args["content"].(string)
	if content != "" {
		t.Errorf("empty CDATA should parse as empty string, got %q", content)
	}
}

// A CDATA value containing ]]> must round-trip: parse keeps the literal,
// replay re-escapes it so the markup stays parseable.
func TestMiniCPM5CDATA_CloseBracketRoundTrip(t *testing.T) {
	const tricky = `array literal: a]]>b`
	_, calls := parseMiniCPM5ToolCalls(
		`<function name="write_file"><param name="content"><![CDATA[` + tricky + `]]></param></function>`)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if got, _ := args["content"].(string); got != tricky {
		t.Errorf("CDATA with ]]>> lost content: got %q, want %q", got, tricky)
	}

	replayed := formatMiniCPM5AssistantToolCalls(calls)
	_, back := parseMiniCPM5ToolCalls(replayed)
	if len(back) != 1 {
		t.Fatalf("replayed markup unparseable: %q", replayed)
	}
	var backArgs map[string]interface{}
	_ = json.Unmarshal([]byte(back[0].Function.Arguments), &backArgs)
	if got, _ := backArgs["content"].(string); got != tricky {
		t.Errorf("replay round-trip broke content: got %q, want %q", got, tricky)
	}
}

// A param value containing the literal "</function>" (a model writing code)
// must not end the call early: the scanner tracks open/close depth, so the
// call survives to its real close and the value round-trips whole.
func TestParseMiniCPM5ToolCalls_LiteralCloseInValue(t *testing.T) {
	inner := "call it via </function> and then retry"
	text := `<function name="write_file"><param name="content"><![CDATA[` + inner + `]]></param></function>`
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d (%q)", len(calls), content)
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if got, _ := args["content"].(string); got != inner {
		t.Errorf("literal </function> in value truncated the call: got %q, want %q", got, inner)
	}
	if content != "" {
		t.Errorf("expected empty visible content, got %q", content)
	}
}

// Same hazard in the plain (non-CDATA) form: the first close tag after the
// open is another param's value, not the call's close.
func TestParseMiniCPM5ToolCalls_LiteralCloseInPlainValue(t *testing.T) {
	text := `<function name="write_file"><param name="content">a </function> trick</param></function>`
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d (%q)", len(calls), content)
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if got, _ := args["content"].(string); got != "a </function> trick" {
		t.Errorf("plain value with literal close parsed as %q", got)
	}
}

// A plain (non-CDATA) param value that happens to contain the literal text
// "<![CDATA[" must parse as the plain branch — the value text, not an empty
// CDATA body.
func TestParseMiniCPM5ToolCalls_PlainValueWithCDATALiteral(t *testing.T) {
	text := `<function name="write_file"><param name="content">docs: <![CDATA[ syntax</param></function>`
	_, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if got, _ := args["content"].(string); got != "docs: <![CDATA[ syntax" {
		t.Errorf("plain value with CDATA marker parsed as %q", got)
	}
}

// A CDATA-wrapped call (the form the tool prompt tells the model to use for
// any value with <, & or a newline — i.e. nearly every write_file) must end
// at its own close: a second call after it parses separately, and trailing
// visible text survives.
func TestParseMiniCPM5ToolCalls_CDATAThenSecondCall(t *testing.T) {
	text := `<function name="write_file"><param name="content"><![CDATA[hello <world>]]></param></function>` +
		`<function name="read_file"><param name="path">/b.go</param></function>`
	_, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	var wargs map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &wargs)
	if wargs["content"] != "hello <world>" {
		t.Errorf("first call content = %v", wargs["content"])
	}
	if calls[1].Function.Name != "read_file" {
		t.Errorf("second call = %q, want read_file", calls[1].Function.Name)
	}
}

// Trailing visible text after a CDATA-wrapped call is content, not part of
// the call body.
func TestParseMiniCPM5ToolCalls_CDATAThenTrailingText(t *testing.T) {
	text := `<function name="write_file"><param name="content"><![CDATA[hello <world>]]></param></function>` + "\nDone."
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if content != "Done." {
		t.Errorf("trailing text lost: content = %q", content)
	}
}

// Two calls back to back must each parse as their own call — the body
// regex is non-greedy, so the first close tag ends the first call.
func TestParseMiniCPM5ToolCalls_TwoAdjacentCalls(t *testing.T) {
	text := `<function name="read_file"><param name="path">/a.go</param></function>` +
		`<function name="read_file"><param name="path">/b.go</param></function>`
	_, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	for i, want := range []string{"/a.go", "/b.go"} {
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(calls[i].Function.Arguments), &args)
		if got, _ := args["path"].(string); got != want {
			t.Errorf("call %d: path = %q, want %q", i, got, want)
		}
	}
}

func TestParseMiniCPM5ToolCalls_NoToolCalls(t *testing.T) {
	text := "Just a regular response."
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
	if content != text {
		t.Errorf("content should be unchanged")
	}
}

func TestParseMiniCPM5ToolCalls_Preamble(t *testing.T) {
	text := `Let me check that file.
<function name="read_file"><param name="path">/a.go</param></function>`
	content, calls := parseMiniCPM5ToolCalls(text)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if !strings.Contains(content, "Let me check") {
		t.Errorf("expected preamble to survive, got %q", content)
	}
}

func TestFormatMiniCPM5ToolsPrompt(t *testing.T) {
	tools := []api.Tool{
		{Type: "function", Function: api.ToolFunction{Name: "read_file", Description: "Read a file"}},
	}
	prompt := formatMiniCPM5ToolsPrompt(tools)
	if prompt == "" {
		t.Fatal("MiniCPM5 tool prompt should not be empty")
	}
	if !contains(prompt, "<tools>") {
		t.Error("MiniCPM5 tool prompt should contain <tools>")
	}
	if !contains(prompt, "read_file") {
		t.Error("MiniCPM5 tool prompt should contain function name")
	}
	if !contains(prompt, "<function name=") {
		t.Error("MiniCPM5 tool prompt should contain the call-format example")
	}
}

func TestFormatMiniCPM5AssistantToolCalls(t *testing.T) {
	calls := []api.ToolCall{
		{
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/some/file.go"}`,
			},
		},
	}
	result := formatMiniCPM5AssistantToolCalls(calls)
	if !contains(result, `<function name="read_file">`) {
		t.Errorf("expected native function tag, got %q", result)
	}
	if !contains(result, `<param name="path">/some/file.go</param>`) {
		t.Errorf("expected native param tag, got %q", result)
	}
}

func TestFormatMiniCPM5AssistantToolCalls_CDATA(t *testing.T) {
	calls := []api.ToolCall{
		{
			Type: "function",
			Function: api.ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"content":"hello <world>\nline2"}`,
			},
		},
	}
	result := formatMiniCPM5AssistantToolCalls(calls)
	if !contains(result, "<![CDATA[") {
		t.Errorf("multi-line value should be wrapped in CDATA, got %q", result)
	}
	if !contains(result, "hello <world>") {
		t.Errorf("CDATA content missing, got %q", result)
	}
}

func TestFormatToolsPrompt_MiniCPM5(t *testing.T) {
	tools := []api.Tool{
		{Type: "function", Function: api.ToolFunction{Name: "read_file", Description: "Read a file"}},
	}
	prompt := formatToolsPrompt(minicpm5Arch, tools)
	if prompt == "" {
		t.Fatal("MiniCPM5 tool prompt should not be empty")
	}
	if !contains(prompt, "<tools>") {
		t.Error("MiniCPM5 tool prompt should contain <tools>")
	}
}

func TestParseLocalToolCalls_MiniCPM5(t *testing.T) {
	text := `<function name="read_file"><param name="path">/some/file.go</param></function>`
	content, calls := parseLocalToolCalls(minicpm5Arch, text)
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

func TestEffectiveArch_MiniCPM5(t *testing.T) {
	dir := t.TempDir()
	tokPath := dir + "/tokenizer.json"
	content := `{"added_tokens":[{"content":"<|im_start|>","id":130072},{"content":"/think","id":130080},{"content":"/no_think","id":130081}]}`
	if err := writeFile(tokPath, []byte(content)); err != nil {
		t.Fatal(err)
	}
	if !isMiniCPM5Model(dir) {
		t.Error("expected isMiniCPM5Model to detect adjacent /think + /no_think")
	}
	cfg := llm.ModelConfig{Arch: "llama"}
	if got := effectiveArch(cfg, dir); got != minicpm5Arch {
		t.Errorf("expected %q, got %q", minicpm5Arch, got)
	}
}

// A tokenizer with the control tokens but on non-adjacent ids (Qwen3's
// layout) is not MiniCPM5 — effectiveArch must keep the config's arch.
func TestEffectiveArch_NonAdjacentThinkTokens(t *testing.T) {
	dir := t.TempDir()
	tokPath := dir + "/tokenizer.json"
	content := `{"added_tokens":[{"content":"/think","id":76511},{"content":"/no_think","id":76519}]}`
	if err := writeFile(tokPath, []byte(content)); err != nil {
		t.Fatal(err)
	}
	if isMiniCPM5Model(dir) {
		t.Error("non-adjacent /think ids must not detect as MiniCPM5")
	}
	cfg := llm.ModelConfig{Arch: "qwen3"}
	if got := effectiveArch(cfg, dir); got != "qwen3" {
		t.Errorf("expected qwen3, got %q", got)
	}
}

func TestEffectiveArch_NonMiniCPM5(t *testing.T) {
	dir := t.TempDir()
	if isMiniCPM5Model(dir) {
		t.Error("empty dir should not be MiniCPM5")
	}
	cfg := llm.ModelConfig{Arch: "llama"}
	if got := effectiveArch(cfg, dir); got != "llama" {
		t.Errorf("expected llama, got %q", got)
	}
}
