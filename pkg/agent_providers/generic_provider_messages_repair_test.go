package providers

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---- mergeConsecutiveAssistants ----

func TestMergeConsecutiveAssistants_MergesTwoAssistants(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "first", "tool_calls": []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
		}},
		{"role": "assistant", "content": "second", "tool_calls": []map[string]interface{}{
			{"id": "c2", "type": "function", "function": map[string]interface{}{"name": "read"}},
		}},
		{"role": "tool", "tool_call_id": "c2", "content": "data"},
	}
	got := mergeConsecutiveAssistants(in)

	// Should be: user, merged assistant (with c2's tool_calls), tool
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got=%v)", len(got), got)
	}
	if r, _ := got[1]["role"].(string); r != "assistant" {
		t.Fatalf("msg[1] role = %v, want assistant", got[1]["role"])
	}
	// Content should be merged
	content, _ := got[1]["content"].(string)
	if !strings.Contains(content, "first") || !strings.Contains(content, "second") {
		t.Errorf("merged content = %q, want both 'first' and 'second'", content)
	}
	// Tool calls should be from the second (the one with results following)
	tcs, _ := got[1]["tool_calls"].([]map[string]interface{})
	if len(tcs) != 1 {
		t.Fatalf("tool_calls len = %d, want 1 (from second assistant)", len(tcs))
	}
	if id, _ := tcs[0]["id"].(string); id != "c2" {
		t.Errorf("tool_call id = %q, want c2", id)
	}
}

func TestMergeConsecutiveAssistants_DropsEmptyAfterMerge(t *testing.T) {
	// Two assistants: first has only tool_calls (empty content), second has content.
	// After merge, the combined entry has content from second and tool_calls from second.
	// The first assistant's empty content doesn't create a separate empty entry.
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		// Assistant with empty content and tool_calls that will be orphaned
		{"role": "assistant", "content": "\n\n", "tool_calls": []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
		}},
		// Second assistant with real content
		{"role": "assistant", "content": "Let me read the file", "tool_calls": []map[string]interface{}{
			{"id": "c2", "type": "function", "function": map[string]interface{}{"name": "read"}},
		}},
		{"role": "tool", "tool_call_id": "c2", "content": "data"},
	}
	got := mergeConsecutiveAssistants(in)

	// Should be: user, merged assistant, tool
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got=%v)", len(got), got)
	}
	// Verify no empty assistant messages
	for i, m := range got {
		if r, _ := m["role"].(string); r == "assistant" {
			content, _ := m["content"].(string)
			tcs, _ := m["tool_calls"].([]map[string]interface{})
			if strings.TrimSpace(content) == "" && len(tcs) == 0 {
				t.Errorf("msg[%d] is an empty assistant (no content, no tool_calls)", i)
			}
		}
	}
}

func TestMergeConsecutiveAssistants_ThreeInARow(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "first"},
		{"role": "assistant", "content": "second"},
		{"role": "assistant", "content": "third", "tool_calls": []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
		}},
		{"role": "tool", "tool_call_id": "c1", "content": "data"},
	}
	got := mergeConsecutiveAssistants(in)

	// Should be: user, merged assistant, tool
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got=%v)", len(got), got)
	}
	content, _ := got[1]["content"].(string)
	if !strings.Contains(content, "first") || !strings.Contains(content, "second") || !strings.Contains(content, "third") {
		t.Errorf("merged content missing parts: %q", content)
	}
}

func TestMergeConsecutiveAssistants_NoConsecutive(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "response"},
	}
	got := mergeConsecutiveAssistants(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestMergeConsecutiveAssistants_Empty(t *testing.T) {
	got := mergeConsecutiveAssistants(nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestMergeConsecutiveAssistants_DropsStandaloneEmptyAssistant(t *testing.T) {
	// An assistant with no content and no tool_calls (e.g. "\n\n" only)
	// should be dropped even if it's not part of a consecutive pair.
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "\n\n"},
		{"role": "user", "content": "again"},
	}
	got := mergeConsecutiveAssistants(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (empty assistant dropped, got=%v)", len(got), got)
	}
	if r, _ := got[1]["role"].(string); r != "user" {
		t.Errorf("msg[1] role = %v, want user", got[1]["role"])
	}
}

// ---- dropEmptyAssistants ----

func TestDropEmptyAssistants(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "", "tool_calls": []map[string]interface{}{}},
		{"role": "assistant", "content": "\n\n"},
		{"role": "assistant", "content": "real response"},
		{"role": "user", "content": "ok"},
	}
	got := dropEmptyAssistants(in)
	// Two empty assistants should be dropped, keeping: user, assistant, user
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got=%v)", len(got), got)
	}
	content, _ := got[1]["content"].(string)
	if content != "real response" {
		t.Errorf("msg[1] content = %q, want 'real response'", content)
	}
}

func TestDropEmptyAssistants_KeepsAssistantWithToolCalls(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "assistant", "content": "", "tool_calls": []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
		}},
	}
	got := dropEmptyAssistants(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (assistant with tool_calls kept)", len(got))
	}
}

func TestDropEmptyAssistants_Empty(t *testing.T) {
	got := dropEmptyAssistants(nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

// ---- End-to-end: convertMessages applies cleanup for ALL providers ----

// TestConvertMessages_AppliesCleanupForAllProviders verifies that the
// orphaned-tool-call cleanup runs for non-strict providers (OpenAI, local
// Qwen, etc.), not just strict-syntax ones. This was previously gated on
// requiresStrictToolCallSyntax() and the corruption silently degraded
// model output on tolerant providers.
func TestConvertMessages_AppliesCleanupForAllProviders(t *testing.T) {
	looseConf := &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "go"},
		// First assistant with tool_calls but no results — orphaned
		{Role: "assistant", Content: "first search", ToolCalls: []api.ToolCall{{
			ID: "c1", Type: "function",
			Function: api.ToolCallFunction{Name: "search", Arguments: "{}"},
		}}},
		// Second assistant with tool_calls and results — valid
		{Role: "assistant", Content: "now read", ToolCalls: []api.ToolCall{{
			ID: "c2", Type: "function",
			Function: api.ToolCallFunction{Name: "read", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c2", Content: "data"},
	}

	p := &GenericProvider{config: looseConf, model: "qwen3.6-27b"}
	got := p.convertMessages(messages, "")

	// Verify: no consecutive assistant messages in output
	for i := 1; i < len(got); i++ {
		prevRole, _ := got[i-1]["role"].(string)
		currRole, _ := got[i]["role"].(string)
		if prevRole == "assistant" && currRole == "assistant" {
			t.Errorf("consecutive assistants at [%d]-[%d]: cleanup not applied for non-strict provider", i-1, i)
		}
	}

	// Verify: the surviving assistant has tool_calls from the second (c2)
	var assistantToolCallIDs []string
	for _, m := range got {
		if r, _ := m["role"].(string); r == "assistant" {
			if tcs, ok := m["tool_calls"].([]map[string]interface{}); ok {
				for _, tc := range tcs {
					if id, _ := tc["id"].(string); id != "" {
						assistantToolCallIDs = append(assistantToolCallIDs, id)
					}
				}
			}
		}
	}
	if len(assistantToolCallIDs) != 1 || assistantToolCallIDs[0] != "c2" {
		t.Errorf("expected surviving tool_call id c2, got %v", assistantToolCallIDs)
	}
}

// TestConvertMessages_ConsecutiveAssistantsRealWorldRepro reproduces the
// exact pattern seen in the diagnostic captures: multiple consecutive
// assistant messages with tool_calls, each representing one iteration of
// the conversation loop, where the tool results for earlier iterations
// were lost from state. This pattern caused 1,690 missing_result violations
// across 10 sessions.
func TestConvertMessages_ConsecutiveAssistantsRealWorldRepro(t *testing.T) {
	conf := &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	// Simulates the diagnostic pattern: messages 2-6 from the capture
	messages := []api.Message{
		{Role: "user", Content: "merge in remote changes"},
		// 5 consecutive assistant messages, each with tool_calls
		{Role: "assistant", Content: "</think>", ReasoningContent: "Let me check the repo state", ToolCalls: []api.ToolCall{{
			ID: "call_019f3f8b", Type: "function",
			Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{}`},
		}}},
		{Role: "assistant", Content: "Local and remote diverged", ToolCalls: []api.ToolCall{{
			ID: "call_019f3f8b4", Type: "function",
			Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{}`},
		}}},
		{Role: "assistant", Content: "Local has 2 commits", ToolCalls: []api.ToolCall{{
			ID: "call_019f3f8b6", Type: "function",
			Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{}`},
		}}},
		{Role: "assistant", Content: "Clean merge", ToolCalls: []api.ToolCall{{
			ID: "call_019f3f8b7", Type: "function",
			Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{}`},
		}}},
		{Role: "assistant", Content: "Merged cleanly", ToolCalls: []api.ToolCall{{
			ID: "call_019f3f8b7e", Type: "function",
			Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_019f3f8b7e", Content: "Build passed"},
	}

	p := &GenericProvider{config: conf, model: "qwen3.6-27b"}
	got := p.convertMessages(messages, "")

	// After cleanup: no consecutive assistant messages
	consecutiveCount := 0
	for i := 1; i < len(got); i++ {
		prevRole, _ := got[i-1]["role"].(string)
		currRole, _ := got[i]["role"].(string)
		if prevRole == "assistant" && currRole == "assistant" {
			consecutiveCount++
		}
	}
	if consecutiveCount > 0 {
		t.Errorf("found %d consecutive assistant pairs after cleanup — corruption not repaired", consecutiveCount)
	}

	// Verify: exactly one assistant survives (the merged one with the last tool_call)
	assistantCount := 0
	for _, m := range got {
		if r, _ := m["role"].(string); r == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 1 {
		t.Errorf("expected 1 merged assistant, got %d", assistantCount)
	}
}

// TestConvertMessages_PreservesWellFormedConversation ensures the cleanup
// doesn't alter conversations that are already correct (no false positives).
func TestConvertMessages_PreservesWellFormedConversation(t *testing.T) {
	conf := &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "search for foo"},
		{Role: "assistant", Content: "searching", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "search", Arguments: `{}`}},
			{ID: "c2", Type: "function", Function: api.ToolCallFunction{Name: "read", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "result1"},
		{Role: "tool", ToolCallID: "c2", Content: "result2"},
		{Role: "assistant", Content: "I found it."},
	}

	p := &GenericProvider{config: conf, model: "qwen3.6-27b"}
	got := p.convertMessages(messages, "")

	// Should preserve: user, assistant(with tool_calls), tool, tool, assistant
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (well-formed conversation should be unchanged)", len(got))
	}
	// First assistant should still have tool_calls
	if tcs, ok := got[1]["tool_calls"].([]map[string]interface{}); !ok || len(tcs) != 2 {
		t.Errorf("first assistant lost tool_calls: %v", got[1])
	}
}

// TestConvertMessages_DropCreatesConsecutiveAssistants verifies that when
// dropOrphanToolResults removes a tool message separating two assistants,
// the merge pass catches the newly-exposed consecutive pair. This requires
// the pipeline order: strip → drop → merge.
func TestConvertMessages_DropCreatesConsecutiveAssistants(t *testing.T) {
	conf := &ProviderConfig{
		Name: "ai-worker",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: false,
		},
	}

	messages := []api.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "first", ToolCalls: []api.ToolCall{{
			ID: "c1", Type: "function",
			Function: api.ToolCallFunction{Name: "search", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c2", Content: "wrong-id"}, // orphan — c1 was stripped
		{Role: "assistant", Content: "second", ToolCalls: []api.ToolCall{{
			ID: "c3", Type: "function",
			Function: api.ToolCallFunction{Name: "read", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c3", Content: "data"}, // valid
	}

	p := &GenericProvider{config: conf, model: "qwen3.6-27b"}
	got := p.convertMessages(messages, "")

	for i := 1; i < len(got); i++ {
		prevRole, _ := got[i-1]["role"].(string)
		currRole, _ := got[i]["role"].(string)
		if prevRole == "assistant" && currRole == "assistant" {
			t.Fatalf("consecutive assistants at [%d]-[%d] after cleanup: %v", i-1, i, got)
		}
	}
}

// TestMergeConsecutiveAssistants_PreservesReasoningContent verifies that
// reasoning_content from the earlier assistant survives the merge.
func TestMergeConsecutiveAssistants_PreservesReasoningContent(t *testing.T) {
	in := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "first", "reasoning_content": "I should search for that"},
		{"role": "assistant", "content": "second", "tool_calls": []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "search"}},
		}},
		{"role": "tool", "tool_call_id": "c1", "content": "data"},
	}
	got := mergeConsecutiveAssistants(in)

	// Should be: user, merged assistant, tool
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (got=%v)", len(got), got)
	}
	// The merged assistant should preserve reasoning_content from the first
	rc, ok := got[1]["reasoning_content"]
	if !ok {
		t.Fatal("reasoning_content missing from merged assistant")
	}
	if rcStr, _ := rc.(string); rcStr != "I should search for that" {
		t.Errorf("reasoning_content = %q, want 'I should search for that'", rcStr)
	}
}
