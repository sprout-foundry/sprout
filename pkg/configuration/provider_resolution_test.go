package configuration

import (
	"testing"
)

func TestResolveProviderModel_ExplicitProviderAndModel(t *testing.T) {
	t.Setenv("LEDIT_PROVIDER", "")
	t.Setenv("LEDIT_MODEL", "")
	cfg := NewConfig()
	clientType, model, err := ResolveProviderModel(cfg, "minimax", "MiniMax-M2.5")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if clientType != "minimax" {
		t.Fatalf("expected provider minimax, got %s", clientType)
	}
	if model != "MiniMax-M2.5" {
		t.Fatalf("expected model MiniMax-M2.5, got %q", model)
	}
}

func TestResolveProviderModel_ModelSpecifierUsesProviderPrefix(t *testing.T) {
	t.Setenv("LEDIT_PROVIDER", "")
	t.Setenv("LEDIT_MODEL", "")
	cfg := NewConfig()
	clientType, model, err := ResolveProviderModel(cfg, "", "openrouter:qwen/qwen3.5-flash")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if clientType != "openrouter" {
		t.Fatalf("expected provider openrouter, got %s", clientType)
	}
	if model != "qwen/qwen3.5-flash" {
		t.Fatalf("expected stripped model, got %q", model)
	}
}

func TestResolveProviderModel_ModelWithColonNotProviderKeepsModel(t *testing.T) {
	t.Setenv("LEDIT_PROVIDER", "")
	t.Setenv("LEDIT_MODEL", "")
	cfg := NewConfig()
	cfg.LastUsedProvider = "ollama-local"

	clientType, model, err := ResolveProviderModel(cfg, "", "qwen3:30b")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if clientType != "ollama-local" {
		t.Fatalf("expected provider ollama-local, got %s", clientType)
	}
	if model != "qwen3:30b" {
		t.Fatalf("expected model qwen3:30b, got %q", model)
	}
}
