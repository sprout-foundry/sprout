package providers_test

import (
	"testing"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// GEMFIX-3: embedded OpenRouter and DeepInfra catalogs must resolve current
// Gemini models to their real context limits via model_overrides, ahead of
// pattern_overrides and the rounded model_info values. Stale gemini-1.5 and
// gemini-2.0-flash-exp entries have been removed.
func TestGeminiContextLimitsFromEmbeddedConfigs(t *testing.T) {
	factory := providers.NewProviderFactory()
	if err := factory.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	cases := []struct {
		provider string
		model    string
		want     int
	}{
		{"openrouter", "google/gemini-2.5-flash", 1048576},
		{"openrouter", "google/gemini-2.5-pro", 1048576},
		{"openrouter", "google/gemini-2.5-flash-lite", 1048576},
		{"openrouter", "google/gemini-3-flash-preview", 1048576},
		{"openrouter", "google/gemini-3.1-pro-preview", 1048576},
		{"openrouter", "google/gemini-3.5-flash", 1048576},
		{"openrouter", "google/gemini-3.6-flash", 1048576},
		{"openrouter", "google/gemini-3.7-flash", 1048576},
		// Image variants have smaller real limits and must not inherit the
		// 1,048,576 gemini-.* pattern override.
		{"openrouter", "google/gemini-2.5-flash-image", 32768},
		{"openrouter", "google/gemini-3-pro-image", 131072},
		{"openrouter", "google/gemini-3.1-flash-image", 131072},

		{"deepinfra", "google/gemini-2.5-flash", 1048576},
		{"deepinfra", "google/gemini-2.5-pro", 1048576},
		{"deepinfra", "google/gemini-3.1-flash-lite", 1048576},
		{"deepinfra", "google/gemini-3.1-pro", 1048576},
		{"deepinfra", "google/gemini-3.5-flash", 1048576},
		{"deepinfra", "google/gemini-3.7-flash", 1048576},
		{"deepinfra", "google/gemini-3-pro-image", 131072},
	}

	for _, tc := range cases {
		t.Run(tc.provider+"_"+tc.model, func(t *testing.T) {
			pconfig, err := factory.GetProviderConfig(tc.provider)
			if err != nil {
				t.Fatalf("GetProviderConfig(%s): %v", tc.provider, err)
			}
			got := pconfig.GetContextLimit(tc.model)
			if got != tc.want {
				t.Errorf("GetContextLimit(%s, %q) = %d, want exactly %d (default_context_limit is %d)",
					tc.provider, tc.model, got, tc.want, pconfig.Models.DefaultContextLimit)
			}
		})
	}
}

func TestStaleGeminiOverridesRemoved(t *testing.T) {
	factory := providers.NewProviderFactory()
	if err := factory.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	stale := []string{
		"google/gemini-1.5-flash",
		"google/gemini-1.5-pro",
		"google/gemini-2.0-flash-exp",
	}

	for _, provider := range []string{"openrouter", "deepinfra"} {
		pconfig, err := factory.GetProviderConfig(provider)
		if err != nil {
			t.Fatalf("GetProviderConfig(%s): %v", provider, err)
		}
		for _, model := range stale {
			if _, exists := pconfig.Models.ModelOverrides[model]; exists {
				t.Errorf("%s model_overrides still contains stale entry %q", provider, model)
			}
			for _, mi := range pconfig.Models.ModelInfo {
				if mi.ID == model {
					t.Errorf("%s model_info still contains stale entry %q", provider, model)
				}
			}
		}
	}
}
