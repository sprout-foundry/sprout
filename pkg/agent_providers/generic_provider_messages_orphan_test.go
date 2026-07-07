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

// ---- stripUnansweredToolCalls tests ----

// TestStripUnansweredToolCalls exercises the safety net that prevents
// MiniMax 2013 errors caused by assistant messages with tool_calls but
// no following tool results — the signature failure mode after checkpoint
// or structural compaction consumes tool results but leaves their parent
// assistant messages in the history.
func TestStripUnansweredToolCalls(t *testing.T) {
	asstWithContent := func(content, id string) map[string]interface{} {
		return map[string]interface{}{
			"role":    "assistant",
			"content": content,
			"tool_calls": []map[string]interface{}{
				{"id": id, "type": "function", "function": map[string]interface{}{"name": "search"}},
			},
		}
	}
	asstMulti := func(content string, ids ...string) map[string]interface{} {
		tcs := make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			tcs = append(tcs, map[string]interface{}{
				"id": id, "type": "function", "function": map[string]interface{}{"name": "search"},
			})
		}
		return map[string]interface{}{"role": "assistant", "content": content, "tool_calls": tcs}
	}
	tool := func(id string) map[string]interface{} {
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": id,
			"content":      "result",
		}
	}

	cases := []struct {
		name           string
		in             []map[string]interface{}
		expectToolCall bool // whether any assistant in output should retain tool_calls
		expectedLen    int
	}{
		{
			name:           "well-formed: assistant + result keeps tool_calls",
			in:             []map[string]interface{}{asstWithContent("searching", "c1"), tool("c1")},
			expectToolCall: true,
			expectedLen:    2,
		},
		{
			name: "orphan: assistant without result strips tool_calls",
			in: []map[string]interface{}{
				asstWithContent("searching", "c1"),
				{"role": "user", "content": "next"},
			},
			expectToolCall: false,
			expectedLen:    2,
		},
		{
			name: "parallel calls: all answered keeps tool_calls",
			in: []map[string]interface{}{
				asstMulti("searching", "c1", "c2"),
				tool("c1"),
				tool("c2"),
			},
			expectToolCall: true,
			expectedLen:    3,
		},
		{
			name: "parallel calls: one missing strips tool_calls",
			in: []map[string]interface{}{
				asstMulti("searching", "c1", "c2"),
				tool("c1"),
				// c2 missing
			},
			expectToolCall: false,
			expectedLen:    2,
		},
		{
			name:           "assistant without tool_calls unchanged",
			in:             []map[string]interface{}{{"role": "assistant", "content": "hello"}},
			expectToolCall: false,
			expectedLen:    1,
		},
		{
			name:           "non-assistant messages unchanged",
			in:             []map[string]interface{}{{"role": "user", "content": "hi"}},
			expectToolCall: false,
			expectedLen:    1,
		},
		{
			name:           "empty slice unchanged",
			in:             nil,
			expectToolCall: false,
			expectedLen:    0,
		},
		{
			name: "multiple assistants: mixed answered/orphan",
			in: []map[string]interface{}{
				asstWithContent("first", "c1"),
				tool("c1"),
				asstWithContent("second", "c2"),
				// c2 orphan
			},
			expectToolCall: true, // c1's assistant keeps tool_calls
			expectedLen:    3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripUnansweredToolCalls(tc.in)
			if len(got) != tc.expectedLen {
				t.Fatalf("len = %d, want %d (got=%v)", len(got), tc.expectedLen, got)
			}
			// Check whether any assistant retained tool_calls.
			anyHasToolCalls := false
			for _, m := range got {
				if r, _ := m["role"].(string); r == "assistant" {
					if tcs, ok := m["tool_calls"]; ok && tcs != nil {
						anyHasToolCalls = true
					}
				}
			}
			if anyHasToolCalls != tc.expectToolCall {
				t.Errorf("expectToolCall=%v but anyHasToolCalls=%v", tc.expectToolCall, anyHasToolCalls)
			}
		})
	}
}

// TestStripUnansweredToolCalls_PreservesContent verifies that the
// assistant message's text content survives even when tool_calls are
// stripped — the model should still see what the assistant said.
func TestStripUnansweredToolCalls_PreservesContent(t *testing.T) {
	in := []map[string]interface{}{
		{
			"role":    "assistant",
			"content": "I'll search for that.",
			"tool_calls": []map[string]interface{}{
				{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
			},
		},
		// No tool result — orphan
		{"role": "user", "content": "next"},
	}
	got := stripUnansweredToolCalls(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	asst, ok := got[0]["role"].(string)
	if !ok || asst != "assistant" {
		t.Fatalf("first message should be assistant, got role=%v", got[0]["role"])
	}
	if _, hasTCs := got[0]["tool_calls"]; hasTCs {
		t.Error("tool_calls should be stripped from orphan assistant")
	}
	content, _ := got[0]["content"].(string)
	if content != "I'll search for that." {
		t.Errorf("content = %q, want %q", content, "I'll search for that.")
	}
}

// TestConvertMessages_StripsUnansweredToolCalls_ForStrictProvider is the
// end-to-end repro: a strict-syntax provider (MiniMax) sees tool_calls
// stripped from orphaned assistant messages, preventing 2013 errors.
func TestConvertMessages_StripsUnansweredToolCalls_ForStrictProvider(t *testing.T) {
	strictConf := &ProviderConfig{
		Name: "minimax",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "searching", ToolCalls: []api.ToolCall{{
			ID: "c1", Type: "function",
			Function: api.ToolCallFunction{Name: "search", Arguments: "{}"},
		}}},
		// No tool result — compaction consumed it
		{Role: "user", Content: "what next?"},
	}

	p := &GenericProvider{config: strictConf, model: "MiniMax-M2.5"}
	got := p.convertMessages(messages, "")
	for _, m := range got {
		if r, _ := m["role"].(string); r == "assistant" {
			if _, hasTCs := m["tool_calls"]; hasTCs {
				t.Errorf("strict provider kept tool_calls on orphaned assistant: %v", m)
			}
		}
	}
}

// TestConvertMessages_PartialParallel_StrictProvider is the regression test
// for the pipeline ordering bug: when an assistant has parallel tool_calls
// and only SOME have results (compaction consumed the others), stripping
// tool_calls must happen BEFORE dropOrphanToolResults. Otherwise the
// remaining tool result becomes an orphan following an assistant with no
// tool_calls — reproducing the exact 2013 error the fix targets.
func TestConvertMessages_PartialParallel_StrictProvider(t *testing.T) {
	strictConf := &ProviderConfig{
		Name: "minimax",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "searching", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "search", Arguments: "{}"}},
			{ID: "c2", Type: "function", Function: api.ToolCallFunction{Name: "read", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "result1"}, // c2 result consumed by compaction
		{Role: "user", Content: "next"},
	}

	p := &GenericProvider{config: strictConf, model: "MiniMax-M2.5"}
	got := p.convertMessages(messages, "")

	// No tool message should follow an assistant without tool_calls.
	for i, m := range got {
		role, _ := m["role"].(string)
		if role != "tool" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			prevRole, _ := got[j]["role"].(string)
			if prevRole == "assistant" {
				if _, hasTCs := got[j]["tool_calls"]; !hasTCs {
					t.Errorf("orphaned tool result at [%d] follows stripped assistant at [%d] — ordering bug", i, j)
				}
				break
			}
			if prevRole != "tool" {
				break
			}
		}
	}
}
