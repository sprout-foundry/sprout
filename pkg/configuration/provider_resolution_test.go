package configuration

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestResolveProviderModel_ExplicitProviderAndModel(t *testing.T) {
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
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
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
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
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
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

// --- SP-034-fix: provider/model mapping round-trip tests ---

// TestMapProviderStringToClientType_RoundTripFromProviderID verifies the
// round-trip fix for Bug 1: after the websocket handler stores the
// provider ID (e.g. "ollama-local") on the chat session, restoring it
// through MapProviderStringToClientType must yield the same ClientType.
//
// Before the fix, handlers stored the display name "Ollama (Local)"
// (via api.GetProviderName), which lowercased to "ollama (local)" and
// didn't match any known provider, breaking per-session provider scoping.
func TestMapProviderStringToClientType_RoundTripFromProviderID(t *testing.T) {
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
	cfg := NewConfig()

	// Exercise every built-in ClientType to ensure the round-trip is universal.
	// We loop over all of them and verify stored = GetProviderType() restores cleanly.
	cts := api.BuiltInClientTypes()
	if len(cts) == 0 {
		t.Fatal("BuiltInClientTypes returned empty list")
	}

	for _, original := range cts {
		// The string the websocket handler would store (post Bug 1 fix).
		stored := string(original)

		got, err := MapProviderStringToClientType(cfg, stored)
		if err != nil {
			t.Errorf("MapProviderStringToClientType(%q) returned error: %v", stored, err)
			continue
		}
		if got != original {
			t.Errorf("round-trip mismatch for %q: stored=%q got=%q (want %q)",
				original, stored, got, original)
		}
	}
}

// TestMapProviderStringToClientType_AcceptsDisplayNames verifies the
// backward-compatibility fix for Bug 5: callers (or stale persisted
// sessions from before the fix) that hand us a display name still get
// back the correct ClientType.
//
// Note: this is a deliberate fallback. New code paths should always
// pass the provider ID, but old session data may still carry display names.
func TestMapProviderStringToClientType_AcceptsDisplayNames(t *testing.T) {
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
	cfg := NewConfig()

	// Spot-check the most common display-name collisions from the Bug 1
	// regression report. The display name list isn't enumerable without
	// duplicating GetProviderName's table, so we cover the most impactful
	// cases explicitly.
	cases := []struct {
		displayName string
		want        api.ClientType
	}{
		{"OpenAI", api.OpenAIClientType},
		{"OpenRouter (Recommended)", api.OpenRouterClientType},
		{"Ollama (Local)", api.OllamaLocalClientType},
		{"Ollama (Cloud)", api.OllamaCloudClientType},
		{"DeepInfra", api.DeepInfraClientType},
		{"Mistral", api.MistralClientType},
		{"MiniMax", api.MinimaxClientType},
		{"Editor Mode", api.EditorClientType},
	}

	for _, tc := range cases {
		got, err := MapProviderStringToClientType(cfg, tc.displayName)
		if err != nil {
			t.Errorf("display name %q: unexpected error: %v", tc.displayName, err)
			continue
		}
		if got != tc.want {
			t.Errorf("display name %q: got %q, want %q", tc.displayName, got, tc.want)
		}
	}
}

// TestMapProviderStringToClientType_DisplayNameCaseInsensitive verifies
// that arbitrary casing of display names still resolves — real-world
// inputs may have been lowercased, uppercased, or mixed.
func TestMapProviderStringToClientType_DisplayNameCaseInsensitive(t *testing.T) {
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
	cfg := NewConfig()

	cases := []struct {
		input string
		want  api.ClientType
	}{
		{"ollama (local)", api.OllamaLocalClientType},
		{"OLLAMA (LOCAL)", api.OllamaLocalClientType},
		{"Ollama (local)", api.OllamaLocalClientType},
		{"openai", api.OpenAIClientType}, // also matches primary ID switch
		{"OPENAI", api.OpenAIClientType},
	}

	for _, tc := range cases {
		got, err := MapProviderStringToClientType(cfg, tc.input)
		if err != nil {
			t.Errorf("input %q: unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("input %q: got %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestMapProviderStringToClientType_UnknownDisplayNameReturnsError
// ensures the fallback doesn't accidentally accept arbitrary strings.
func TestMapProviderStringToClientType_UnknownDisplayNameReturnsError(t *testing.T) {
	t.Setenv("SPROUT_PROVIDER", "")
	t.Setenv("SPROUT_MODEL", "")
	cfg := NewConfig()

	cases := []string{
		"",
		"   ",
		"Definitely Not A Provider",
		"Some Random LLM",
	}
	for _, input := range cases {
		_, err := MapProviderStringToClientType(cfg, input)
		if err == nil {
			t.Errorf("input %q: expected error, got nil", input)
		}
	}
}

// TestBuildDisplayNameToClientType_AllBuiltinsCovered verifies the
// reverse map covers every built-in ClientType (the "ollama" alias
// excluded by design — see comment in buildDisplayNameToClientType).
func TestBuildDisplayNameToClientType_AllBuiltinsCovered(t *testing.T) {
	m := buildDisplayNameToClientType()
	if len(m) == 0 {
		t.Fatal("display name map is empty")
	}
	for _, ct := range api.BuiltInClientTypes() {
		if ct == api.OllamaClientType {
			continue // alias — handled by primary switch
		}
		name := strings.ToLower(strings.TrimSpace(api.GetProviderName(ct)))
		if name == "" {
			t.Errorf("ClientType %q has empty display name", ct)
			continue
		}
		got, ok := m[name]
		if !ok {
			t.Errorf("ClientType %q (display %q) missing from reverse map", ct, name)
			continue
		}
		if got != ct {
			t.Errorf("ClientType %q (display %q) maps to %q in reverse map, want %q",
				ct, name, got, ct)
		}
	}
}
