package api

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

func TestResolveCredentialValueReturnsStoredKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	err := credentials.Save(credentials.Store{
		"openai": "stored-openai-key",
	})
	if err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	value, err := resolveCredentialValue("openai", "OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("expected stored key to resolve, got error: %v", err)
	}
	if value != "stored-openai-key" {
		t.Fatalf("expected stored key, got %q", value)
	}
}

func TestResolveCredentialValueReturnsExplicitMissingCredentialError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	value, err := resolveCredentialValue("openai", "OPENAI_API_KEY")
	if err == nil {
		t.Fatalf("expected missing credential error, got value %q", value)
	}
	expected := "OPENAI_API_KEY not set and no stored API key configured"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}
