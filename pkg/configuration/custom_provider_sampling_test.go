package configuration

import (
	"encoding/json"
	"testing"
)

// Per-model sampling in a custom (user) provider config must land in the
// resulting provider config's model_info entries, so the resolution helpers
// pick them up. This is the "user config overrides embedded" channel: a user
// config is loaded after (and replaces) the embedded provider of the same
// name, carrying its own per-model sampling.
func TestCustomProviderPerModelSamplingMapsToModelInfo(t *testing.T) {
	temp := 0.15
	topP := 0.8
	cfg := CustomProviderConfig{
		Name:        "sampling-test",
		Endpoint:    "http://localhost:9/v1/chat/completions",
		ContextSize: 8192,
		ModelSampling: map[string]SamplingOverride{
			"model-a": {
				Temperature: &temp,
				TopP:        &topP,
				Parameters:  map[string]interface{}{"frequency_penalty": 0.5},
			},
		},
	}

	pc, err := cfg.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig: %v", err)
	}

	mi := pc.GetModelInfo("model-a")
	if mi == nil || mi.Sampling == nil {
		t.Fatalf("expected model-a sampling to be carried into model_info")
	}
	if mi.Sampling.Temperature == nil || *mi.Sampling.Temperature != 0.15 {
		t.Fatalf("temperature not carried: %v", mi.Sampling.Temperature)
	}
	if mi.Sampling.TopP == nil || *mi.Sampling.TopP != 0.8 {
		t.Fatalf("top_p not carried: %v", mi.Sampling.TopP)
	}
	if mi.Sampling.Parameters["frequency_penalty"] != 0.5 {
		t.Fatalf("parameters not carried: %#v", mi.Sampling.Parameters)
	}

	// Resolution reflects the carried values.
	if got := pc.ResolveTemperature("model-a"); got == nil || *got != 0.15 {
		t.Fatalf("ResolveTemperature = %v, want 0.15", got)
	}

	// Defaults stay untouched (sampling is per-model only).
	if pc.Defaults.Temperature != nil {
		t.Fatalf("per-model sampling must not leak into provider defaults")
	}
}

// A custom provider with no per-model sampling produces no sampling model_info
// entries — existing behavior is unchanged.
func TestCustomProviderWithoutSamplingProducesNoSamplingEntries(t *testing.T) {
	cfg := CustomProviderConfig{
		Name:        "no-sampling",
		Endpoint:    "http://localhost:9/v1/chat/completions",
		ContextSize: 8192,
	}
	pc, err := cfg.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig: %v", err)
	}
	if pc.ResolveTemperature("anything") != nil {
		t.Fatal("expected no sampling resolution for a config without sampling")
	}
	if pc.ResolveParameters("anything") != nil {
		t.Fatal("expected no parameters for a config without sampling")
	}
	if len(pc.Models.ModelInfo) != 0 {
		t.Fatalf("expected no model_info entries, got %#v", pc.Models.ModelInfo)
	}
}

// An empty model_sampling entry (all fields unset) must not be emitted as a
// model_info entry, so it cannot shadow an embedded entry for the same model.
func TestCustomProviderEmptySamplingEntryNotEmitted(t *testing.T) {
	cfg := CustomProviderConfig{
		Name:          "empty-sampling",
		Endpoint:      "http://localhost:9/v1/chat/completions",
		ContextSize:   8192,
		ModelSampling: map[string]SamplingOverride{"model-a": {}},
	}
	pc, err := cfg.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig: %v", err)
	}
	if len(pc.Models.ModelInfo) != 0 {
		t.Fatalf("empty sampling entry must not be emitted, got %#v", pc.Models.ModelInfo)
	}
}

// Out-of-range sampling values in a custom provider config are rejected, while
// unknown parameter keys are allowed.
func TestCustomProviderSamplingValidation(t *testing.T) {
	bad := 2.5
	_, err := NormalizeCustomProviderConfig(CustomProviderConfig{
		Name:          "bad-sampling",
		Endpoint:      "http://localhost:9/v1/chat/completions",
		ContextSize:   8192,
		ModelSampling: map[string]SamplingOverride{"m": {Temperature: &bad}},
	})
	if err == nil {
		t.Fatal("expected out-of-range temperature to be rejected")
	}

	ok := 1.5
	_, err = NormalizeCustomProviderConfig(CustomProviderConfig{
		Name:        "ok-sampling",
		Endpoint:    "http://localhost:9/v1/chat/completions",
		ContextSize: 8192,
		ModelSampling: map[string]SamplingOverride{
			"m": {Temperature: &ok, Parameters: map[string]interface{}{"some_future_knob": 1}},
		},
	})
	if err != nil {
		t.Fatalf("expected in-range sampling with unknown parameters to be accepted, got %v", err)
	}
}

// The model_sampling block must round-trip through JSON so hand-edited user
// config files are parsed.
func TestCustomProviderModelSamplingJSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"name": "json-sampling",
		"endpoint": "http://localhost:9/v1/chat/completions",
		"context_size": 8192,
		"requires_api_key": false,
		"model_sampling": {
			"model-x": {"temperature": 0.3, "top_p": 0.9,
			             "parameters": {"presence_penalty": 0.4}}
		}
	}`)

	var cfg CustomProviderConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	override, ok := cfg.ModelSampling["model-x"]
	if !ok {
		t.Fatalf("model_sampling did not parse: %#v", cfg.ModelSampling)
	}
	if override.Temperature == nil || *override.Temperature != 0.3 {
		t.Fatalf("temperature not parsed: %v", override.Temperature)
	}
	if override.Parameters["presence_penalty"] != 0.4 {
		t.Fatalf("parameters not parsed: %#v", override.Parameters)
	}

	pc, err := cfg.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig: %v", err)
	}
	if got := pc.ResolveTopP("model-x"); got == nil || *got != 0.9 {
		t.Fatalf("ResolveTopP = %v, want 0.9", got)
	}
	params := pc.ResolveParameters("model-x")
	if params["presence_penalty"] != 0.4 {
		t.Fatalf("resolved parameters missing presence_penalty: %#v", params)
	}
}
