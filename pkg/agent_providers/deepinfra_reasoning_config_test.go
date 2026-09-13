package providers

import "testing"

// TestDeepInfraEmbeddedConfigReplaysReasoning pins the catalog value: the
// deepinfra config must set reasoning_content_field so historical assistant
// reasoning is replayed on requests. DeepInfra's API accepts the field and
// the model consumes it (verified live against GLM-5.3-Flash: with the
// replayed trace the model correctly recalled a subset it had only derived
// in reasoning; without it, it hallucinated a different answer). The field
// was left empty since the Dec-2025 generic-provider refactor, disabling
// multi-turn reasoning preservation on every DeepInfra session.
func TestDeepInfraEmbeddedConfigReplaysReasoning(t *testing.T) {
	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}
	cfg, err := f.GetProviderConfig("deepinfra")
	if err != nil || cfg == nil {
		t.Fatalf("GetProviderConfig(deepinfra): %v", err)
	}
	if cfg.Conversion.ReasoningContentField != "reasoning_content" {
		t.Errorf("deepinfra reasoning_content_field = %q, want %q",
			cfg.Conversion.ReasoningContentField, "reasoning_content")
	}
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("NewGenericProvider: %v", err)
	}
	if !p.ReplaysReasoningHistory() {
		t.Error("GenericProvider over the deepinfra config must report ReplaysReasoningHistory() = true")
	}
}
