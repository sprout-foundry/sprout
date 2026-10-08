package providers

import (
	"encoding/json"
	"testing"
)

// floatPtr / sampling fixture helpers keep the tests terse.

func floatPtr(v float64) *float64 { return &v }

// TestResolveTemperaturePrecedence pins the temperature precedence chain:
// per-model sampling beats the provider-level default, which beats "unset"
// (nil — no value emitted).
func TestResolveTemperaturePrecedence(t *testing.T) {
	providerDefault := 0.4
	config := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "m", Temperature: &providerDefault},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{ID: "override-model", Sampling: &SamplingParams{Temperature: floatPtr(0.9)}},
			},
		},
	}

	// Per-model sampling wins.
	if got := config.ResolveTemperature("override-model"); got == nil || *got != 0.9 {
		t.Fatalf("override-model: expected 0.9, got %v", got)
	}
	// Unknown model falls back to the provider default.
	if got := config.ResolveTemperature("other-model"); got == nil || *got != 0.4 {
		t.Fatalf("other-model: expected provider default 0.4, got %v", got)
	}

	// No provider default → nil (nothing emitted).
	noDefault := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "m"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}
	if got := noDefault.ResolveTemperature("anything"); got != nil {
		t.Fatalf("expected nil temperature when nothing is configured, got %v", *got)
	}
}

// TestResolveTopPPrecedence mirrors the temperature precedence for top_p.
func TestResolveTopPPrecedence(t *testing.T) {
	providerDefault := 0.5
	config := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "m", TopP: &providerDefault},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{ID: "override-model", Sampling: &SamplingParams{TopP: floatPtr(0.95)}},
			},
		},
	}

	if got := config.ResolveTopP("override-model"); got == nil || *got != 0.95 {
		t.Fatalf("override-model: expected 0.95, got %v", got)
	}
	if got := config.ResolveTopP("other-model"); got == nil || *got != 0.5 {
		t.Fatalf("other-model: expected provider default 0.5, got %v", got)
	}
}

// TestResolveTemperatureZeroIsMeaningful guards the pointer semantics: an
// explicit per-model temperature of 0 must not be treated as "unset".
func TestResolveTemperatureZeroIsMeaningful(t *testing.T) {
	providerDefault := 0.7
	config := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "m", Temperature: &providerDefault},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{ID: "deterministic", Sampling: &SamplingParams{Temperature: floatPtr(0)}},
			},
		},
	}
	got := config.ResolveTemperature("deterministic")
	if got == nil {
		t.Fatal("explicit zero temperature must resolve to a non-nil pointer")
	}
	if *got != 0 {
		t.Fatalf("expected 0, got %v", *got)
	}
}

// TestResolveParametersMergeOverrides verifies per-model parameters merge over
// (and win over) provider-level parameters, with provider-only keys surviving.
func TestResolveParametersMergeOverrides(t *testing.T) {
	config := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{
			Model:      "m",
			Parameters: map[string]interface{}{"frequency_penalty": 0.1, "top_k": 40},
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			ModelInfo: []ModelInfo{
				{
					ID: "override-model",
					Sampling: &SamplingParams{
						Parameters: map[string]interface{}{"frequency_penalty": 0.8},
					},
				},
			},
		},
	}

	merged := config.ResolveParameters("override-model")
	if merged["frequency_penalty"] != 0.8 {
		t.Fatalf("expected per-model frequency_penalty 0.8 to win, got %v", merged["frequency_penalty"])
	}
	if merged["top_k"] != 40 {
		t.Fatalf("expected provider-only top_k to survive merge, got %v", merged["top_k"])
	}

	// Unknown model → provider-level parameters only.
	base := config.ResolveParameters("unknown-model")
	if base["frequency_penalty"] != 0.1 || base["top_k"] != 40 {
		t.Fatalf("unexpected provider-level parameters for unknown model: %#v", base)
	}

	// Nothing configured → nil.
	empty := &ProviderConfig{
		Name:     "t",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "m"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}
	if got := empty.ResolveParameters("any"); got != nil {
		t.Fatalf("expected nil parameters when nothing is configured, got %#v", got)
	}
}

// TestSamplingParamsJSONRoundTrip verifies the per-model sampling shape
// round-trips through JSON and that existing configs without it stay nil.
func TestSamplingParamsJSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name": "t",
		"endpoint": "https://example.com",
		"auth": {"type": "bearer"},
		"defaults": {"model": "m"},
		"models": {
			"default_context_limit": 4096,
			"model_info": [
				{"id": "with-sampling", "context_length": 4096,
				 "sampling": {"temperature": 0.2, "top_p": 0.8,
				              "parameters": {"frequency_penalty": 0.3}}},
				{"id": "no-sampling", "context_length": 4096}
			]
		}
	}`)

	var cfg ProviderConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	withSampling := cfg.GetModelInfo("with-sampling")
	if withSampling == nil || withSampling.Sampling == nil {
		t.Fatalf("expected sampling to parse for with-sampling")
	}
	if withSampling.Sampling.Temperature == nil || *withSampling.Sampling.Temperature != 0.2 {
		t.Fatalf("unexpected temperature: %v", withSampling.Sampling.Temperature)
	}
	if withSampling.Sampling.Parameters["frequency_penalty"] != 0.3 {
		t.Fatalf("unexpected frequency_penalty: %v", withSampling.Sampling.Parameters["frequency_penalty"])
	}
	if noSampling := cfg.GetModelInfo("no-sampling"); noSampling == nil || noSampling.Sampling != nil {
		t.Fatalf("expected nil sampling for no-sampling entry")
	}

	// Re-marshal and confirm omitempty drops the field for the plain entry.
	encoded, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reDecoded ProviderConfig
	if err := json.Unmarshal(encoded, &reDecoded); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if reDecoded.GetModelInfo("with-sampling").Sampling == nil {
		t.Fatalf("sampling did not survive round trip")
	}
	if reDecoded.GetModelInfo("no-sampling").Sampling != nil {
		t.Fatalf("plain entry gained a sampling field")
	}
}

// TestValidateSamplingRanges rejects out-of-range temperature/top_p while
// allowing arbitrary parameter keys.
func TestValidateSamplingRanges(t *testing.T) {
	base := func(s *SamplingParams) *ProviderConfig {
		return &ProviderConfig{
			Name:     "t",
			Endpoint: "https://example.com",
			Auth:     AuthConfig{Type: "bearer"},
			Defaults: RequestDefaults{Model: "m"},
			Models: ModelConfig{
				DefaultContextLimit: 4096,
				ModelInfo:           []ModelInfo{{ID: "m", Sampling: s}},
			},
		}
	}

	if err := base(&SamplingParams{Temperature: floatPtr(2.5)}).Validate(); err == nil {
		t.Fatal("expected temperature 2.5 to be rejected")
	}
	if err := base(&SamplingParams{Temperature: floatPtr(-0.1)}).Validate(); err == nil {
		t.Fatal("expected negative temperature to be rejected")
	}
	if err := base(&SamplingParams{TopP: floatPtr(1.5)}).Validate(); err == nil {
		t.Fatal("expected top_p 1.5 to be rejected")
	}
	if err := base(&SamplingParams{TopP: floatPtr(-0.1)}).Validate(); err == nil {
		t.Fatal("expected negative top_p to be rejected")
	}
	// Boundary values and arbitrary parameter keys are accepted.
	if err := base(&SamplingParams{
		Temperature: floatPtr(2),
		TopP:        floatPtr(0),
		Parameters:  map[string]interface{}{"some_new_knob": 123},
	}).Validate(); err != nil {
		t.Fatalf("expected boundary sampling values to validate, got %v", err)
	}
}
