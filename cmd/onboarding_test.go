//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// writeTestConfig writes a minimal config.json into dir with the given
// LastUsedProvider value. Returns the absolute path to the file.
func writeTestConfig(t *testing.T, dir, lastUsedProvider string) string {
	t.Helper()
	cfg := configuration.NewConfig()
	cfg.LastUsedProvider = lastUsedProvider
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	configPath := filepath.Join(dir, configuration.ConfigFileName)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// setupIsolatedConfig creates a temp config directory, sets the env vars,
// and returns a helper to write a config with the given LastUsedProvider.
// The caller should NOT defer cleanup — the temp dir is managed by t.TempDir().
func setupIsolatedConfig(t *testing.T) func(provider string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	return func(provider string) {
		writeTestConfig(t, tmpDir, provider)
	}
}

func TestNeedsOnboarding_EmptyLastUsedProvider(t *testing.T) {
	writeConfig := setupIsolatedConfig(t)
	writeConfig("")

	if got := needsOnboarding(); !got {
		t.Error("needsOnboarding() = false, want true when LastUsedProvider is empty")
	}
}

func TestNeedsOnboarding_TestSentinel(t *testing.T) {
	writeConfig := setupIsolatedConfig(t)
	writeConfig("test")

	if got := needsOnboarding(); !got {
		t.Error("needsOnboarding() = false, want true when LastUsedProvider is \"test\"")
	}
}

func TestNeedsOnboarding_EditorSentinel(t *testing.T) {
	writeConfig := setupIsolatedConfig(t)
	writeConfig("editor")

	if got := needsOnboarding(); !got {
		t.Error("needsOnboarding() = false, want true when LastUsedProvider is \"editor\"")
	}
}

func TestNeedsOnboarding_KnownProviderNoCreds(t *testing.T) {
	// This is the key regression test: when LastUsedProvider is a known
	// built-in provider (openrouter) but no API key is configured, the
	// old code returned true (triggering onboarding every time). The fix
	// returns false — let agent creation surface the real auth error.
	writeConfig := setupIsolatedConfig(t)
	writeConfig("openrouter")

	// Make sure OPENROUTER_API_KEY is NOT set
	t.Setenv("OPENROUTER_API_KEY", "")

	got := needsOnboarding()
	if got {
		t.Error("needsOnboarding() = true, want false for known provider without creds (should let agent creation surface the real error)")
	}
}

func TestNeedsOnboarding_KnownProviderWithEnvVar(t *testing.T) {
	writeConfig := setupIsolatedConfig(t)
	writeConfig("openrouter")

	// Set the env var so HasProviderAuth would return true even without
	// the isKnownProvider short-circuit — this confirms the happy path.
	t.Setenv("OPENROUTER_API_KEY", "sk-test-key-12345")

	got := needsOnboarding()
	if got {
		t.Error("needsOnboarding() = true, want false when provider has env var set")
	}
}

func TestNeedsOnboarding_UnknownProviderNoCreds(t *testing.T) {
	// "openai-typo" is not a built-in or custom provider, so isKnownProvider
	// returns false and we fall through to HasProviderAuth.
	//
	// HasProviderAuth delegates to credentials.HasProviderCredential which
	// calls getProviderInfo → GetProviderAuthMetadata. For an unknown
	// provider, GetProviderAuthMetadata returns a zero-value ProviderInfo
	// (RequiresAPIKey == false, EnvVar == ""). The credential resolver
	// has a pre-existing passthrough for providers that don't require
	// API keys — it returns true. This means HasProviderAuth(true) →
	// needsOnboarding(false).
	//
	// This is NOT a regression from the isKnownProvider fix — it's a
	// pre-existing behavior in the credential resolution layer. The
	// isKnownProvider guard exists to protect KNOWN providers from
	// transient HasProviderAuth false-negatives; it intentionally does
	// NOT apply to unknown names. A typo'd provider will be caught by
	// the agent-creation path (which validates against KnownProviderNames)
	// or by runKeysSet (which gates on isKnownProvider).
	writeConfig := setupIsolatedConfig(t)
	writeConfig("openai-typo")

	got := needsOnboarding()
	if got {
		t.Error(`needsOnboarding() = true for unknown provider. The isKnownProvider guard must NOT short-circuit unknown names — this would mask provider typos and block the agent on a non-existent provider.`)
	}
}

func TestNeedsOnboarding_UnreadableConfig(t *testing.T) {
	// Point SPROUT_CONFIG at a non-existent directory — configuration.Load()
	// will create it and return a default config (empty LastUsedProvider).
	// But to test the "err != nil" path, we point at a path that can't be
	// read. We use a file path (not a directory) so GetConfigPath() returns
	// a path inside it, and the subsequent os.Stat for config.json fails.
	tmpDir := t.TempDir()
	// Create a file named config.json at the path where GetConfigDir expects
	// a directory — this causes Load to fail when trying to stat the config
	// file path (it will try to read a file inside a file).
	// Actually, the simpler approach: set SPROUT_CONFIG to a path that
	// exists but is a file, not a directory. Then GetConfigDir will try
	// to create subdirectories inside it and fail.
	configFile := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(configFile, []byte("not a dir"), 0600); err != nil {
		t.Fatalf("write decoy file: %v", err)
	}
	t.Setenv("SPROUT_CONFIG", configFile)
	t.Setenv("SPROUT_CONFIG", configFile)

	got := needsOnboarding()
	if !got {
		t.Error("needsOnboarding() = false, want true when config is unreadable")
	}
}

func TestNeedsOnboarding_LocalProviderNoKey(t *testing.T) {
	// ollama-local doesn't require an API key, so HasProviderAuth returns
	// true for it. But it's a known provider, so isKnownProvider short-circuits
	// to false before even checking credentials.
	writeConfig := setupIsolatedConfig(t)
	writeConfig("ollama-local")

	got := needsOnboarding()
	if got {
		t.Error("needsOnboarding() = true, want false for local provider (ollama-local)")
	}
}

func TestNeedsOnboarding_CustomProvider(t *testing.T) {
	// A custom provider defined in config.CustomProviders should be treated
	// as known — isKnownProvider checks cfg.CustomProviders.
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a config with a custom provider entry
	cfg := configuration.NewConfig()
	cfg.LastUsedProvider = "my-custom-provider"
	cfg.CustomProviders["my-custom-provider"] = configuration.CustomProviderConfig{
		Endpoint: "https://custom.example.com/v1",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	configPath := filepath.Join(tmpDir, configuration.ConfigFileName)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := needsOnboarding()
	if got {
		t.Error("needsOnboarding() = true, want false for custom provider defined in config")
	}
}

func TestNeedsOnboarding_DeepinfraNoKey(t *testing.T) {
	// Another known built-in provider — same regression as openrouter.
	writeConfig := setupIsolatedConfig(t)
	writeConfig("deepinfra")

	// Make sure DEEPINFRA_API_KEY is NOT set
	t.Setenv("DEEPINFRA_API_KEY", "")

	got := needsOnboarding()
	if got {
		t.Error("needsOnboarding() = true, want false for known provider (deepinfra) without creds")
	}
}
