package providerregistry

import (
	"encoding/json"
	"testing"
)

// Per-model sampling must survive the remote-registry → native conversion so a
// published provider JSON can carry sampling overrides that reach the request
// layer.
func TestRemoteModelInfoSamplingToNative(t *testing.T) {
	raw := []byte(`{
		"name": "sampling-remote",
		"endpoint": "https://api.example.com/v1/chat/completions",
		"auth": {"type": "bearer", "env_var": "SAMPLE_KEY"},
		"defaults": {"model": "m", "temperature": 0.7, "top_p": 1.0},
		"models": {
			"default_context_limit": 4096,
			"model_info": [
				{"id": "with-sampling", "context_length": 4096,
				 "sampling": {"temperature": 0.2, "top_p": 0.85,
				              "parameters": {"frequency_penalty": 0.6}}},
				{"id": "plain", "context_length": 4096}
			]
		}
	}`)

	var r RemoteProviderConfig
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	native := r.ToProviderConfig()
	if native == nil {
		t.Fatal("expected non-nil native config")
	}

	withSampling := native.GetModelInfo("with-sampling")
	if withSampling == nil || withSampling.Sampling == nil {
		t.Fatalf("sampling not carried into native model_info")
	}
	if withSampling.Sampling.Temperature == nil || *withSampling.Sampling.Temperature != 0.2 {
		t.Fatalf("temperature not converted: %v", withSampling.Sampling.Temperature)
	}
	if withSampling.Sampling.TopP == nil || *withSampling.Sampling.TopP != 0.85 {
		t.Fatalf("top_p not converted: %v", withSampling.Sampling.TopP)
	}
	if withSampling.Sampling.Parameters["frequency_penalty"] != 0.6 {
		t.Fatalf("parameters not converted: %#v", withSampling.Sampling.Parameters)
	}

	// Deep-copy isolation: mutating the converted parameters must not touch
	// the source.
	withSampling.Sampling.Parameters["frequency_penalty"] = 0.0
	if r.Models.ModelInfo[0].Sampling.Parameters["frequency_penalty"] != 0.6 {
		t.Fatalf("native conversion shares parameter map with source")
	}

	// A model with no sampling stays nil.
	if plain := native.GetModelInfo("plain"); plain == nil || plain.Sampling != nil {
		t.Fatalf("plain entry gained a sampling field")
	}

	// Resolution on the native config reflects the converted value.
	if got := native.ResolveTemperature("with-sampling"); got == nil || *got != 0.2 {
		t.Fatalf("ResolveTemperature = %v, want 0.2", got)
	}
}
