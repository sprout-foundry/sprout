package configuration

import (
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

func TestCredentialsResolveProvider_CustomProviderUsesStoredKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	customProvider := CustomProviderConfig{
		Name:           "gateway",
		Endpoint:       "https://example.com/v1",
		EnvVar:         "GATEWAY_API_KEY",
		RequiresAPIKey: true,
	}
	if err := SaveCustomProvider(customProvider); err != nil {
		t.Fatalf("save custom provider: %v", err)
	}

	// Store the key in the credential store instead of APIKeys map
	credentials.ResetStorageBackend()
	if err := credentials.Save(credentials.Store{"gateway": "stored-gateway-key"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := credentials.ResolveProvider("gateway")
	if err != nil {
		t.Fatalf("resolve provider credential: %v", err)
	}
	if resolved.Value != "stored-gateway-key" {
		t.Fatalf("expected stored key, got %q", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected stored source, got %q", resolved.Source)
	}
	if !HasProviderAuth("gateway") {
		t.Fatalf("expected custom provider credential to be available")
	}
}

// --- Tests for credentials.ResolveProvider and HasProviderAuth ---

func TestCredentialsResolveProvider_LocalProviderReturnsNone(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	resolved, err := credentials.ResolveProvider("ollama")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Source != "none" {
		t.Fatalf("expected source %q, got %q", "none", resolved.Source)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Provider != "ollama" {
		t.Fatalf("expected provider %q, got %q", "ollama", resolved.Provider)
	}
}

func TestCredentialsResolveProvider_EnvVarTakesPrecedence(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "env-openai-priority-key")

	// Clear any stored credentials to ensure env var takes precedence
	credentials.ResetStorageBackend()

	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "env-openai-priority-key" {
		t.Fatalf("expected env key %q, got %q", "env-openai-priority-key", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected source %q, got %q", "environment", resolved.Source)
	}
	if resolved.EnvVar != "OPENAI_API_KEY" {
		t.Fatalf("expected env var %q, got %q", "OPENAI_API_KEY", resolved.EnvVar)
	}
}

func TestCredentialsResolveProvider_UsesCredentialStore(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	// Clear any ambient OPENAI_API_KEY so the store is used instead
	t.Setenv("OPENAI_API_KEY", "")

	// Store the key in the credential store
	credentials.ResetStorageBackend()
	if err := credentials.Save(credentials.Store{"openai": "from-credential-store"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "from-credential-store" {
		t.Fatalf("expected store key %q, got %q", "from-credential-store", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected source %q, got %q", "stored", resolved.Source)
	}
}

func TestCredentialsResolveProvider_FallsBackToCredentialStore(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	// Save a key to the credential store
	if err := credentials.Save(credentials.Store{"openai": "file-stored-openai-key"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "file-stored-openai-key" {
		t.Fatalf("expected file-store key %q, got %q", "file-stored-openai-key", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected source %q, got %q", "stored", resolved.Source)
	}
}

func TestCredentialsResolveProvider_NoCredentialAvailable(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	// No key in store
	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error for missing credential: %v", err)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
}

func TestHasProviderAuth_LocalProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	if !HasProviderAuth("lmstudio") {
		t.Fatal("expected HasProviderAuth to return true for local provider lmstudio")
	}
	if !HasProviderAuth("ollama") {
		t.Fatal("expected HasProviderAuth to return true for local provider ollama")
	}
}

func TestHasProviderAuth_WithCredential(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "env-openai-key")

	if !HasProviderAuth("openai") {
		t.Fatal("expected HasProviderAuth to return true when env var is set")
	}
}

// TestGetProviderAuthMetadata_RemoteOnlyProvider verifies the SP-022
// remote-registry path: a provider that isn't embedded in the binary
// (e.g., one freshly published to GitHub Pages at
// /providers/<name>.json) should still have its declared auth.env_var
// and auth.type surfaced by GetProviderAuthMetadata. Pre-fix, the
// function created a throwaway factory that loaded only embedded
// configs, so remote-only providers fell through to the
// "bearer / no env var" default and the user couldn't configure the
// key via an environment variable.
func TestGetProviderAuthMetadata_RemoteOnlyProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	// Restore whatever the package-init wired up (pkg/factory's
	// callback) so we don't poison sibling tests.
	providerConfigLookupMu.RLock()
	prev := providerConfigLookup
	providerConfigLookupMu.RUnlock()
	t.Cleanup(func() { SetProviderConfigLookup(prev) })

	const remoteOnlyName = "totally-new-provider"
	SetProviderConfigLookup(func(name string) (string, string, bool) {
		if name == remoteOnlyName {
			return "TOTALLY_NEW_API_KEY", "bearer", true
		}
		return "", "", false
	})

	got, err := GetProviderAuthMetadata(remoteOnlyName)
	if err != nil {
		t.Fatalf("GetProviderAuthMetadata: %v", err)
	}
	if got.EnvVar != "TOTALLY_NEW_API_KEY" {
		t.Errorf("EnvVar: got %q, want %q", got.EnvVar, "TOTALLY_NEW_API_KEY")
	}
	if got.AuthType != "bearer" {
		t.Errorf("AuthType: got %q, want %q", got.AuthType, "bearer")
	}
	if !got.RequiresAPIKey {
		t.Errorf("RequiresAPIKey: got false, want true")
	}

	// And: when the lookup returns ok=false (provider unknown to the
	// runtime factory too), we still get the default fallback rather
	// than an error.
	got, err = GetProviderAuthMetadata("entirely-unknown-provider")
	if err != nil {
		t.Fatalf("GetProviderAuthMetadata for unknown: %v", err)
	}
	if got.AuthType != "bearer" {
		t.Errorf("default fallback AuthType: got %q, want %q", got.AuthType, "bearer")
	}
}

// TestKnownProviderNames_MergesRuntimeAdditions verifies that
// providers registered only at runtime (e.g. fetched from the
// GitHub Pages registry, NOT in the generated staticProviderNames
// list) appear in the enumeration used by onboarding, the env-var
// credential sweep, and default-provider auto-selection. Static
// entries must keep their generated order at the front; runtime
// additions are appended in sorted order; duplicates are dropped.
func TestKnownProviderNames_MergesRuntimeAdditions(t *testing.T) {
	providerNamesLookupMu.RLock()
	prev := providerNamesLookup
	providerNamesLookupMu.RUnlock()
	t.Cleanup(func() { SetProviderNamesLookup(prev) })

	SetProviderNamesLookup(func() []string {
		// One name that overlaps with the static list (should be
		// deduped) and two that don't (should be appended sorted).
		return []string{"openai", "zeta-remote", "alpha-remote"}
	})

	got := knownProviderNames()

	if len(got) < len(staticProviderNames)+2 {
		t.Fatalf("expected at least %d entries (static + 2 extras), got %d: %v",
			len(staticProviderNames)+2, len(got), got)
	}

	// Static prefix preserved in order.
	for i, want := range staticProviderNames {
		if got[i] != want {
			t.Fatalf("static prefix mismatch at index %d: got %q, want %q (full: %v)",
				i, got[i], want, got)
		}
	}

	extras := got[len(staticProviderNames):]
	wantExtras := []string{"alpha-remote", "zeta-remote"}
	if len(extras) != len(wantExtras) {
		t.Fatalf("extras: got %v, want %v", extras, wantExtras)
	}
	for i, want := range wantExtras {
		if extras[i] != want {
			t.Errorf("extras[%d]: got %q, want %q", i, extras[i], want)
		}
	}

	// "openai" must appear exactly once (came from static, dedup'd
	// from runtime additions).
	openaiCount := 0
	for _, n := range got {
		if n == "openai" {
			openaiCount++
		}
	}
	if openaiCount != 1 {
		t.Errorf("expected openai exactly once, got %d times", openaiCount)
	}
}

// TestKnownProviderNames_NoLookupReturnsStatic covers the path where
// pkg/factory's init hasn't wired the lookup — a narrow unit test
// that imports only pkg/configuration must still get a usable list
// (the compile-time built-ins).
func TestKnownProviderNames_NoLookupReturnsStatic(t *testing.T) {
	providerNamesLookupMu.RLock()
	prev := providerNamesLookup
	providerNamesLookupMu.RUnlock()
	t.Cleanup(func() { SetProviderNamesLookup(prev) })

	SetProviderNamesLookup(nil)

	got := knownProviderNames()
	if len(got) != len(staticProviderNames) {
		t.Fatalf("with no lookup, expected %d entries, got %d", len(staticProviderNames), len(got))
	}
	for i, want := range staticProviderNames {
		if got[i] != want {
			t.Errorf("index %d: got %q, want %q", i, got[i], want)
		}
	}
}

func TestHasProviderAuth_WithoutCredential(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	if HasProviderAuth("openai") {
		t.Fatal("expected HasProviderAuth to return false when no credential available")
	}
}
