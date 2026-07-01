package providers

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestBuildOpenAIChatMessages_ToolConversion(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "tool", Content: "Generated output"},
		{Role: "assistant", Content: "Done", ToolCallID: "call_123"},
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

	// images come first (Anthropic recommendation), then text
	if content[0]["type"] != "image_url" {
		t.Fatalf("first element should be image_url, got %#v", content[0])
	}
	if content[1]["type"] != "text" {
		t.Fatalf("second element should be text, got %#v", content[1])
	}
}

// SP-103-B3: A message with 2 images and text produces [image, image, text] order.
func TestBuildOpenAIChatMessages_TwoImagesAndText_Order(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "user",
			Content: "first paragraph\n\nsecond paragraph",
			Images: []api.ImageData{
				{Base64: "aW1nQQ==", Type: "image/png"},
				{Base64: "aW1nQg==", Type: "image/png"},
			},
		},
	}

	result := BuildOpenAIChatMessages(messages, MessageConversionOptions{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{}, got %T", result[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("expected 3 blocks (2 images + 1 text), got %d", len(content))
	}

	// block[0] = first image
	if content[0]["type"] != "image_url" {
		t.Errorf("block[0] should be image_url, got %v", content[0]["type"])
	}
	img0URL, ok := content[0]["image_url"].(map[string]interface{})
	if !ok || !strings.Contains(img0URL["url"].(string), "aW1nQQ==") {
		t.Errorf("block[0] should contain first image (imgA), got %v", img0URL)
	}

	// block[1] = second image
	if content[1]["type"] != "image_url" {
		t.Errorf("block[1] should be image_url, got %v", content[1]["type"])
	}
	img1URL, ok := content[1]["image_url"].(map[string]interface{})
	if !ok || !strings.Contains(img1URL["url"].(string), "aW1nQg==") {
		t.Errorf("block[1] should contain second image (imgB), got %v", img1URL)
	}

	// block[2] = text
	if content[2]["type"] != "text" {
		t.Errorf("block[2] should be text, got %v", content[2]["type"])
	}
	if content[2]["text"] != "first paragraph\n\nsecond paragraph" {
		t.Errorf("block[2] text should be original content, got %q", content[2]["text"])
	}
}

// SP-103-B3: Images-only message (empty text) produces only image blocks.
func TestBuildOpenAIChatMessages_ImagesOnly_NoText(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "user",
			Content: "   ", // whitespace-only → treated as empty
			Images: []api.ImageData{
				{Base64: "aW1nQQ==", Type: "image/png"},
			},
		},
	}

	result := BuildOpenAIChatMessages(messages, MessageConversionOptions{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected content to be []map[string]interface{}, got %T", result[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 block (image only), got %d", len(content))
	}
	if content[0]["type"] != "image_url" {
		t.Errorf("block[0] should be image_url, got %v", content[0]["type"])
	}
}

// SP-103-B3: Text-only message (no images) produces plain string content.
func TestBuildOpenAIChatMessages_TextOnly_NoImages(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "user",
			Content: "just text",
			Images:  nil,
		},
	}

	result := BuildOpenAIChatMessages(messages, MessageConversionOptions{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	content, ok := result[0]["content"].(string)
	if !ok {
		t.Fatalf("expected plain string content, got %T", result[0]["content"])
	}
	if content != "just text" {
		t.Errorf("expected 'just text', got %q", content)
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
	if maxTokens != 64000 {
		t.Fatalf("expected default request cap 64000, got %d", maxTokens)
	}

	t.Setenv("LEDIT_MAX_REQUEST_COMPLETION_TOKENS", "200000")
	maxTokens = CalculateMaxTokensWithLimits(200000, 128000, messages, tools)
	if maxTokens != 128000 {
		t.Fatalf("expected completion cap 128000 with raised request cap, got %d", maxTokens)
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
