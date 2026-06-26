package providers

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Known embedded provider names, derived from the files in configs/*.json.
// ---------------------------------------------------------------------------
var knownEmbeddedProviders = []string{
	"openai",
	"openrouter",
	"deepseek",
	"cerebras",
	"mistral",
	"chutes",
	"deepinfra",
	"ollama-cloud",
	"lmstudio",
	"minimax",
	"zai",
	"zai-coding",
}

// ---------------------------------------------------------------------------
// TestEmbeddedOnly — verify every embedded config loads and has required fields
// ---------------------------------------------------------------------------

func TestEmbeddedOnly(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	providers := f.GetAvailableProviders()

	// Every known provider must be present
	for _, name := range knownEmbeddedProviders {
		if !slices.Contains(providers, name) {
			t.Errorf("expected provider %q in available list %v", name, providers)
		}
	}

	// For each provider, verify the loaded config has required fields
	for _, name := range knownEmbeddedProviders {
		cfg, err := f.GetProviderConfig(name)
		if err != nil {
			t.Errorf("GetProviderConfig(%q): %v", name, err)
			continue
		}

		if cfg.Name != name {
			t.Errorf("%q: config.Name = %q, want %q", name, cfg.Name, name)
		}
		if cfg.Endpoint == "" {
			t.Errorf("%q: Endpoint is empty", name)
		}
		if cfg.Auth.Type == "" {
			t.Errorf("%q: Auth.Type is empty", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRemoteConfigMergeOverEmbedded — upsert overwrites embedded config in-place
// ---------------------------------------------------------------------------

func TestRemoteConfigMergeOverEmbedded(t *testing.T) {
	t.Parallel()

	const overrideEndpoint = "https://remote-override.example.com/v1/chat/completions"
	const overrideModel = "remote-model-override"

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// Record the original "openai" config values
	orig, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai): %v", err)
	}

	if orig.Endpoint == overrideEndpoint {
		t.Fatalf("original openai endpoint already equals override value (coincidental)")
	}
	if orig.Defaults.Model == overrideModel {
		t.Fatalf("original openai model already equals override value (coincidental)")
	}

	// Upsert a modified "openai" config
	overrideCfg := &ProviderConfig{
		Name:     "openai",
		Endpoint: overrideEndpoint,
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: overrideModel},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	}
	if err := f.UpsertConfig("openai", overrideCfg); err != nil {
		t.Fatalf("UpsertConfig(openai): %v", err)
	}

	// Fetch the config again and verify the override took effect
	updated, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai) after upsert: %v", err)
	}

	if updated.Endpoint != overrideEndpoint {
		t.Errorf("Endpoint = %q, want %q", updated.Endpoint, overrideEndpoint)
	}
	if updated.Defaults.Model != overrideModel {
		t.Errorf("Defaults.Model = %q, want %q", updated.Defaults.Model, overrideModel)
	}

	// Provider count must stay the same — no new providers, just an update
	providers := f.GetAvailableProviders()
	if len(providers) != len(knownEmbeddedProviders) {
		t.Errorf("expected %d providers after merge, got %d", len(knownEmbeddedProviders), len(providers))
	}
}

// ---------------------------------------------------------------------------
// TestRemoteOnlyProvider — upserting a brand-new provider adds it
// ---------------------------------------------------------------------------

func TestRemoteOnlyProvider(t *testing.T) {
	t.Parallel()

	const remoteName = "remote-only-test"
	const remoteEndpoint = "https://remote-only.example.com/v1/chat/completions"
	const remoteModel = "remote-only-model"

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// Baseline: exactly the known embedded providers
	initialCount := len(f.GetAvailableProviders())
	if initialCount != len(knownEmbeddedProviders) {
		t.Fatalf("expected %d embedded providers, got %d", len(knownEmbeddedProviders), initialCount)
	}

	// Ensure the remote name doesn't already exist
	if slices.Contains(f.GetAvailableProviders(), remoteName) {
		t.Fatalf("provider %q already exists in embedded configs", remoteName)
	}

	// Upsert the new provider
	remoteCfg := &ProviderConfig{
		Name:     remoteName,
		Endpoint: remoteEndpoint,
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: remoteModel},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	}
	if err := f.UpsertConfig(remoteName, remoteCfg); err != nil {
		t.Fatalf("UpsertConfig(%s): %v", remoteName, err)
	}

	// Count should be embedded + 1
	providers := f.GetAvailableProviders()
	expectedCount := len(knownEmbeddedProviders) + 1
	if len(providers) != expectedCount {
		t.Errorf("expected %d providers, got %d", expectedCount, len(providers))
	}

	// The remote-only provider must appear in the list
	if !slices.Contains(providers, remoteName) {
		t.Errorf("provider %q not found in available list %v", remoteName, providers)
	}

	// Fetch it and verify fields
	cfg, err := f.GetProviderConfig(remoteName)
	if err != nil {
		t.Fatalf("GetProviderConfig(%s): %v", remoteName, err)
	}

	if cfg.Name != remoteName {
		t.Errorf("config.Name = %q, want %q", cfg.Name, remoteName)
	}
	if cfg.Endpoint != remoteEndpoint {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, remoteEndpoint)
	}
	if cfg.Defaults.Model != remoteModel {
		t.Errorf("Defaults.Model = %q, want %q", cfg.Defaults.Model, remoteModel)
	}
	if cfg.Auth.Type != "bearer" {
		t.Errorf("Auth.Type = %q, want %q", cfg.Auth.Type, "bearer")
	}
	if cfg.Models.DefaultContextLimit != 8192 {
		t.Errorf("Models.DefaultContextLimit = %d, want 8192", cfg.Models.DefaultContextLimit)
	}
}

// ---------------------------------------------------------------------------
// TestEmbeddedConfigFileCountMatchesList — dynamic check that knownEmbeddedProviders
// is in sync with the actual embedded config files on disk.
// ---------------------------------------------------------------------------

func TestEmbeddedConfigFileCountMatchesList(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	providers := f.GetAvailableProviders()

	// The number of loaded configs must equal the number of names in knownEmbeddedProviders
	// If they differ, it means a config file was added/removed without updating the list.
	if len(providers) != len(knownEmbeddedProviders) {
		t.Errorf("knownEmbeddedProviders has %d entries but %d configs were loaded from embedded files — list is out of sync",
			len(knownEmbeddedProviders), len(providers))
	}

	// Cross-check: every loaded config name must appear in the known list (and vice versa)
	for _, name := range providers {
		if !slices.Contains(knownEmbeddedProviders, name) {
			t.Errorf("provider %q loaded from embedded files but not in knownEmbeddedProviders list", name)
		}
	}
	for _, name := range knownEmbeddedProviders {
		if !slices.Contains(providers, name) {
			t.Errorf("provider %q in knownEmbeddedProviders but not loaded from embedded files", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestUpsertConfigInvalid — invalid configs must be rejected
// ---------------------------------------------------------------------------

func TestUpsertConfigInvalid(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	tests := []struct {
		name            string
		cfg             *ProviderConfig
		wantErrContains string
	}{
		{
			name:            "nil_config_returns_nil",
			cfg:             nil,
			wantErrContains: "", // nil config is a no-op, returns nil
		},
		{
			name:            "empty_config_overwritten_by_key",
			cfg:             &ProviderConfig{Endpoint: "https://example.com/v1", Auth: AuthConfig{Type: "bearer"}, Models: ModelConfig{DefaultContextLimit: 8192}},
			wantErrContains: "", // UpsertConfig overwrites cfg.Name with the key, so this succeeds
		},
		{
			name:            "empty_endpoint",
			cfg:             &ProviderConfig{Auth: AuthConfig{Type: "bearer"}, Models: ModelConfig{DefaultContextLimit: 8192}},
			wantErrContains: "invalid provider config",
		},
		{
			name:            "empty_auth_type",
			cfg:             &ProviderConfig{Endpoint: "https://example.com/v1", Models: ModelConfig{DefaultContextLimit: 8192}},
			wantErrContains: "invalid provider config",
		},
		{
			name:            "zero_context_limits",
			cfg:             &ProviderConfig{Endpoint: "https://example.com/v1", Auth: AuthConfig{Type: "bearer"}, Models: ModelConfig{}},
			wantErrContains: "invalid provider config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.UpsertConfig(tt.name, tt.cfg)
			if tt.wantErrContains == "" {
				if err != nil {
					t.Errorf("expected nil error for nil config, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErrContains)
				} else if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrContains, err.Error())
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestUpsertConfigDeepCopy — external mutations don't affect stored state
// ---------------------------------------------------------------------------

func TestUpsertConfigDeepCopy(t *testing.T) {
	t.Parallel()

	const name = "deepcopy-test"
	const initialEndpoint = "https://initial.example.com/v1"

	f := NewProviderFactory()
	cfg := &ProviderConfig{
		Name:     name,
		Endpoint: initialEndpoint,
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "model-a"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}
	if err := f.UpsertConfig(name, cfg); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	// Mutate the original config pointer after upsert
	cfg.Endpoint = "https://mutated.example.com/v1"
	cfg.Defaults.Model = "model-mutated"

	// Verify the stored config is unaffected
	stored, err := f.GetProviderConfig(name)
	if err != nil {
		t.Fatalf("GetProviderConfig: %v", err)
	}
	if stored.Endpoint != initialEndpoint {
		t.Errorf("Endpoint was mutated externally: got %q, want %q", stored.Endpoint, initialEndpoint)
	}
	if stored.Defaults.Model != "model-a" {
		t.Errorf("Defaults.Model was mutated externally: got %q, want %q", stored.Defaults.Model, "model-a")
	}
}

// ---------------------------------------------------------------------------
// TestGetProviderConfigReturnsCopy — mutating the returned config doesn't affect the factory
// ---------------------------------------------------------------------------

func TestGetProviderConfigReturnsCopy(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// Get a copy and mutate it
	cfg, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai): %v", err)
	}
	originalEndpoint := cfg.Endpoint
	cfg.Endpoint = "https://mutated.example.com/v1"

	// Get it again — should still be the original value
	cfg2, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai) after mutation: %v", err)
	}
	if cfg2.Endpoint != originalEndpoint {
		t.Errorf("factory's stored config was affected by external mutation: got %q, want %q", cfg2.Endpoint, originalEndpoint)
	}
}

// ---------------------------------------------------------------------------
// TestUpsertConfigNameMismatch — config.Name is overwritten to match the key
// ---------------------------------------------------------------------------

func TestUpsertConfigNameMismatch(t *testing.T) {
	t.Parallel()

	const keyName = "name-mismatch-test"
	const configName = "wrong-name-in-config"

	f := NewProviderFactory()

	cfg := &ProviderConfig{
		Name:     configName, // Intentionally different from keyName
		Endpoint: "https://example.com/v1",
		Auth:     AuthConfig{Type: "none"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}
	if err := f.UpsertConfig(keyName, cfg); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	// Verify it's stored under the key name, not the config's Name field
	cfg2, err := f.GetProviderConfig(keyName)
	if err != nil {
		t.Fatalf("GetProviderConfig(%s): %v", keyName, err)
	}
	if cfg2.Name != keyName {
		t.Errorf("config.Name = %q, want %q (should be overwritten to match the key)", cfg2.Name, keyName)
	}
}

// ---------------------------------------------------------------------------
// TestGetRegistryAfterUpsert — GetRegistry reflects upserted providers
// ---------------------------------------------------------------------------

func TestGetRegistryAfterUpsert(t *testing.T) {
	t.Parallel()

	const remoteName = "registry-remote-test"

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// Upsert a new provider
	cfg := &ProviderConfig{
		Name:     remoteName,
		Endpoint: "https://registry-test.example.com/v1",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "registry-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}
	if err := f.UpsertConfig(remoteName, cfg); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	// Verify the registry contains the new provider
	reg := f.GetRegistry()
	if _, exists := reg.ProviderConfigs[remoteName]; !exists {
		t.Errorf("provider %q not found in registry.ProviderConfigs", remoteName)
	}
	// Verify the registry config matches
	if reg.ProviderConfigs[remoteName].Endpoint != cfg.Endpoint {
		t.Errorf("registry endpoint = %q, want %q", reg.ProviderConfigs[remoteName].Endpoint, cfg.Endpoint)
	}

	// Mutation isolation: mutating the returned registry must not affect the factory
	mutated := reg.ProviderConfigs[remoteName]
	mutated.Endpoint = "https://mutated.example.com/v1"
	reg.ProviderConfigs[remoteName] = mutated

	reg2 := f.GetRegistry()
	if reg2.ProviderConfigs[remoteName].Endpoint == "https://mutated.example.com/v1" {
		t.Error("factory registry was affected by external mutation of GetRegistry() result")
	}
}

// ---------------------------------------------------------------------------
// TestReloadConfigViaUpsert — simulates a "remote reload" by replacing an
// embedded provider config via UpsertConfig and verifying the replacement.
// ---------------------------------------------------------------------------

func TestReloadConfigViaUpsert(t *testing.T) {
	t.Parallel()

	const reloadEndpoint = "https://remote-upsert.example.com/v1"
	const reloadModel = "reloaded-model"

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// Record the original "openai" config
	orig, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai): %v", err)
	}

	// Upsert a new "openai" config to simulate a reload
	reloadCfg := &ProviderConfig{
		Name:     "openai",
		Endpoint: reloadEndpoint,
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: reloadModel},
		Models:   ModelConfig{DefaultContextLimit: 16384},
	}
	if err := f.UpsertConfig("openai", reloadCfg); err != nil {
		t.Fatalf("UpsertConfig(openai): %v", err)
	}

	// Verify the new values are present
	updated, err := f.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(openai) after upsert: %v", err)
	}
	if updated.Endpoint != reloadEndpoint {
		t.Errorf("Endpoint = %q, want %q", updated.Endpoint, reloadEndpoint)
	}
	if updated.Defaults.Model != reloadModel {
		t.Errorf("Defaults.Model = %q, want %q", updated.Defaults.Model, reloadModel)
	}
	if updated.Models.DefaultContextLimit != 16384 {
		t.Errorf("Models.DefaultContextLimit = %d, want 16384", updated.Models.DefaultContextLimit)
	}

	// Old embedded values should be gone
	if updated.Endpoint == orig.Endpoint {
		t.Error("old embedded endpoint still present after upsert")
	}

	// Provider count must stay the same — replacement, not addition
	providers := f.GetAvailableProviders()
	if len(providers) != len(knownEmbeddedProviders) {
		t.Errorf("expected %d providers after upsert replacement, got %d", len(knownEmbeddedProviders), len(providers))
	}
}

// ---------------------------------------------------------------------------
// TestGetProviderConfigNotFound — error for non-existent provider
// ---------------------------------------------------------------------------

func TestGetProviderConfigNotFound(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()

	_, err := f.GetProviderConfig("nonexistent-provider")
	if err == nil {
		t.Error("expected error for non-existent provider, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-provider") {
		t.Errorf("error should mention provider name: got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestValidateProvider — model validation for available models list
// ---------------------------------------------------------------------------

func TestValidateProvider(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	tests := []struct {
		name        string
		provider    string
		model       string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid_provider_no_models_list",
			provider: "openai",
			model:    "any-model", // openai has no AvailableModels, so any model is valid
			wantErr:  false,
		},
		{
			name:        "nonexistent_provider",
			provider:    "nonexistent",
			model:       "any-model",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "model_not_in_available_list",
			provider:    "validate-only-test",
			model:       "model-c",
			wantErr:     true,
			errContains: "not available",
		},
		{
			name:     "model_in_available_list",
			provider: "validate-only-test",
			model:    "model-a",
			wantErr:  false,
		},
	}

	// Upsert a provider with an explicit AvailableModels list for the "model_not_in_available_list" subtest
	validateCfg := &ProviderConfig{
		Name:     "validate-only-test",
		Endpoint: "https://validate-test.example.com/v1",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "model-a"},
		Models:   ModelConfig{DefaultContextLimit: 4096, AvailableModels: []string{"model-a", "model-b"}},
	}
	if err := f.UpsertConfig("validate-only-test", validateCfg); err != nil {
		t.Fatalf("UpsertConfig(validate-only-test): %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.ValidateProvider(tt.provider, tt.model)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestListProvidersWithModels — verifies model lists for each provider
// ---------------------------------------------------------------------------

func TestListProvidersWithModels(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	result := f.ListProvidersWithModels()

	// Every embedded provider should appear in the result
	for _, name := range knownEmbeddedProviders {
		models, exists := result[name]
		if !exists {
			t.Errorf("provider %q not found in ListProvidersWithModels result", name)
		}
		if len(models) == 0 {
			t.Errorf("provider %q has no models in result", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestGetDefaultProvider — returns a provider name when configs are loaded
// ---------------------------------------------------------------------------

func TestGetDefaultProvider(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()

	// No configs loaded — should return empty
	if name := f.GetDefaultProvider(); name != "" {
		t.Errorf("expected empty default provider, got %q", name)
	}

	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// After loading, should return a non-empty name
	name := f.GetDefaultProvider()
	if name == "" {
		t.Error("expected non-empty default provider after loading configs")
	}
	if !slices.Contains(f.GetAvailableProviders(), name) {
		t.Errorf("default provider %q not in available list %v", name, f.GetAvailableProviders())
	}
}

// ---------------------------------------------------------------------------
// TestConcurrentAccess — concurrent upserts and reads must not panic
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 types of operations per goroutine

	for i := 0; i < goroutines; i++ {
		name := fmt.Sprintf("concurrent-%d", i)

		// Concurrent UpsertConfig writes
		go func() {
			defer wg.Done()
			cfg := &ProviderConfig{
				Name:     name,
				Endpoint: "https://concurrent.example.com/v1",
				Auth:     AuthConfig{Type: "none"},
				Defaults: RequestDefaults{Model: "concurrent-model"},
				Models:   ModelConfig{DefaultContextLimit: 4096},
			}
			_ = f.UpsertConfig(name, cfg)
		}()

		// Concurrent GetProviderConfig reads
		go func() {
			defer wg.Done()
			_, _ = f.GetProviderConfig("openai")
		}()

		// Concurrent GetAvailableProviders reads
		go func() {
			defer wg.Done()
			_ = f.GetAvailableProviders()
		}()
	}

	wg.Wait()

	// Verify all concurrent providers were added
	providers := f.GetAvailableProviders()
	if len(providers) != len(knownEmbeddedProviders)+goroutines {
		t.Errorf("expected %d providers after concurrent upserts, got %d",
			len(knownEmbeddedProviders)+goroutines, len(providers))
	}
}

// ---------------------------------------------------------------------------
// TestCreateProviderSmoke — smoke test that CreateProvider and
// CreateProviderWithModel work against embedded configs.
// ---------------------------------------------------------------------------

func TestCreateProviderSmoke(t *testing.T) {
	t.Parallel()

	f := NewProviderFactory()
	if err := f.LoadEmbeddedConfigs(); err != nil {
		t.Fatalf("LoadEmbeddedConfigs: %v", err)
	}

	// CreateProvider returns a non-nil provider without error
	provider, err := f.CreateProvider("openai")
	if err != nil {
		t.Fatalf("CreateProvider(openai): %v", err)
	}
	if provider == nil {
		t.Fatal("CreateProvider(openai) returned nil")
	}

	// CreateProviderWithModel returns a non-nil provider without error
	providerWithModel, err := f.CreateProviderWithModel("openai", "gpt-4")
	if err != nil {
		t.Fatalf("CreateProviderWithModel(openai, gpt-4): %v", err)
	}
	if providerWithModel == nil {
		t.Fatal("CreateProviderWithModel(openai, gpt-4) returned nil")
	}
}
