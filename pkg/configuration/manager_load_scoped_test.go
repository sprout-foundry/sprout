package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigWithLayers_GlobalCustomProviderVisibleFromScopedSession
// reproduces the bug where a global custom provider was invisible when
// SPROUT_CONFIG pointed at a scoped dir with no provider of the same name.
//
// Setup: a global HOME dir has providers/myglobal.json; a separate scoped
// config dir (SPROUT_CONFIG) has no providers. After LoadConfigWithLayers,
// the global provider must appear in the result's CustomProviders map so
// that MapProviderStringToClientType can resolve it at startup.
func TestLoadConfigWithLayers_GlobalCustomProviderVisibleFromScopedSession(t *testing.T) {
	// Global home dir with a custom provider.
	globalHome := t.TempDir()
	globalProvidersDir := filepath.Join(globalHome, ".config", "sprout", "providers")
	if err := os.MkdirAll(globalProvidersDir, 0700); err != nil {
		t.Fatalf("mkdir global providers: %v", err)
	}
	globalProvider := CustomProviderConfig{
		Name:           "myglobal",
		Endpoint:       "https://api.example.com/v1/chat/completions",
		EnvVar:         "MYGLOBAL_API_KEY",
		RequiresAPIKey: true,
		ModelName:      "global-model",
	}
	globalJSON, _ := json.Marshal(globalProvider)
	if err := os.WriteFile(filepath.Join(globalProvidersDir, "myglobal.json"), globalJSON, 0644); err != nil {
		t.Fatalf("write global provider: %v", err)
	}

	// Scoped config dir (SPROUT_CONFIG) with NO providers.
	scopedDir := t.TempDir()
	scopedConfigPath := filepath.Join(scopedDir, ConfigFileName)
	// Write a minimal config so LoadConfigWithLayers reads it.
	if err := os.WriteFile(scopedConfigPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write scoped config: %v", err)
	}

	// Point HOME at the global home, XDG_CONFIG_HOME unset.
	t.Setenv("HOME", globalHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", scopedDir)

	// Load through the layered path (this is what the config manager uses).
	cfg, err := LoadConfigWithLayers(scopedConfigPath, "", "", scopedDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}

	// The global custom provider must be visible.
	if _, ok := cfg.CustomProviders["myglobal"]; !ok {
		t.Fatalf("global custom provider 'myglobal' not found after layered load. CustomProviders: %v", cfg.CustomProviders)
	}

	// And MapProviderStringToClientType must resolve it (this is the
	// resolution path that previously failed with "unsupported provider").
	ct, err := MapProviderStringToClientType(cfg, "myglobal")
	if err != nil {
		t.Fatalf("MapProviderStringToClientType failed for global provider: %v", err)
	}
	if string(ct) != "myglobal" {
		t.Fatalf("expected client type 'myglobal', got %q", ct)
	}
}

// TestLoadConfigWithLayers_ScopedOverridesGlobal proves that a scoped
// custom provider overrides a global one with the same name.
func TestLoadConfigWithLayers_ScopedOverridesGlobal(t *testing.T) {
	globalHome := t.TempDir()
	globalProvidersDir := filepath.Join(globalHome, ".config", "sprout", "providers")
	os.MkdirAll(globalProvidersDir, 0700)

	// Global provider points at endpoint-A.
	globalJSON := `{"name":"shared","endpoint":"https://endpoint-a.example.com/v1/chat/completions","env_var":"SHARED_KEY","requires_api_key":true,"defaults":{"model":"model-a"}}`
	os.WriteFile(filepath.Join(globalProvidersDir, "shared.json"), []byte(globalJSON), 0644)

	// Scoped dir with a provider of the SAME name, different endpoint.
	scopedDir := t.TempDir()
	scopedProvidersDir := filepath.Join(scopedDir, "providers")
	os.MkdirAll(scopedProvidersDir, 0700)
	scopedJSON := `{"name":"shared","endpoint":"https://endpoint-b.example.com/v1/chat/completions","env_var":"SHARED_KEY","requires_api_key":true,"defaults":{"model":"model-b"}}`
	os.WriteFile(filepath.Join(scopedProvidersDir, "shared.json"), []byte(scopedJSON), 0644)

	scopedConfigPath := filepath.Join(scopedDir, ConfigFileName)
	os.WriteFile(scopedConfigPath, []byte(`{}`), 0644)

	t.Setenv("HOME", globalHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", scopedDir)

	cfg, err := LoadConfigWithLayers(scopedConfigPath, "", "", scopedDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}

	provider, ok := cfg.CustomProviders["shared"]
	if !ok {
		t.Fatal("provider 'shared' not found")
	}
	// The scoped override must win.
	if provider.Endpoint != "https://endpoint-b.example.com/v1/chat/completions" {
		t.Fatalf("expected scoped endpoint (endpoint-b), got %q", provider.Endpoint)
	}
}
