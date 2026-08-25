package providers_test

import (
	"context"
	"testing"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

func TestContextLimitResolution(t *testing.T) {
	factory := providers.NewProviderFactory()
	if err := factory.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	cases := []struct {
		provider string
		model    string
		wantMin  int
	}{
		{"deepseek", "deepseek-v4-flash", 1000000},
		{"deepseek", "deepseek-v4-pro", 1000000},
		{"deepseek", "deepseek-chat", 1000000},
		{"deepinfra", "deepseek-ai/DeepSeek-V4-Flash", 1000000},
		{"openrouter", "deepseek/deepseek-v4-flash", 1000000},
	}

	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.provider+"_"+tc.model, func(t *testing.T) {
			pconfig, err := factory.GetProviderConfig(tc.provider)
			if err != nil {
				t.Fatalf("GetProviderConfig(%s): %v", tc.provider, err)
			}

			configLimit := pconfig.GetContextLimit(tc.model)
			t.Logf("GetContextLimit(%s/%s) = %d (%dK)", tc.provider, tc.model, configLimit, configLimit/1000)
			if configLimit < tc.wantMin {
				t.Errorf("GetContextLimit(%q) = %d, want >= %d", tc.model, configLimit, tc.wantMin)
			}

			// Create a real provider instance to test GetModelContextLimit
			client, err := factory.CreateProviderWithModel(tc.provider, tc.model)
			if err != nil {
				t.Fatalf("CreateProviderWithModel(%s, %s): %v", tc.provider, tc.model, err)
			}

			limit, err := client.GetModelContextLimit()
			if err != nil {
				t.Logf("GetModelContextLimit(%s/%s): %v (registry fetch may fail in test)", tc.provider, tc.model, err)
			}
			t.Logf("GetModelContextLimit(%s/%s) = %d (%dK)", tc.provider, tc.model, limit, limit/1000)
			if limit < tc.wantMin {
				t.Errorf("GetModelContextLimit(%s/%s) = %d (%dK), want >= %d (%dK)",
					tc.provider, tc.model, limit, limit/1000, tc.wantMin, tc.wantMin/1000)
			}

			// SP-131: verify warmModelsCache doesn't cause a different
			// context limit than the pre-warm value. Previously, the cache
			// enrichment used model_info.context_length, bypassing
			// model_overrides — producing a different (wrong) value after
			// ListModels() populated the cache.
			_, _ = client.ListModels(ctx)
			limitAfterWarm, _ := client.GetModelContextLimit()
			if limitAfterWarm != limit {
				t.Errorf("context limit changed after warmModelsCache: before=%d, after=%d",
					limit, limitAfterWarm)
			}

			_ = ctx
		})
	}
}
