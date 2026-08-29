package providers

import (
	"encoding/json"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func localVLLMLikeConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "sprout-local",
		Endpoint: "http://127.0.0.1:18081/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "qwen3.6-27b"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
			IncludeToolCallID:     true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
			DefaultModel:        "qwen3.6-27b",
		},
	}
}

func decodeRequest(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return req
}

func TestChatTemplateKwargsPreserveThinkingDefaultOnForQwen36(t *testing.T) {
	p, err := NewGenericProvider(localVLLMLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)
	kwargs, ok := req["chat_template_kwargs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected chat_template_kwargs for qwen3.6 on loopback, got %#v", req["chat_template_kwargs"])
	}
	if kwargs["preserve_thinking"] != true {
		t.Fatalf("expected preserve_thinking=true default, got %#v", kwargs["preserve_thinking"])
	}
}

func TestChatTemplateKwargsSuppressedWhenThinkingDisabled(t *testing.T) {
	p, err := NewGenericProvider(localVLLMLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "", true, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)
	kwargs, ok := req["chat_template_kwargs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected chat_template_kwargs present for disable path, got %#v", req["chat_template_kwargs"])
	}
	if _, has := kwargs["preserve_thinking"]; has {
		t.Fatal("preserve_thinking must not be injected when thinking disabled")
	}
	if kwargs["enable_thinking"] != false {
		t.Fatalf("expected enable_thinking=false inside chat_template_kwargs, got %#v", kwargs["enable_thinking"])
	}
	if _, hasTopLevel := req["enable_thinking"]; hasTopLevel {
		t.Fatal("loopback must not emit top-level enable_thinking (ignored by vLLM/llama.cpp)")
	}
}

func TestChatTemplateKwargsNotSentForHostedEndpoint(t *testing.T) {
	cfg := openRouterLikeConfig()
	cfg.Defaults.Model = "qwen/qwen3.6-27b"
	cfg.Models.DefaultModel = "qwen/qwen3.6-27b"
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)
	if _, has := req["chat_template_kwargs"]; has {
		t.Fatal("chat_template_kwargs must never be sent to hosted endpoints")
	}
}

func TestChatTemplateKwargsExplicitConfigWins(t *testing.T) {
	cfg := localVLLMLikeConfig()
	cfg.Conversion.ChatTemplateKwargs = map[string]interface{}{"preserve_thinking": false}
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)
	kwargs := req["chat_template_kwargs"].(map[string]interface{})
	if v, has := kwargs["preserve_thinking"]; !has || v != false {
		t.Fatalf("explicit config value must win over default, got %#v", kwargs)
	}
}

func TestChatTemplateKwargsNotInjectedForOlderQwen(t *testing.T) {
	cfg := localVLLMLikeConfig()
	cfg.Defaults.Model = "qwen3-8b"
	cfg.Models.DefaultModel = "qwen3-8b"
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "q"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req := decodeRequest(t, body)
	if _, has := req["chat_template_kwargs"]; has {
		t.Fatal("preserve_thinking default applies only to Qwen3.6+ families")
	}
}

func TestLocalReasoningContentReplayed(t *testing.T) {
	p, err := NewGenericProvider(localVLLMLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	msgs := []api.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer", ReasoningContent: "prior turn thoughts"},
		{Role: "user", Content: "second"},
	}
	converted := p.convertMessages(msgs, "")
	for _, e := range converted {
		if e["role"] == "assistant" && e["reasoning_content"] != "prior turn thoughts" {
			t.Fatalf("expected reasoning_content replayed for local provider, got %#v", e)
		}
	}
}
