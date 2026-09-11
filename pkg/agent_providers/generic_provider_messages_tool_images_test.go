package providers

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Regression: a tool message with attached images (e.g. a read_file result
// carrying a screenshot) must keep string content on the wire. Providers
// like ai-worker (Qwen) reject multimodal-array tool content with
// "tool messages must contain string content".
func TestToolMessageWithImagesKeepsStringContent(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "ai-worker",
		Conversion: MessageConversion{IncludeToolCallID: true},
	}
	p := &GenericProvider{config: cfg}

	msgs := []api.Message{
		{Role: "user", Content: "look at the screenshot"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{
			ID: "call_123", Type: "function",
			Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"},
		}}},
		{
			Role:       "tool",
			Content:    "[image: shot.png (236755 bytes, image/png)] OCR text: some text",
			ToolCallID: "call_123",
			Images: []api.ImageData{
				{Base64: "aGVsbG8=", Type: "image/png"},
			},
		},
	}

	out := p.convertMessages(msgs, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	tool := out[2]
	if role, _ := tool["role"].(string); role != "tool" {
		t.Fatalf("expected role tool, got %v", tool["role"])
	}
	if s, ok := tool["content"].(string); !ok {
		t.Fatalf("tool content must be a string, got %T", tool["content"])
	} else if s == "" {
		t.Fatal("tool content must not be empty")
	}
	if id, _ := tool["tool_call_id"].(string); id != "call_123" {
		t.Fatalf("tool_call_id lost: %v", tool["tool_call_id"])
	}
}
