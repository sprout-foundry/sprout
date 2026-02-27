package providers

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestBuildOpenAIChatMessages_ToolConversion(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "tool", Content: "Generated output"},
		{Role: "assistant", Content: "Done", ToolCallId: "call_123"},
	}

	opts := MessageConversionOptions{ConvertToolRoleToUser: true, IncludeToolCallID: true}
	result := BuildOpenAIChatMessages(messages, opts)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if role := result[1]["role"]; role != "user" {
		t.Fatalf("expected tool role to convert to user, got %v", role)
	}

	content, ok := result[1]["content"].(string)
	if !ok || content != "Tool Response:\nGenerated output" {
		t.Fatalf("unexpected converted tool content: %#v", result[1]["content"])
	}

	if _, ok := result[2]["tool_call_id"]; !ok {
		t.Fatalf("expected tool_call_id to be preserved on assistant message")
	}
}

func TestBuildOpenAIChatMessages_Images(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "user",
			Content: "Check this screenshot",
			Images:  []api.ImageData{{URL: "https://example.com/image.png"}},
		},
	}

	result := BuildOpenAIChatMessages(messages, MessageConversionOptions{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("expected multimodal content slice, got %#v", result[0]["content"])
	}

	if content[0]["type"] != "text" {
		t.Fatalf("first element should be text, got %#v", content[0])
	}
	if content[1]["type"] != "image_url" {
		t.Fatalf("second element should be image_url, got %#v", content[1])
	}
}

func TestBuildOpenAIStreamingMessages(t *testing.T) {
	messages := []api.Message{{Role: "assistant", Content: "Streaming"}}
	stream := BuildOpenAIStreamingMessages(messages, MessageConversionOptions{})
	if len(stream) != 1 {
		t.Fatalf("expected 1 streaming message, got %d", len(stream))
	}
}

func TestBuildOpenAIToolsPayload(t *testing.T) {
	tool := api.Tool{Type: "function"}
	tool.Function.Name = "example"
	tool.Function.Description = "do something"
	tool.Function.Parameters = map[string]interface{}{"type": "object"}

	tools := []api.Tool{tool}

	payload := BuildOpenAIToolsPayload(tools)
	if len(payload) != 1 {
		t.Fatalf("expected 1 tool payload, got %d", len(payload))
	}

	fn, ok := payload[0]["function"].(map[string]interface{})
	if !ok || fn["name"] != "example" {
		t.Fatalf("unexpected tool payload: %#v", payload[0])
	}
}

func TestCalculateMaxTokensBounds(t *testing.T) {
	messages := []api.Message{{Role: "user", Content: "hello"}}
	tools := []api.Tool{}

	maxTokens := CalculateMaxTokens(4000, messages, tools)
	if maxTokens <= 0 {
		t.Fatalf("expected positive max tokens, got %d", maxTokens)
	}

	maxTokens = CalculateMaxTokens(2000, []api.Message{{Content: string(make([]byte, 8000))}}, nil)
	if maxTokens < 256 {
		t.Fatalf("expected lower bound of 256, got %d", maxTokens)
	}
}

func TestCalculateMaxTokensWithCompletionLimit(t *testing.T) {
	messages := []api.Message{{Role: "user", Content: "hello"}}
	tools := []api.Tool{}

	maxTokens := CalculateMaxTokensWithLimits(200000, 128000, messages, tools)
	if maxTokens != 128000 {
		t.Fatalf("expected completion cap 128000, got %d", maxTokens)
	}
}

func TestEstimateInputTokens(t *testing.T) {
	messages := []api.Message{{Content: "12345678"}}
	tools := []api.Tool{{}}

	tokens := EstimateInputTokens(messages, tools)
	if tokens <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokens)
	}

	tokens2 := EstimateInputTokens(append(messages, api.Message{Content: "more"}), tools)
	if tokens2 <= tokens {
		t.Fatalf("expected additional content to increase estimate (%d vs %d)", tokens2, tokens)
	}
}
