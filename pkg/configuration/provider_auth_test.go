package configuration

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

func TestResolveProviderAuth_CustomProviderUsesStoredKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

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

	resolved, err := ResolveProviderAuth("gateway")
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

// --- Tests for ResolveProviderAuth and HasProviderAuth ---

func TestResolveProviderAuth_LocalProviderReturnsNone(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	resolved, err := ResolveProviderAuth("ollama")
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

func TestResolveProviderAuth_EnvVarTakesPrecedence(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "env-openai-priority-key")

	// Clear any stored credentials to ensure env var takes precedence
	credentials.ResetStorageBackend()

	resolved, err := ResolveProviderAuth("openai")
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

func TestResolveProviderAuth_UsesCredentialStore(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	// Clear any ambient OPENAI_API_KEY so the store is used instead
	t.Setenv("OPENAI_API_KEY", "")

	// Store the key in the credential store
	credentials.ResetStorageBackend()
	if err := credentials.Save(credentials.Store{"openai": "from-credential-store"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := ResolveProviderAuth("openai")
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

func TestResolveProviderAuth_FallsBackToCredentialStore(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	// Save a key to the credential store
	if err := credentials.Save(credentials.Store{"openai": "file-stored-openai-key"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := ResolveProviderAuth("openai")
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

func TestResolveProviderAuth_NoCredentialAvailable(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	// No key in store
	resolved, err := ResolveProviderAuth("openai")
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
	t.Setenv("OPENAI_API_KEY", "env-openai-key")

	if !HasProviderAuth("openai") {
		t.Fatal("expected HasProviderAuth to return true when env var is set")
	}
}

func TestHasProviderAuth_WithoutCredential(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// Ensure no env var is set
	os.Unsetenv("OPENAI_API_KEY")

	if HasProviderAuth("openai") {
		t.Fatal("expected HasProviderAuth to return false when no credential available")
	}
}
