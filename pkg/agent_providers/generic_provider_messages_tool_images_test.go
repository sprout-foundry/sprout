package providers

import (
	"encoding/json"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Tool messages must keep string content on the wire (strict backends
// reject multimodal arrays there), and the pixels must NOT be dropped:
// they are re-delivered as a trailing user message (the OpenAI-compat
// pattern) so a vision-capable primary actually sees tool output images.
func TestToolMessageWithImagesKeepsStringContent(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "ai-worker",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Defaults:   RequestDefaults{Model: "qwen3.8-27b"},
		Models: ModelConfig{
			SupportsVision: true,
		},
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
			Content:    "[image: shot.png (236755 bytes, image/png)]",
			ToolCallID: "call_123",
			Images: []api.ImageData{
				{Base64: "aGVsbG8=", Type: "image/png"},
			},
		},
	}

	out := p.convertMessages(msgs, "")
	if len(out) != 4 {
		t.Fatalf("expected 4 messages (user, assistant, tool, image delivery), got %d: %v", len(out), out)
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

	// The trailing user message carries the pixels.
	delivery := out[3]
	if role, _ := delivery["role"].(string); role != "user" {
		t.Fatalf("expected image-delivery message role=user, got %v", delivery["role"])
	}
	parts, ok := delivery["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected multimodal content array, got %T", delivery["content"])
	}
	var sawImage, sawText bool
	for _, part := range parts {
		switch partType, _ := part["type"].(string); partType {
		case "image_url":
			sawImage = true
			iu, _ := part["image_url"].(map[string]interface{})
			url, _ := iu["url"].(string)
			if url != "data:image/png;base64,aGVsbG8=" {
				t.Fatalf("expected data-URI image, got %q", url)
			}
		case "text":
			sawText = true
		}
	}
	if !sawImage || !sawText {
		t.Fatalf("expected image and text parts, got image=%v text=%v", sawImage, sawText)
	}
}

// Multiple tool results in one batch: their images are delivered together
// in one trailing user message, in call order.
func TestToolResultImagesCollectedAcrossBatch(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "ai-worker",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Defaults:   RequestDefaults{Model: "qwen3.8-27b"},
		Models:     ModelConfig{SupportsVision: true},
	}
	p := &GenericProvider{config: cfg}

	msgs := []api.Message{
		{Role: "user", Content: "compare two screenshots"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
			{ID: "c2", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "[image: a.png]", Images: []api.ImageData{{Base64: "YQ==", Type: "image/png"}}},
		{Role: "tool", ToolCallID: "c2", Content: "[image: b.png]", Images: []api.ImageData{{Base64: "Yg==", Type: "image/png"}}},
	}

	out := p.convertMessages(msgs, "")
	if len(out) != 5 {
		t.Fatalf("expected 5 messages (user, assistant, tool, tool, delivery), got %d", len(out))
	}
	delivery := out[4]
	if role, _ := delivery["role"].(string); role != "user" {
		t.Fatalf("expected trailing user message, got %v", delivery["role"])
	}
	parts, ok := delivery["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected multimodal array, got %T", delivery["content"])
	}
	imageCount := 0
	for _, part := range parts {
		if partType, _ := part["type"].(string); partType == "image_url" {
			imageCount++
		}
	}
	if imageCount != 2 {
		t.Fatalf("expected 2 images in delivery message, got %d", imageCount)
	}
}

// ConvertToolRoleToUser providers end on a user-role tool result; the
// images must merge into that message instead of appending a second
// consecutive user message.
func TestToolResultImagesMergeIntoConvertedUserTail(t *testing.T) {
	cfg := &ProviderConfig{
		Name: "deepinfra",
		Conversion: MessageConversion{
			IncludeToolCallID:     true,
			ConvertToolRoleToUser: true,
		},
		Defaults: RequestDefaults{Model: "google/gemma-3-27b-it"},
		Models:   ModelConfig{SupportsVision: true},
	}
	p := &GenericProvider{config: cfg}

	msgs := []api.Message{
		{Role: "user", Content: "look"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "[image: a.png]", Images: []api.ImageData{{Base64: "YQ==", Type: "image/png"}}},
	}

	out := p.convertMessages(msgs, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (user, assistant, merged tool→user), got %d", len(out))
	}
	tail := out[2]
	if role, _ := tail["role"].(string); role != "user" {
		t.Fatalf("expected tail role=user, got %v", tail["role"])
	}
	parts, ok := tail["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected merged multimodal content, got %T", tail["content"])
	}
	sawImage := false
	for _, part := range parts {
		if partType, _ := part["type"].(string); partType == "image_url" {
			sawImage = true
		}
	}
	if !sawImage {
		t.Fatal("expected image part merged into trailing user message")
	}
}

// Images on tool results are dropped when the provider has no vision at
// all (seed normally strips them earlier; this is the belt-and-suspenders
// check for direct callers).
func TestToolResultImagesOmittedForNonVisionProvider(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "text-only",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Models:     ModelConfig{SupportsVision: false},
	}
	p := &GenericProvider{config: cfg}

	msgs := []api.Message{
		{Role: "user", Content: "look"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "result text", Images: []api.ImageData{{Base64: "YQ==", Type: "image/png"}}},
	}

	out := p.convertMessages(msgs, "")
	for i, m := range out {
		if m["content"] == nil {
			continue
		}
		if _, isArr := m["content"].([]map[string]interface{}); isArr {
			t.Fatalf("message %d carries multimodal content on a non-vision provider: %v", i, m)
		}
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

// Assistant turns may carry images in state (a screenshot-producing turn,
// or an image attached to the assistant's own reply). On the wire they must
// NOT become a multimodal content array: strict OpenAI-compat backends
// (DeepInfra) reject a non-string assistant content with HTTP 422 "Input
// should be a valid string" (param messages.<i>.assistant.content.str), and
// because the malformed message lives in persistent history the session is
// wedged — every request re-sends it, the 422 is non-retryable, and its body
// carries no image keyword so the image-strip self-heal never fires. The
// assistant keeps its (string) content; the pixels are not re-delivered —
// the model re-reads the image file when it needs the pixels again.
func TestAssistantMessageWithImagesKeepsStringContent(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "deepinfra",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Defaults:   RequestDefaults{Model: "deepseek-ai/DeepSeek-V4.1-Flash-1017"},
		Models:     ModelConfig{SupportsVision: true},
	}
	p := &GenericProvider{config: cfg}

	// Non-mergeable path: assistant with tool_calls (carries a screenshot).
	// Mirror of the DeepInfra session that wedged on HTTP 422.
	nonMergeable := []api.Message{
		{Role: "user", Content: "make the results page accessible"},
		{Role: "assistant", Content: "Now the results page screenshot:",
			ToolCalls: []api.ToolCall{{ID: "c1", Type: "function",
				Function: api.ToolCallFunction{Name: "computer_use", Arguments: "{\"action\":\"take_screenshot\"}"}}},
			Images: []api.ImageData{{Base64: "aGVsbG8=", Type: "image/png"}}},
		{Role: "tool", ToolCallID: "c1", Content: "Screenshot saved"},
	}
	out := p.convertMessages(nonMergeable, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	asst := out[1]
	if role, _ := asst["role"].(string); role != "assistant" {
		t.Fatalf("expected assistant role, got %v", role)
	}
	c, ok := asst["content"].(string)
	if !ok {
		t.Fatalf("assistant content must be a string on the wire, got %T: %v", asst["content"], asst["content"])
	}
	if c == "" {
		t.Fatal("assistant content must be preserved as a string")
	}
	if _, has := asst["tool_calls"]; !has {
		t.Fatal("assistant tool_calls must be preserved")
	}

	// Mergeable path: a bare assistant message (no tool_calls) with images.
	mergeable := []api.Message{
		{Role: "user", Content: "now check the QR code"},
		{Role: "assistant", Content: "Now the QR code — generating it locally",
			Images: []api.ImageData{{Base64: "aGVsbG8=", Type: "image/png"}}},
	}
	out = p.convertMessages(mergeable, "")
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if c, ok := out[1]["content"].(string); !ok {
		t.Fatalf("mergeable assistant content must be a string, got %T", out[1]["content"])
	} else if c == "" {
		t.Fatal("mergeable assistant content must be preserved")
	}
}

// User messages are the one role allowed a multimodal content array: an
// image-bearing user turn (pasted screenshot) still embeds its images.
func TestUserMessageWithImagesKeepsMultimodalContent(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "deepinfra",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Defaults:   RequestDefaults{Model: "deepseek-ai/DeepSeek-V4.1-Flash-1017"},
		Models:     ModelConfig{SupportsVision: true},
	}
	p := &GenericProvider{config: cfg}

	msgs := []api.Message{
		{Role: "user", Content: "look at this", Images: []api.ImageData{{Base64: "aGVsbG8=", Type: "image/png"}}},
	}
	out := p.convertMessages(msgs, "")
	parts, ok := out[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("user content must be a multimodal array, got %T", out[0]["content"])
	}
	sawImage := false
	for _, part := range parts {
		if partType, _ := part["type"].(string); partType == "image_url" {
			sawImage = true
		}
	}
	if !sawImage {
		t.Fatal("expected an image_url part in the user multimodal content")
	}
}

// Cache stability: a delivery inserted in iteration N must not move when
// iteration N+1 appends more history. Prefix-stable insertions keep
// provider prompt caching (vLLM auto-prefix cache, Anthropic
// cache_control) hitting across tool iterations of one turn.
func TestToolResultImageDeliveriesArePrefixStable(t *testing.T) {
	cfg := &ProviderConfig{
		Name:       "ai-worker",
		Conversion: MessageConversion{IncludeToolCallID: true},
		Defaults:   RequestDefaults{Model: "qwen3.8-27b"},
		Models:     ModelConfig{SupportsVision: true},
	}
	p := &GenericProvider{config: cfg}

	base := []api.Message{
		{Role: "user", Content: "compare two screenshots over two rounds"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "[image: a.png]", Images: []api.ImageData{{Base64: "YQ==", Type: "image/png"}}},
	}

	round1 := p.convertMessages(base, "")

	grown := append(append([]api.Message{}, base...),
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "c2", Type: "function", Function: api.ToolCallFunction{Name: "read_file", Arguments: "{}"}},
		}},
		api.Message{Role: "tool", ToolCallID: "c2", Content: "[image: b.png]", Images: []api.ImageData{{Base64: "Yg==", Type: "image/png"}}},
	)
	round2 := p.convertMessages(grown, "")

	if len(round1) != 4 {
		t.Fatalf("round1: expected 4 messages, got %d", len(round1))
	}
	if len(round2) != 7 {
		t.Fatalf("round2: expected 7 messages, got %d", len(round2))
	}

	// Round 1's prefix (through its delivery message) must be byte-stable
	// in round 2's output.
	prefixStable := true
	for i := range round1 {
		got, _ := json.Marshal(round2[i])
		want, _ := json.Marshal(round1[i])
		if string(got) != string(want) {
			prefixStable = false
			t.Errorf("message %d changed between rounds:\n round1=%s\n round2=%s", i, want, got)
		}
	}
	if !prefixStable {
		t.Fatal("tool-image delivery broke prefix stability — provider prompt caches would miss every iteration")
	}
}
