package providers

import (
	"encoding/json"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestBuildChatRequestConfigSamplingBeatsCatalog locks the "config wins over
// built-in" contract: the model-settings catalog (applied during request
// build) must not silently replace an operator's configured per-model
// temperature/top_p for a model it knows about.
func TestBuildChatRequestConfigSamplingBeatsCatalog(t *testing.T) {
	// "mistral-small-latest" matches the built-in mistral family profile,
	// whose recommended temperature is 0.7 — our configured 0.2 must win.
	config := &ProviderConfig{
		Name:     "sampling",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "mistral-small-latest"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{
					ID: "mistral-small-latest",
					Sampling: &SamplingParams{
						Temperature: floatPtr(0.2),
						TopP:        floatPtr(0.15),
					},
				},
			},
		},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "mistral-small-latest"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if decoded["temperature"] != 0.2 {
		t.Fatalf("temperature = %v, want configured 0.2 to beat the catalog default", decoded["temperature"])
	}
	if decoded["top_p"] != 0.15 {
		t.Fatalf("top_p = %v, want configured 0.15 to beat the catalog default", decoded["top_p"])
	}
}

// TestBuildChatRequestCatalogStillAppliesWhenUnconfigured confirms that a
// model with a built-in profile and no config sampling still gets the
// profile's recommended parameters — existing behavior is preserved.
func TestBuildChatRequestCatalogStillAppliesWhenUnconfigured(t *testing.T) {
	config := &ProviderConfig{
		Name:     "sampling",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "mistral-small-latest"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "mistral-small-latest"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if decoded["temperature"] != 0.7 {
		t.Fatalf("temperature = %v, want catalog default 0.7 when nothing is configured", decoded["temperature"])
	}
}

// TestBuildChatRequestDoesNotReintroduceCatalogBlockedParams confirms the
// catalog's unsupported-parameter suppression is a hard backend constraint:
// a configured parameter the catalog blocks (temperature/top_p for gpt-5.x)
// must not be re-emitted by the config-sampling pass.
func TestBuildChatRequestDoesNotReintroduceCatalogBlockedParams(t *testing.T) {
	temp := 0.3
	topP := 0.9
	config := &ProviderConfig{
		Name:     "sampling",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "openai/gpt-5"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{
					ID: "openai/gpt-5",
					Sampling: &SamplingParams{
						Temperature: &temp,
						TopP:        &topP,
						Parameters:  map[string]interface{}{"temperature": 0.3},
					},
				},
			},
		},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "openai/gpt-5"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if _, present := decoded["temperature"]; present {
		t.Fatalf("temperature = %v, but the catalog blocks it for gpt-5 and config must not re-introduce it", decoded["temperature"])
	}
	if _, present := decoded["top_p"]; present {
		t.Fatalf("top_p = %v, but the catalog blocks it for gpt-5", decoded["top_p"])
	}
}

// TestBuildChatRequestPerModelSampling is the end-to-end assertion: resolved
// sampling params land in the request body, per-model winning over provider,
// and unknown parameter keys pass straight through.
func TestBuildChatRequestPerModelSampling(t *testing.T) {
	providerTemp := 0.4
	providerTopP := 0.5
	config := &ProviderConfig{
		Name:     "sampling",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{
			Model:       "override-model",
			Temperature: &providerTemp,
			TopP:        &providerTopP,
			Parameters:  map[string]interface{}{"frequency_penalty": 0.1, "provider_only": "keep"},
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{
					ID: "override-model",
					Sampling: &SamplingParams{
						Temperature: floatPtr(0.9),
						TopP:        floatPtr(0.2),
						Parameters:  map[string]interface{}{"frequency_penalty": 0.75, "fresh_knob": true},
					},
				},
			},
		},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "override-model"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	if decoded["temperature"] != 0.9 {
		t.Fatalf("temperature = %v, want per-model 0.9", decoded["temperature"])
	}
	if decoded["top_p"] != 0.2 {
		t.Fatalf("top_p = %v, want per-model 0.2", decoded["top_p"])
	}
	if decoded["frequency_penalty"] != 0.75 {
		t.Fatalf("frequency_penalty = %v, want per-model 0.75", decoded["frequency_penalty"])
	}
	// Provider-only key survives the merge.
	if decoded["provider_only"] != "keep" {
		t.Fatalf("provider_only = %v, want provider-level value preserved", decoded["provider_only"])
	}
	// Unknown per-model key passes through untouched.
	if decoded["fresh_knob"] != true {
		t.Fatalf("fresh_knob = %v, want unknown key passed through", decoded["fresh_knob"])
	}
}

// TestBuildChatRequestProviderSamplingFallback confirms a model with no
// per-model sampling falls back to the provider-level values.
func TestBuildChatRequestProviderSamplingFallback(t *testing.T) {
	providerTemp := 0.33
	config := &ProviderConfig{
		Name:     "sampling",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{
			Model:       "plain-model",
			Temperature: &providerTemp,
			Parameters:  map[string]interface{}{"frequency_penalty": 0.2},
		},
		Models: ModelConfig{DefaultContextLimit: 4096},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "plain-model"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if decoded["temperature"] != 0.33 {
		t.Fatalf("temperature = %v, want provider default 0.33", decoded["temperature"])
	}
	if decoded["frequency_penalty"] != 0.2 {
		t.Fatalf("frequency_penalty = %v, want provider default 0.2", decoded["frequency_penalty"])
	}
	if _, present := decoded["top_p"]; present {
		t.Fatalf("top_p unexpectedly present when unset")
	}
}

// TestBuildChatRequestNoSamplingEmitsNothing confirms that a provider with no
// sampling configured at either level emits none of the sampling fields —
// existing configs keep their exact previous behavior.
func TestBuildChatRequestNoSamplingEmitsNothing(t *testing.T) {
	config := &ProviderConfig{
		Name:     "bare",
		Endpoint: "http://localhost:9/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "bare-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	p := &GenericProvider{config: config}
	p.mu.Lock()
	p.model = "bare-model"
	p.mu.Unlock()

	body, err := p.buildChatRequest([]api.Message{{Role: "user", Content: "hi"}}, nil, "", false, false)
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	for _, key := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty"} {
		if _, present := decoded[key]; present {
			t.Fatalf("%s unexpectedly present in a config with no sampling", key)
		}
	}
}
