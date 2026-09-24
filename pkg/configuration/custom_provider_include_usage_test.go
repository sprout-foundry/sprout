package configuration

import (
	"testing"
)

// The flat custom-provider key include_usage must round-trip through
// normalization into StreamingConfig.IncludeUsage — that flag is what
// makes strict-OpenAI backends (vLLM et al.) stream token counts instead
// of silently reporting zero usage.
func TestCustomProviderIncludeUsageMapsToStreamingConfig(t *testing.T) {
	cfg := CustomProviderConfig{
		Name:         "usage-test",
		Endpoint:     "http://localhost:9/v1/chat/completions",
		ContextSize:  8192,
		IncludeUsage: true,
	}
	pc, err := cfg.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig: %v", err)
	}
	if !pc.Streaming.IncludeUsage {
		t.Fatalf("Streaming.IncludeUsage = false, want true (flat include_usage key not mapped)")
	}

	// Default (unset) stays off — the flag is opt-in per provider.
	cfgOff := CustomProviderConfig{
		Name:        "usage-test-off",
		Endpoint:    "http://localhost:9/v1/chat/completions",
		ContextSize: 8192,
	}
	pcOff, err := cfgOff.ToProviderConfig()
	if err != nil {
		t.Fatalf("ToProviderConfig (off): %v", err)
	}
	if pcOff.Streaming.IncludeUsage {
		t.Fatalf("Streaming.IncludeUsage = true, want false by default")
	}
}
