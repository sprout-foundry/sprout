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
			IncludeToolCallID:      true,
			ReasoningContentField:  "reasoning_content",
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

// TestConvertMessages_EmptyContentToolMessageIsNull verifies the same null
// normalization applies to tool-role messages with empty content.
func TestConvertMessages_EmptyContentToolMessageIsNull(t *testing.T) {
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

	// Find the tool message and verify content is nil
	for _, msg := range converted {
		if msg["role"] == "tool" {
			if content := msg["content"]; content != nil {
				t.Fatalf("expected tool message content to be nil, got %v (type %T)", content, content)
			}
		}
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
			IncludeToolCallID:      true,
			ReasoningContentField:  "reasoning_content",
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
// assistant+tool_calls messages, simulating the real Z.AI request that
// triggered HTTP 400 "messages parameter is illegal".
func TestBuildChatRequest_EmptyContentAssistantSerializesAsNull(t *testing.T) {
	config := &ProviderConfig{
		Name:     "zai-coding",
		Endpoint: "https://api.z.ai/api/coding/paas/v4/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "ZAI_CODING_API_KEY"},
		Defaults: RequestDefaults{Model: "glm-5.2"},
		Conversion: MessageConversion{
			IncludeToolCallID:      true,
			ReasoningContentField:  "reasoning_content",
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
	// assistant messages (the exact pattern from the 86-minute turn that
	// triggered the 400 error).
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

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
