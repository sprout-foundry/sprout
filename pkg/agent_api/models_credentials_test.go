package api

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

func TestResolveProviderReturnsStoredKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	err := credentials.Save(credentials.Store{
		"openai": "stored-openai-key",
	})
	if err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("expected stored key to resolve, got error: %v", err)
	}
	if resolved.Value != "stored-openai-key" {
		t.Fatalf("expected stored key, got %q", resolved.Value)
	}
}

func TestResolveProviderReturnsEmptyWithMissingCredential(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	resolved, err := credentials.ResolveProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.EnvVar != "OPENAI_API_KEY" {
		t.Fatalf("expected env var OPENAI_API_KEY, got %q", resolved.EnvVar)
	}
}
