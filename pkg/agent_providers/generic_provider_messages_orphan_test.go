package providers

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestRequiresStrictToolCallSyntax covers the strict-syntax detection
// used by convertMessages to decide whether to apply the orphan-tool
// safety net. Mistakenly tagging a non-strict provider (OpenAI, Anthropic,
// OpenRouter, ZAI, etc.) as strict causes harmless extra work; mistakenly
// NOT tagging a strict one (MiniMax, DeepSeek) reproduces the original bug.
func TestRequiresStrictToolCallSyntax(t *testing.T) {
	cases := []struct {
		name   string
		conf   *ProviderConfig
		model  string
		expect bool
	}{
		{name: "minimax provider", conf: &ProviderConfig{Name: "minimax"}, model: "MiniMax-M2.5", expect: true},
		{name: "deepseek provider", conf: &ProviderConfig{Name: "deepseek"}, model: "deepseek-chat", expect: true},
		{name: "model contains minimax", conf: &ProviderConfig{Name: "openrouter"}, model: "minimax-m2.5", expect: true},
		{name: "model contains deepseek", conf: &ProviderConfig{Name: "openrouter"}, model: "deepseek-reasoner", expect: true},
		{name: "openai not strict", conf: &ProviderConfig{Name: "openai"}, model: "gpt-5", expect: false},
		{name: "openrouter not strict", conf: &ProviderConfig{Name: "openrouter"}, model: "anthropic/claude-sonnet-4", expect: false},
		{name: "zai not strict", conf: &ProviderConfig{Name: "zai"}, model: "glm-4.6", expect: false},
		{name: "empty config", conf: &ProviderConfig{Name: ""}, model: "", expect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &GenericProvider{config: tc.conf, model: tc.model}
			got := p.requiresStrictToolCallSyntax()
			if got != tc.expect {
				t.Errorf("requiresStrictToolCallSyntax(%s/%s) = %v, want %v",
					tc.conf.Name, tc.model, got, tc.expect)
			}
		})
	}
}

func TestRequiresStrictToolCallSyntax_NilSafe(t *testing.T) {
	// Nil config must not panic.
	p := &GenericProvider{}
	if p.requiresStrictToolCallSyntax() {
		t.Error("provider with nil config should return false")
	}
}

// TestDropOrphanToolResults exercises the safety net helper directly. Each
// case encodes a conversation shape that historically produced MiniMax
// error 2013 ("tool call result does not follow tool call").
func TestDropOrphanToolResults(t *testing.T) {
	asst := func(id string) map[string]interface{} {
		return map[string]interface{}{
			"role": "assistant",
			"tool_calls": []map[string]interface{}{
				{"id": id, "type": "function", "function": map[string]interface{}{"name": "echo"}},
			},
		}
	}
	asstMulti := func(ids ...string) map[string]interface{} {
		tcs := make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			tcs = append(tcs, map[string]interface{}{
				"id": id, "type": "function", "function": map[string]interface{}{"name": "echo"},
			})
		}
		return map[string]interface{}{"role": "assistant", "tool_calls": tcs}
	}
	tool := func(id string) map[string]interface{} {
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": id,
			"content":      "ok",
		}
	}

	cases := []struct {
		name     string
		in       []map[string]interface{}
		expected []map[string]interface{}
	}{
		{
			name:     "well-formed: assistant + matching tool kept",
			in:       []map[string]interface{}{asst("c1"), tool("c1")},
			expected: []map[string]interface{}{asst("c1"), tool("c1")},
		},
		{
			name:     "orphan tool dropped",
			in:       []map[string]interface{}{tool("c1")},
			expected: []map[string]interface{}{},
		},
		{
			name:     "orphan tool after non-assistant dropped",
			in:       []map[string]interface{}{{"role": "user", "content": "hi"}, tool("c1")},
			expected: []map[string]interface{}{{"role": "user", "content": "hi"}},
		},
		{
			name:     "orphan tool after assistant with different id dropped",
			in:       []map[string]interface{}{asst("c1"), tool("c2")},
			expected: []map[string]interface{}{asst("c1")},
		},
		{
			name:     "multiple tools: only orphan dropped",
			in:       []map[string]interface{}{asst("c1"), tool("c1"), tool("c2")},
			expected: []map[string]interface{}{asst("c1"), tool("c1")},
		},
		{
			// Regression: with parallel tool calls, tool(c2) is preceded
			// by another tool (not the assistant). The orphan check must
			// walk backward past the tool block to find the parent.
			name:     "parallel tool calls: both results kept",
			in:       []map[string]interface{}{asstMulti("c1", "c2"), tool("c1"), tool("c2")},
			expected: []map[string]interface{}{asstMulti("c1", "c2"), tool("c1"), tool("c2")},
		},
		{
			// Regression: with one parallel call and one orphan, the
			// walker must keep c1 (matched parent) and drop c2 (no match).
			name:     "parallel tool calls: one matched one orphan",
			in:       []map[string]interface{}{asstMulti("c1"), tool("c1"), tool("c2")},
			expected: []map[string]interface{}{asstMulti("c1"), tool("c1")},
		},
		{
			// Regression: tool message with empty tool_call_id must be
			// preserved (e.g. providers with IncludeToolCallID=false).
			name:     "tool with empty tool_call_id is preserved (not proven orphan)",
			in:       []map[string]interface{}{asst("c1"), {"role": "tool", "content": "ok"}},
			expected: []map[string]interface{}{asst("c1"), {"role": "tool", "content": "ok"}},
		},
		{
			name:     "empty slice unchanged",
			in:       nil,
			expected: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dropOrphanToolResults(tc.in)
			if len(got) != len(tc.expected) {
				t.Fatalf("len mismatch: got %d, want %d (got=%v)", len(got), len(tc.expected), got)
			}
			for i := range got {
				if got[i]["role"] != tc.expected[i]["role"] {
					t.Errorf("[%d] role = %v, want %v", i, got[i]["role"], tc.expected[i]["role"])
				}
				if wantID, ok := tc.expected[i]["tool_call_id"]; ok {
					if got[i]["tool_call_id"] != wantID {
						t.Errorf("[%d] tool_call_id = %v, want %v", i, got[i]["tool_call_id"], wantID)
					}
				}
			}
		})
	}
}

// TestConvertMessages_StripsOrphanToolResults_ForStrictProvider is the
// end-to-end repro: a strict-syntax provider (MiniMax) sees the orphan
// tool message removed before serialization, but a non-strict provider
// (OpenAI) passes it through unchanged.
func TestConvertMessages_StripsOrphanToolResults_ForStrictProvider(t *testing.T) {
	strictConf := &ProviderConfig{
		Name: "minimax",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}
	looseConf := &ProviderConfig{
		Name: "openai",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{
			ID: "c1", Type: "function",
			Function: api.ToolCallFunction{Name: "echo", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c2", Content: "ok"}, // orphan — id mismatch
	}

	strictP := &GenericProvider{config: strictConf, model: "MiniMax-M2.5"}
	got := strictP.convertMessages(messages, "")
	if hasRole(got, "tool") {
		t.Errorf("strict provider kept the orphan tool message: %v", got)
	}

	looseP := &GenericProvider{config: looseConf, model: "gpt-5"}
	got = looseP.convertMessages(messages, "")
	if !hasRole(got, "tool") {
		t.Errorf("non-strict provider dropped the tool message: %v", got)
	}
}

// TestConvertMessages_StripsOrphanToolResults_PreservesNonToolFlow is a
// smoke test ensuring the safety net doesn't change the behavior of
// normal flows like consecutive user messages (which the merging logic
// collapses).
func TestConvertMessages_StripsOrphanToolResults_PreservesNonToolFlow(t *testing.T) {
	conf := &ProviderConfig{Name: "openai"}
	p := &GenericProvider{config: conf, model: "gpt-5"}
	in := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "world"},
	}
	got := p.convertMessages(in, "")
	// Two user messages should be merged into one.
	users := 0
	var content string
	for _, m := range got {
		if r, _ := m["role"].(string); r == "user" {
			users++
			if c, ok := m["content"].(string); ok {
				content = c
			}
		}
	}
	if users != 1 {
		t.Errorf("expected 1 merged user message, got %d: %v", users, got)
	}
	if !strings.Contains(content, "hello") || !strings.Contains(content, "world") {
		t.Errorf("merged content missing pieces: %q", content)
	}
}

// hasRole returns true if any converted message has the given role.
func hasRole(converted []map[string]interface{}, role string) bool {
	for _, m := range converted {
		if r, _ := m["role"].(string); r == role {
			return true
		}
	}
	return false
}
