package providers

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// deepInfraReasoningLikeConfig builds a DeepInfra-shaped config (no unified
// object, a native reasoning_effort knob, reasoning_content replay wired) with
// a reasoning-capable model carrying a per-model ReasoningSpec.
func deepInfraReasoningLikeConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "deepinfra",
		Endpoint: "https://api.deepinfra.com/v1/openai/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "DEEPINFRA_API_KEY"},
		Defaults: RequestDefaults{Model: "deepseek-ai/DeepSeek-V4.1-Flash"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 131072,
			DefaultModel:        "deepseek-ai/DeepSeek-V4.1-Flash",
			ModelInfo: []ModelInfo{
				{
					ID:            "deepseek-ai/DeepSeek-V4.1-Flash",
					Tags:          []string{"tools", "vision", "reasoning", "structured_output"},
					ContextLength: 1048576,
					Reasoning:     &ReasoningSpec{Param: "reasoning_effort", DefaultEffort: "high"},
				},
			},
		},
	}
}

// TestApplyReasoningEffortNativeKnobDefault is the rel.1 default-policy
// guarantee: a request built with NO reasoning settings still enables
// reasoning on a reasoning-capable model — the host-native knob is emitted at
// the family's default depth.
func TestApplyReasoningEffortNativeKnobDefault(t *testing.T) {
	p, err := NewGenericProvider(deepInfraReasoningLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	request := map[string]interface{}{}
	p.applyReasoningEffort("deepseek-ai/DeepSeek-V4.1-Flash", "", request)
	if got := request["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want the family default %q", got, "high")
	}
}

// TestApplyReasoningEffortNativeKnobConfigured: a configured effort beats the
// family default.
func TestApplyReasoningEffortNativeKnobConfigured(t *testing.T) {
	p, err := NewGenericProvider(deepInfraReasoningLikeConfig())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	request := map[string]interface{}{}
	p.applyReasoningEffort("deepseek-ai/DeepSeek-V4.1-Flash", "medium", request)
	if got := request["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %v, want configured %q", got, "medium")
	}
}

// TestApplyReasoningEffortProviderLevelKnob: when a model has no per-model
// spec, the provider-level ReasoningEffortParam supplies the knob and the
// effort is still emitted for a reasoning-capable model.
func TestApplyReasoningEffortProviderLevelKnob(t *testing.T) {
	cfg := &ProviderConfig{
		Name:     "chutes",
		Endpoint: "https://api.chutes.ai/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "CHUTES_API_KEY"},
		Conversion: MessageConversion{
			ReasoningEffortParam: "reasoning_effort",
		},
		Models: ModelConfig{DefaultContextLimit: 131072, ModelInfo: []ModelInfo{
			{ID: "deepseek-ai/DeepSeek-V4.1-Flash", Tags: []string{"reasoning"}},
		}},
	}
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	request := map[string]interface{}{}
	// No per-model spec; the provider-level knob + the global fallback depth
	// must still enable reasoning (the "medium" fallback).
	p.applyReasoningEffort("deepseek-ai/DeepSeek-V4.1-Flash", "", request)
	if got := request["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %v, want the fallback %q", got, "medium")
	}
}

// TestApplyReasoningEffortNoKnobNoEmit: a non-reasoning model on a host with
// a native knob gets no knob (no spurious field, no capability it lacks).
func TestApplyReasoningEffortNoKnobNoEmit(t *testing.T) {
	cfg := &ProviderConfig{
		Name:     "deepinfra",
		Endpoint: "https://api.deepinfra.com/v1/openai/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "DEEPINFRA_API_KEY"},
		Conversion: MessageConversion{
			ReasoningEffortParam:  "reasoning_effort",
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{DefaultContextLimit: 131072, ModelInfo: []ModelInfo{
			{ID: "meta-llama/Llama-3.3-70B-Instruct", Tags: []string{"structured_output"}},
		}},
	}
	p, _ := NewGenericProvider(cfg)
	request := map[string]interface{}{}
	p.applyReasoningEffort("meta-llama/Llama-3.3-70B-Instruct", "high", request)
	if got := request["reasoning_effort"]; got != nil {
		t.Fatalf("expected no reasoning knob for a non-reasoning model, got %v", got)
	}
}

// TestDeepInfraReasoningContentReplayed is the rel.1 contract test against a
// recorded (synthetic) DeepInfra response: a reasoning-capable model's
// reasoning_content is captured on the message and replayed back into the
// next request (the replay path, reasoning_content_field, is already wired).
func TestDeepInfraReasoningContentReplayed(t *testing.T) {
	cfg := deepInfraReasoningLikeConfig()
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	// Simulate a recorded DeepInfra response: the assistant turn carried
	// reasoning_content which sprout captured. The next request must replay
	// it for the reasoning-capable model.
	msgs := []api.Message{
		{Role: "user", Content: "what is 2+2?"},
		{Role: "assistant", Content: "4", ReasoningContent: "Let me add the two integers."},
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
	raw, present := assistant["reasoning_content"]
	if !present {
		t.Fatal("expected reasoning_content to be replayed for the DeepInfra reasoning model")
	}
	if !strings.Contains(toString(raw), "add the two integers") {
		t.Errorf("replayed reasoning_content = %q, want it to contain the captured trace", toString(raw))
	}
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}

// A self-hosted endpoint serving Qwen 3.8 has no catalog entry: the family's
// own knob still applies, at medium unless a level it accepts is configured,
// and a level it would refuse ("high") gets medium instead.
func TestApplyReasoningEffortQwen38Family(t *testing.T) {
	p, err := NewGenericProvider(&ProviderConfig{
		Name:     "self-hosted",
		Endpoint: "https://inference.example/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "SELF_HOSTED_API_KEY"},
		Models:   ModelConfig{DefaultContextLimit: 262144},
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	for _, c := range []struct{ configured, want string }{
		{"", "medium"},
		{"low", "low"},
		{"xhigh", "xhigh"},
		{"high", "medium"},
	} {
		request := map[string]interface{}{}
		p.applyReasoningEffort("qwen3.8", c.configured, request)
		if got := request["reasoning_effort"]; got != c.want {
			t.Errorf("configured %q: reasoning_effort = %v, want %q", c.configured, got, c.want)
		}
	}
	request := map[string]interface{}{}
	p.applyReasoningEffort("some-other-model", "", request)
	if _, ok := request["reasoning_effort"]; ok {
		t.Errorf("a model with no knob got %v", request)
	}
}
