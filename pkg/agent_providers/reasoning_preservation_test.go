package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func openRouterLikeConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "openrouter",
		Endpoint: "https://openrouter.ai/api/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		Defaults: RequestDefaults{Model: "anthropic/claude-sonnet-4.6"},
		Conversion: MessageConversion{
			ReasoningContentField:    "reasoning_content",
			PreserveReasoningDetails: true,
			UnifiedReasoningParam:    true,
			IncludeToolCallID:        true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
			DefaultModel:        "anthropic/claude-sonnet-4.6",
		},
	}
}

func reasoningDetailsJSON(blocks ...map[string]interface{}) string {
	raw, _ := json.Marshal(blocks)
	return string(raw)
}

func TestConvertMessagesReplaysReasoningDetailsArray(t *testing.T) {
	p, err := NewGenericProvider(openRouterLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	details := reasoningDetailsJSON(
		map[string]interface{}{"type": "reasoning.text", "text": "step 1", "signature": "sig1"},
		map[string]interface{}{"type": "reasoning.encrypted", "data": "eyJkYXRhIn0="},
	)
	msgs := []api.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "", ReasoningContent: "step 1", Meta: map[string]string{api.ReasoningDetailsMetaKey: details}, ToolCalls: []api.ToolCall{{ID: "c1", Type: "function", Function: api.ToolCallFunction{Name: "get_weather"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "sunny"},
	}
	converted := p.convertMessages(msgs, "")
	var assistant map[string]interface{}
	for _, e := range converted {
		if e["role"] == "assistant" {
			assistant = e
		}
	}
	if assistant == nil {
		t.Fatal("no assistant entry in converted messages")
	}
	raw, present := assistant["reasoning_details"]
	if !present {
		t.Fatalf("expected reasoning_details on assistant entry, got keys %v", keysOf(assistant))
	}
	arr, ok := raw.([]map[string]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2 reasoning blocks, got %#v", raw)
	}
	if arr[0]["signature"] != "sig1" {
		t.Fatalf("expected signature preserved, got %#v", arr[0]["signature"])
	}
	if _, hasString := assistant["reasoning_content"]; hasString {
		t.Fatal("expected string replay suppressed when details present")
	}
}

func TestConvertMessagesStringReplayWhenNoDetails(t *testing.T) {
	p, err := NewGenericProvider(openRouterLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	msgs := []api.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello", ReasoningContent: "thinking about greeting"},
	}
	converted := p.convertMessages(msgs, "")
	var assistant map[string]interface{}
	for _, e := range converted {
		if e["role"] == "assistant" {
			assistant = e
		}
	}
	if assistant["reasoning_content"] != "thinking about greeting" {
		t.Fatalf("expected string replay, got %#v", assistant["reasoning_content"])
	}
	if _, hasDetails := assistant["reasoning_details"]; hasDetails {
		t.Fatal("expected no reasoning_details for string-only message")
	}
}

func TestConvertMessagesMergePreservesReasoningDetails(t *testing.T) {
	p, err := NewGenericProvider(openRouterLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	details := reasoningDetailsJSON(map[string]interface{}{"type": "reasoning.text", "text": "earlier thought"})
	msgs := []api.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "a1", Meta: map[string]string{api.ReasoningDetailsMetaKey: details}},
		{Role: "assistant", Content: "a2"},
	}
	converted := p.convertMessages(msgs, "")
	assistants := 0
	for _, e := range converted {
		if e["role"] == "assistant" {
			assistants++
			if _, ok := e["reasoning_details"]; !ok {
				t.Fatalf("merged assistant lost reasoning_details, entry: %#v", e)
			}
		}
	}
	if assistants != 1 {
		t.Fatalf("expected merged single assistant, got %d", assistants)
	}
}

func TestBuildChatRequestUnifiedReasoningEffort(t *testing.T) {
	p, err := NewGenericProvider(openRouterLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "high", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reasoning, ok := req["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning={effort:high}, got %#v", req["reasoning"])
	}
	if _, has := req["reasoning_effort"]; has {
		t.Fatal("expected no top-level reasoning_effort for openrouter claude")
	}
	if _, has := req["thinking"]; has {
		t.Fatal("expected no thinking object for openrouter claude")
	}
}

func TestBuildChatRequestNoReasoningParamForUnsupportedModel(t *testing.T) {
	cfg := openRouterLikeConfig()
	cfg.Defaults.Model = "openai/gpt-5.2-chat"
	cfg.Models.DefaultModel = "openai/gpt-5.2-chat"
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "high", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := req["reasoning"]; has {
		t.Fatal("expected no reasoning object for model without reasoning support")
	}
}

func TestBuildChatRequestGptOssKeepsTopLevelEffort(t *testing.T) {
	// Non-unified providers keep the legacy top-level reasoning_effort knob
	// for gpt-oss models.
	cfg := openRouterLikeConfig()
	cfg.Defaults.Model = "openai/gpt-oss-120b"
	cfg.Models.DefaultModel = "openai/gpt-oss-120b"
	cfg.Conversion.UnifiedReasoningParam = false
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "medium", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort=medium, got %#v", req["reasoning_effort"])
	}
	if _, has := req["reasoning"]; has {
		t.Fatal("non-unified provider should not emit the unified reasoning object")
	}
}

func TestBuildChatRequestUnifiedReasoningPreferredForCatalogModel(t *testing.T) {
	// gpt-oss-120b lists the unified `reasoning` parameter in the OpenRouter
	// catalog, so a unified provider routes effort through it.
	cfg := openRouterLikeConfig()
	cfg.Defaults.Model = "openai/gpt-oss-120b"
	cfg.Models.DefaultModel = "openai/gpt-oss-120b"
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "medium", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reasoning, ok := req["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("expected reasoning={effort:medium}, got %#v", req["reasoning"])
	}
}

func TestBuildChatRequestDisableThinkingClaudeUsesUnifiedReasoningLow(t *testing.T) {
	p, err := NewGenericProvider(openRouterLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "medium", true, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reasoning, ok := req["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "low" {
		t.Fatalf("expected reasoning={effort:low} on disable path, got %#v", req["reasoning"])
	}
	if _, has := req["thinking"]; has {
		t.Fatal("expected no anthropic-native thinking object via openrouter")
	}
}

func TestDecodeChatResponseCapturesReasoningDetails(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":"answer","reasoning_details":[{"type":"reasoning.text","text":"why","signature":"s"},{"type":"reasoning.encrypted","data":"abc"}]}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`
	resp, err := decodeChatResponseWithCost(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw := resp.Choices[0].Message.Meta[api.ReasoningDetailsMetaKey]
	if raw == "" {
		t.Fatal("expected reasoning_details captured on Meta")
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) != 2 {
		t.Fatalf("expected 2 blocks, raw=%s err=%v", raw, err)
	}
}

func TestStreamingAccumulatesReasoningDetails(t *testing.T) {
	chunk1 := `{"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.text","text":"part1","index":0}]}}]}`
	chunk2 := `{"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","data":"enc","index":1}]}}]}`
	builder := api.NewStreamingResponseBuilder(nil)
	for _, data := range []string{chunk1, chunk2} {
		chunk, err := api.ParseSSEData(data)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := builder.ProcessChunk(chunk); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	resp := builder.GetResponse()
	raw := resp.Choices[0].Message.Meta[api.ReasoningDetailsMetaKey]
	if raw == "" {
		t.Fatal("expected reasoning_details in response Meta")
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) != 2 {
		t.Fatalf("expected 2 accumulated blocks, raw=%s err=%v", raw, err)
	}
	if arr[0]["text"] != "part1" || arr[1]["data"] != "enc" {
		t.Fatalf("blocks out of order: %#v", arr)
	}
}

func TestStreamingMinimaxStringDetailsStillFeedsReasoning(t *testing.T) {
	chunk := `{"choices":[{"index":0,"delta":{"reasoning_details":"minimax reasoning chunk"}}]}`
	builder := api.NewStreamingResponseBuilder(nil)
	parsed, err := api.ParseSSEData(chunk)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := builder.ProcessChunk(parsed); err != nil {
		t.Fatalf("process: %v", err)
	}
	resp := builder.GetResponse()
	if resp.Choices[0].Message.ReasoningContent != "minimax reasoning chunk" {
		t.Fatalf("expected string reasoning, got %#v", resp.Choices[0].Message.ReasoningContent)
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
