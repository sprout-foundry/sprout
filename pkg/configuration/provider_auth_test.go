package configuration

import "testing"

func TestResolveProviderCredentialPrefersEnvironment(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "env-openai-key")

	keys := APIKeys{
		"openai": "stored-openai-key",
	}

	resolved, err := ResolveProviderCredential("openai", &keys)
	if err != nil {
		t.Fatalf("resolve provider credential: %v", err)
	}
	if resolved.Value != "env-openai-key" {
		t.Fatalf("expected environment key, got %q", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected environment source, got %q", resolved.Source)
	}
}

func TestResolveProviderCredentialUsesStoredCustomProviderKey(t *testing.T) {
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

	keys := APIKeys{
		"gateway": "stored-gateway-key",
	}

	resolved, err := ResolveProviderCredential("gateway", &keys)
	if err != nil {
		t.Fatalf("resolve provider credential: %v", err)
	}
	if resolved.Value != "stored-gateway-key" {
		t.Fatalf("expected stored key, got %q", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected stored source, got %q", resolved.Source)
	}
	if !HasProviderCredential("gateway", &keys) {
		t.Fatalf("expected custom provider credential to be available")
	}
}
