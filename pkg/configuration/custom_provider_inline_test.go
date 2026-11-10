package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeProviderSeed writes a global config.json carrying an inline
// custom_providers definition — the layout a template or a hand-written
// config produces (sprout's own Save strips the key).
func writeProviderSeed(t *testing.T, globalDir string) {
	t.Helper()
	if err := os.MkdirAll(globalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"last_used_provider": "llm-bridge",
		"custom_providers": map[string]any{
			"llm-bridge": map[string]any{
				"name":                    "llm-bridge",
				"endpoint":                "http://127.0.0.1:8080/v1",
				"model_name":              "qwen3.8-27b",
				"context_size":            32768,
				"disable_model_discovery": true,
			},
		},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, ConfigFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// isolateEnv points every config root at the fixture's home and the given
// workspace config directory.
func isolateEnv(t *testing.T, home, configDir string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG_DIR", configDir)
}

// A provider defined inline in the global config.json must be visible to a run
// whose active config directory is a workspace layer. That run's own config
// never carries custom_providers (Save strips it), and the provider files live
// under the global dir, so without reading the inline block the provider is
// unresolvable and a non-interactive run fails with "unsupported provider".
//
// This is the warm-VM failure from docs/106 (firecracker-lab): the cold run
// writes .sprout/config.json into the workspace, the next run on that VM takes
// the isolated path, and its agent exits at once.
func TestIsolatedConfigSeesInlineGlobalProvider(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	writeProviderSeed(t, filepath.Join(home, ".config", "sprout"))

	isolated := filepath.Join(root, "workspace", ConfigDirName)
	if err := os.MkdirAll(isolated, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := map[string]any{
		"version":            ConfigVersion,
		"last_used_provider": "llm-bridge",
		"provider_models":    map[string]string{"llm-bridge": "deepseek-ai/DeepSeek-V4.1-Flash-1017"},
	}
	wsData, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(isolated, ConfigFileName), wsData, 0o600); err != nil {
		t.Fatal(err)
	}
	isolateEnv(t, home, isolated)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.CustomProviders["llm-bridge"]; !ok {
		t.Fatalf("isolated Load lost the global provider definition: %v", cfg.CustomProviders)
	}
	if _, err := MapProviderStringToClientType(cfg, "llm-bridge"); err != nil {
		t.Fatalf("MapProviderStringToClientType: %v", err)
	}
	// The metadata lookup the non-interactive startup path uses.
	md, err := GetProviderAuthMetadata("llm-bridge")
	if err != nil {
		t.Fatalf("GetProviderAuthMetadata: %v", err)
	}
	if md.RequiresAPIKey {
		t.Fatalf("provider needs no key, but metadata says it does: %+v", md)
	}
}

// The same provider must also resolve through the layered manager path, which
// is what the CLI uses when it auto-detects a repository.
func TestLayeredManagerSeesInlineGlobalProvider(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	globalDir := filepath.Join(home, ".config", "sprout")
	writeProviderSeed(t, globalDir)

	workspaceDir := filepath.Join(root, "workspace", ConfigDirName)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wsData, err := json.MarshalIndent(map[string]any{
		"version":            ConfigVersion,
		"last_used_provider": "llm-bridge",
		"provider_models":    map[string]string{"llm-bridge": "workspace-model"},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, WorkspaceConfigFileName), wsData, 0o600); err != nil {
		t.Fatal(err)
	}
	isolateEnv(t, home, workspaceDir)

	m, err := NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		t.Fatalf("NewManagerWithLayers: %v", err)
	}
	clientType, model, err := m.ResolveProviderModel("", "")
	if err != nil {
		t.Fatalf("ResolveProviderModel: %v", err)
	}
	if string(clientType) != "llm-bridge" {
		t.Fatalf("provider = %q, want llm-bridge", clientType)
	}
	if model != "workspace-model" {
		t.Fatalf("model = %q, want the workspace layer's provider_models value", model)
	}
}

// A provider file wins over an inline definition of the same name, so a user
// who moved a provider into the file-per-provider format keeps the file's
// values.
func TestProviderFileBeatsInlineDefinition(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	globalDir := filepath.Join(home, ".config", "sprout")
	writeProviderSeed(t, globalDir)

	providersDir := filepath.Join(globalDir, ProvidersDirName)
	if err := os.MkdirAll(providersDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fileProvider := CustomProviderConfig{
		Name:        "llm-bridge",
		Endpoint:    "http://127.0.0.1:9999/v1",
		ModelName:   "file-model",
		ContextSize: 4096,
	}
	data, err := json.Marshal(fileProvider)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "llm-bridge.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	isolateEnv(t, home, globalDir)

	providers, err := LoadCustomProviders()
	if err != nil {
		t.Fatalf("LoadCustomProviders: %v", err)
	}
	got, ok := providers["llm-bridge"]
	if !ok {
		t.Fatal("provider missing")
	}
	if got.ModelName != "file-model" {
		t.Fatalf("model = %q, want the provider file's (the file wins on a name conflict)", got.ModelName)
	}
}

// A config sprout wrote itself carries no custom_providers; loading providers
// from it must stay a no-op rather than an error.
func TestLoadCustomProvidersWithoutInlineBlock(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	globalDir := filepath.Join(home, ".config", "sprout")
	if err := os.MkdirAll(globalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, ConfigFileName),
		[]byte(`{"last_used_provider":"deepseek"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	isolateEnv(t, home, globalDir)

	providers, err := LoadCustomProviders()
	if err != nil {
		t.Fatalf("LoadCustomProviders: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("providers = %v, want none", providers)
	}
}
