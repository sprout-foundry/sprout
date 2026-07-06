//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestIsKnownProvider_BuiltIn(t *testing.T) {
	// openrouter is a built-in provider — should always be recognized
	if !isKnownProvider("openrouter", nil) {
		t.Error("isKnownProvider(\"openrouter\", nil) = false, want true (built-in provider)")
	}

	if !isKnownProvider("openai", nil) {
		t.Error("isKnownProvider(\"openai\", nil) = false, want true (built-in provider)")
	}

	if !isKnownProvider("deepinfra", nil) {
		t.Error("isKnownProvider(\"deepinfra\", nil) = false, want true (built-in provider)")
	}

	// Case normalization: uppercase input should be treated as known.
	if !isKnownProvider("OpenRouter", nil) {
		t.Error("isKnownProvider(\"OpenRouter\", nil) = false, want true (case normalization)")
	}

	if !isKnownProvider("  OpenAI  ", nil) {
		t.Error("isKnownProvider(\"  OpenAI  \", nil) = false, want true (trim + case normalization)")
	}
}

func TestIsKnownProvider_Typo(t *testing.T) {
	// "openai-typo" is not a built-in and not in custom providers
	// (unless the test config happens to have it, which it shouldn't).
	if isKnownProvider("openai-typo", nil) {
		t.Error("isKnownProvider(\"openai-typo\", nil) = true, want false (typo provider)")
	}

	if isKnownProvider("bogus-provider-xyz", nil) {
		t.Error("isKnownProvider(\"bogus-provider-xyz\", nil) = true, want false")
	}
}

func TestIsKnownProvider_Empty(t *testing.T) {
	if isKnownProvider("", nil) {
		t.Error("isKnownProvider(\"\", nil) = true, want false")
	}
}

func TestIsKnownProvider_CustomProviderInConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)

	cfg := configuration.NewConfig()
	cfg.CustomProviders["my-custom-llm"] = configuration.CustomProviderConfig{
		Endpoint: "https://custom.example.com/v1",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	configPath := filepath.Join(tmpDir, configuration.ConfigFileName)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if !isKnownProvider("my-custom-llm", nil) {
		t.Error("isKnownProvider(\"my-custom-llm\", nil) = false, want true (custom provider in config)")
	}

	// Caller-supplied config path avoids the disk reload.
	if !isKnownProvider("my-custom-llm", cfg) {
		t.Error("isKnownProvider(\"my-custom-llm\", cfg) = false, want true (custom provider via caller-supplied config)")
	}
}
