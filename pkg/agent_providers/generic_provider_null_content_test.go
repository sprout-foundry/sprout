package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestConvertMessages_EmptyContentAssistantWithToolCallsIsNull verifies that
// assistant messages with tool_calls and empty content are serialized with
// JSON null (not empty string) for the content field. Z.AI/GLM rejects
// "content": "" on these messages with HTTP 400 error code 1214:
// "The messages parameter is illegal." The OpenAI spec also expects null.
func TestConvertMessages_EmptyContentAssistantWithToolCallsIsNull(t *testing.T) {
	config := &ProviderConfig{
		Name:     "zai-coding",
		Endpoint: "https://api.z.ai/api/coding/paas/v4/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "ZAI_CODING_API_KEY"},
		Defaults: RequestDefaults{Model: "glm-5.2"},
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "glm-5.2",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{Role: "user", Content: "Run a command"},
		{
			Role:    "assistant",
			Content: "", // empty content, only tool_calls
			ToolCalls: []api.ToolCall{
				{
					ID:   "call_abc123",
					Type: "function",
					Function: api.ToolCallFunction{
						Name:      "shell_command",
						Arguments: `{"command":"echo hi"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			Content:    "hi",
			ToolCallID: "call_abc123",
		},
		{Role: "user", Content: "Thanks"},
	}

	converted := provider.convertMessages(messages, "")

	// The assistant message (index 1) should have nil content, not ""
	assistantMsg := converted[1]
	if assistantMsg["role"] != "assistant" {
		t.Fatalf("expected role 'assistant', got %v", assistantMsg["role"])
	}

	content, exists := assistantMsg["content"]
	if !exists {
		t.Fatalf("expected content key to exist (as nil), but it was missing")
	}
	if content != nil {
		t.Fatalf("expected content to be nil (JSON null), got %v (type %T)", content, content)
	}

	// Verify that JSON serialization produces null, not ""
	raw, err := json.Marshal(assistantMsg)
	if err != nil {
		t.Fatalf("failed to marshal assistant message: %v", err)
	}
	if !strings.Contains(string(raw), `"content":null`) {
		t.Fatalf("expected JSON to contain \"content\":null, got: %s", string(raw))
	}
	if strings.Contains(string(raw), `"content":""`) {
		t.Fatalf("JSON should NOT contain \"content\":\"\" (Z.AI rejects this), got: %s", string(raw))
	}
}

// TestConvertMessages_EmptyContentToolMessageIsPlaceholder verifies that an
// empty tool result is emitted as a non-empty string placeholder (not null):
// strict backends (ninfer, vLLM, llama.cpp) reject a tool message whose
// content is null with HTTP 400 "tool messages must contain content", so the
// placeholder keeps the wire valid while signalling "the tool ran, it
// produced nothing". This is the mirror image of the assistant+tool_calls
// case, which must stay null for Z.AI/GLM.
func TestConvertMessages_EmptyContentToolMessageIsPlaceholder(t *testing.T) {
	config := &ProviderConfig{
		Name:     "openai",
		Endpoint: "https://api.openai.com/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Defaults: RequestDefaults{Model: "gpt-4o"},
		Conversion: MessageConversion{
			IncludeToolCallID: true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
			DefaultModel:        "gpt-4o",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{Role: "user", Content: "Run a command"},
		{
			Role:    "assistant",
			Content: "Let me check.",
			ToolCalls: []api.ToolCall{
				{
					ID:   "call_xyz",
					Type: "function",
					Function: api.ToolCallFunction{
						Name:      "shell_command",
						Arguments: `{"command":"true"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			Content:    "", // empty tool result
			ToolCallID: "call_xyz",
		},
		{Role: "user", Content: "Done"},
	}

	converted := provider.convertMessages(messages, "")

	// Find the tool message and verify content is the non-empty placeholder
	// (a string), never nil.
	found := false
	for _, msg := range converted {
		if msg["role"] == "tool" {
			found = true
			content, ok := msg["content"].(string)
			if !ok {
				t.Fatalf("expected tool message content to be a string, got %v (type %T)", msg["content"], msg["content"])
			}
			if content == "" {
				t.Fatalf("expected non-empty placeholder tool content, got empty string")
			}
			if content != emptyToolContentPlaceholder {
				t.Fatalf("expected tool content %q, got %q", emptyToolContentPlaceholder, content)
			}
		}
	}
	if !found {
		t.Fatal("expected a tool message in the converted output")
	}
}

// TestConvertMessages_AssistantWithContentAndToolCallsPreservesContent verifies
// that non-empty content on assistant+tool_calls messages is NOT nulled.
func TestConvertMessages_AssistantWithContentAndToolCallsPreservesContent(t *testing.T) {
	config := &ProviderConfig{
		Name:     "zai-coding",
		Endpoint: "https://api.z.ai/api/coding/paas/v4/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "ZAI_CODING_API_KEY"},
		Defaults: RequestDefaults{Model: "glm-5.2"},
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "glm-5.2",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{Role: "user", Content: "Check the file"},
		{
			Role:    "assistant",
			Content: "Let me read it.",
			ToolCalls: []api.ToolCall{
				{
					ID:   "call_read1",
					Type: "function",
					Function: api.ToolCallFunction{
						Name:      "read_file",
						Arguments: `{"path":"test.txt"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			Content:    "file contents",
			ToolCallID: "call_read1",
		},
		{Role: "user", Content: "Great"},
	}

	converted := provider.convertMessages(messages, "")

	assistantMsg := converted[1]
	content, ok := assistantMsg["content"].(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", assistantMsg["content"])
	}
	if content != "Let me read it." {
		t.Fatalf("expected content 'Let me read it.', got %q", content)
	}
}

// TestBuildChatRequest_EmptyContentAssistantSerializesAsNull is an end-to-end
// test that verifies the actual JSON request body has null content for empty
// assistant+tool_calls messages, which strict providers reject with
// HTTP 400 "messages parameter is illegal" when serialized as "".
func TestBuildChatRequest_EmptyContentAssistantSerializesAsNull(t *testing.T) {
	config := &ProviderConfig{
		Name:     "zai-coding",
		Endpoint: "https://api.z.ai/api/coding/paas/v4/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "ZAI_CODING_API_KEY"},
		Defaults: RequestDefaults{Model: "glm-5.2"},
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "glm-5.2",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Simulate a realistic conversation with multiple empty-content
	// assistant messages — the pattern long agentic turns hit when every
	// assistant message carries only tool_calls.
	messages := []api.Message{
		{Role: "system", Content: "You are a coding assistant."},
		{Role: "user", Content: "Do the task"},
		// First empty-content assistant with tool_calls
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "call_1", Type: "function", Function: api.ToolCallFunction{Name: "search", Arguments: `{"query":"test"}`}},
			},
		},
		{Role: "tool", Content: "no results", ToolCallID: "call_1"},
		// Second empty-content assistant with tool_calls
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "call_2", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.go"}`}},
			},
		},
		{Role: "tool", Content: "file content", ToolCallID: "call_2"},
		// Normal assistant with content (the turn completion)
		{Role: "assistant", Content: "Done!"},
		{Role: "user", Content: "Next task"},
	}

	body, err := provider.buildChatRequest(messages, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest failed: %v", err)
	}

	bodyStr := string(body)
	// Must NOT contain any empty-string content on assistant/tool messages
	if strings.Contains(bodyStr, `"content":""`) {
		t.Fatalf("request body contains \"content\":\"\" which Z.AI/GLM rejects.\n"+
			"Body snippet: %s", truncateForLog(bodyStr, 500))
	}
	// Must contain null content for the empty assistant messages
	if !strings.Contains(bodyStr, `"content":null`) {
		t.Fatalf("expected request body to contain \"content\":null for empty assistant messages.\n"+
			"Body snippet: %s", truncateForLog(bodyStr, 500))
	}
}

// TestBuildChatRequest_EmptyToolResultSerializesAsPlaceholder is an
// end-to-end test mirroring the strict-backends invariant (ninfer, vLLM,
// llama.cpp): a tool-role message must never carry a null or missing
// content field, or the server rejects the whole request with HTTP 400
// "tool messages must contain content". An empty tool result must therefore
// be serialized as a non-empty string placeholder. We inspect the actual
// JSON body and assert every tool-role message has a string content.
func TestBuildChatRequest_EmptyToolResultSerializesAsPlaceholder(t *testing.T) {
	config := &ProviderConfig{
		Name:     "ai-worker",
		Endpoint: "https://ai-worker.example/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "AI_WORKER_API_KEY"},
		Defaults: RequestDefaults{Model: "qwen3.8-27b"},
		Conversion: MessageConversion{
			IncludeToolCallID: true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "qwen3.8-27b",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// A realistic tool loop where one tool call yields an empty result —
	// the exact pattern that 400s on ninfer-style backends.
	messages := []api.Message{
		{Role: "system", Content: "You are a coding assistant."},
		{Role: "user", Content: "Check the deployment"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{"command":"kubectl get pods"}`}},
			},
		},
		{Role: "tool", Content: "pod-1 1/1 Running", ToolCallID: "c1"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "c2", Type: "function", Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{"command":"true"}`}},
			},
		},
		{Role: "tool", Content: "", ToolCallID: "c2"}, // empty tool result
		{Role: "user", Content: "Done"},
	}

	body, err := provider.buildChatRequest(messages, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest failed: %v", err)
	}

	// Decode the wire body and assert the tool-role invariant.
	var payload struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	for i, m := range payload.Messages {
		if m["role"] != "tool" {
			continue
		}
		content, isString := m["content"].(string)
		if !isString {
			t.Fatalf("tool message %d has non-string content %v (type %T); strict backends 400 on null tool content", i, m["content"], m["content"])
		}
		if strings.TrimSpace(content) == "" {
			t.Fatalf("tool message %d has empty/whitespace content %q; strict backends require non-empty tool content", i, content)
		}
	}
	// The empty tool result specifically must carry the placeholder.
	if !strings.Contains(string(body), `"content":"`+emptyToolContentPlaceholder+`"`) {
		t.Fatalf("expected the empty tool result to serialize as %q in the wire body.\nBody: %s",
			emptyToolContentPlaceholder, truncateForLog(string(body), 600))
	}
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// TestEnforceToolContentInvariantIsDefenseInDepth verifies the final wire
// guard repairs a tool message that reached the wire with a null or missing
// content field — the exact shape that 400s on strict backends — regardless
// of which code path produced it. Non-tool messages are left untouched.
func TestEnforceToolContentInvariantIsDefenseInDepth(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
	}{
		{"null content", map[string]interface{}{"role": "tool", "content": nil}},
		{"missing content", map[string]interface{}{"role": "tool"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := enforceToolContentInvariant([]map[string]interface{}{tc.in})
			if out[0]["role"] != "tool" {
				t.Fatalf("role mangled: %v", out[0]["role"])
			}
			if c, ok := out[0]["content"].(string); !ok || c != emptyToolContentPlaceholder {
				t.Fatalf("expected tool content repaired to %q, got %v (type %T)", emptyToolContentPlaceholder, out[0]["content"], out[0]["content"])
			}
		})
	}

	// A tool message with a real (non-null) string must be left as-is.
	kept := enforceToolContentInvariant([]map[string]interface{}{
		{"role": "tool", "content": "result"},
	})
	if kept[0]["content"] != "result" {
		t.Fatalf("expected existing tool content preserved, got %v", kept[0]["content"])
	}
	// A non-tool message with null content must be untouched (assistant+
	// tool_calls legitimately carries null for Z.AI/GLM).
	assistant := enforceToolContentInvariant([]map[string]interface{}{
		{"role": "assistant", "content": nil},
	})
	if assistant[0]["content"] != nil {
		t.Fatalf("expected assistant null content untouched, got %v", assistant[0]["content"])
	}
}
