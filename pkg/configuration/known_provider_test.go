package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookupKnownProvider_CustomProvider(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	// Write a custom provider config to the manager's temp providers dir.
	providersDir, err := GetProvidersDir()
	if err != nil {
		t.Fatalf("GetProvidersDir: %v", err)
	}
	cfg := `{
  "name": "unit-test-fixture-prov",
  "endpoint": "http://192.168.1.134:8033/v1/chat/completions",
  "model_name": "qwen3.6-27b",
  "context_size": 200000,
  "requires_api_key": true,
  "env_var": "UNIT_TEST_FIXTURE_API_KEY"
}`
	if err := os.WriteFile(filepath.Join(providersDir, "unit-test-fixture-prov.json"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write provider: %v", err)
	}

	info, ok := LookupKnownProvider("unit-test-fixture-prov")
	if !ok {
		t.Fatal("expected LookupKnownProvider to find the custom provider")
	}
	if info.Source != "custom" {
		t.Errorf("Source = %q, want %q", info.Source, "custom")
	}
	if info.Name != "unit-test-fixture-prov" {
		t.Errorf("Name = %q, want %q", info.Name, "unit-test-fixture-prov")
	}
	if info.EnvVar != "UNIT_TEST_FIXTURE_API_KEY" {
		t.Errorf("EnvVar = %q, want %q", info.EnvVar, "UNIT_TEST_FIXTURE_API_KEY")
	}
	if !info.RequiresAPIKey {
		t.Error("RequiresAPIKey = false, want true")
	}
	if info.DefaultModel != "qwen3.6-27b" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "qwen3.6-27b")
	}
	if info.ContextSize != 200000 {
		t.Errorf("ContextSize = %d, want %d", info.ContextSize, 200000)
	}
}

func TestLookupKnownProvider_CanonicalizesName(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	providersDir, err := GetProvidersDir()
	if err != nil {
		t.Fatalf("GetProvidersDir: %v", err)
	}
	cfg := `{
  "name": "unit-test-canon-prov",
  "endpoint": "http://example.com/v1",
  "env_var": "UNIT_TEST_CANON_API_KEY",
  "requires_api_key": true
}`
	if err := os.WriteFile(filepath.Join(providersDir, "unit-test-canon-prov.json"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write provider: %v", err)
	}

	// Mixed case should still resolve to the lowercase stored name.
	info, ok := LookupKnownProvider("Unit-Test-Canon-Prov")
	if !ok {
		t.Fatal("expected LookupKnownProvider to canonicalize mixed-case input")
	}
	if info.Name != "unit-test-canon-prov" {
		t.Errorf("Name = %q, want %q (canonicalized)", info.Name, "unit-test-canon-prov")
	}
}

func TestLookupKnownProvider_UnknownName(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	_, ok := LookupKnownProvider("nonexistent-provider-xyz-12345")
	if ok {
		t.Error("expected LookupKnownProvider to return ok=false for unknown provider")
	}
}

func TestLookupKnownProvider_InvalidName(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	// Names with invalid characters (uppercase, spaces) should fail
	// canonicalization and return ok=false.
	_, ok := LookupKnownProvider("not valid")
	if ok {
		t.Error("expected LookupKnownProvider to reject names with invalid characters")
	}
}

func TestLookupKnownProvider_EmptyName(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	_, ok := LookupKnownProvider("")
	if ok {
		t.Error("expected LookupKnownProvider to reject empty name")
	}

	_, ok = LookupKnownProvider("   ")
	if ok {
		t.Error("expected LookupKnownProvider to reject whitespace-only name")
	}
}

func TestLookupKnownProvider_NoAuthRequired(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	providersDir, err := GetProvidersDir()
	if err != nil {
		t.Fatalf("GetProvidersDir: %v", err)
	}
	cfg := `{
  "name": "my-local",
  "endpoint": "http://localhost:11434/v1",
  "env_var": "",
  "requires_api_key": false
}`
	if err := os.WriteFile(filepath.Join(providersDir, "my-local.json"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write provider: %v", err)
	}

	info, ok := LookupKnownProvider("my-local")
	if !ok {
		t.Fatal("expected LookupKnownProvider to find no-auth custom provider")
	}
	if info.RequiresAPIKey {
		t.Error("RequiresAPIKey = true, want false (no env var configured)")
	}
	if info.EnvVar != "" {
		t.Errorf("EnvVar = %q, want empty", info.EnvVar)
	}
}
func TestLookupKnownProvider_FactorySource(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	// Use a name that exists in pkg/agent_providers/configs/ but
	// is NOT present as a custom provider config in the temp
	// providers dir. With no custom file written, LookupKnownProvider
	// falls through to the embedded factory and reports source="factory".
	//
	// We don't want a custom file because the custom-source branch is
	// already covered by TestLookupKnownProvider_CustomProvider. The
	// factory-source branch is what fires for skill-installed and
	// remote-published providers — the common follow-up to a
	// "Run 'sprout custom add <name>'" hint.
	info, ok := LookupKnownProvider("minimax")
	if !ok {
		t.Skip("minimax not in embedded factory (running outside source tree?)")
	}
	if info.Source != "factory" {
		t.Errorf("Source = %q, want %q (no custom config was written, so it must come from the embedded factory)",
			info.Source, "factory")
	}
	if info.Name != "minimax" {
		t.Errorf("Name = %q, want %q", info.Name, "minimax")
	}
	if info.EnvVar == "" {
		t.Error("EnvVar = \"\", want non-empty (embedded minimax.json declares MINIMAX_API_KEY)")
	}
	if !info.RequiresAPIKey {
		t.Error("RequiresAPIKey = false, want true (minimax uses bearer auth)")
	}
	// Display name comes from the static GetProviderDisplayName map
	// when the factory doesn't expose a display_name. Just make sure
	// it's non-empty and human-readable.
	if info.DisplayName == "" {
		t.Error("DisplayName = \"\", want non-empty")
	}
}

func TestLookupKnownProvider_FactorySource_NoConfig(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	_ = mgr

	// A name that is canonical (lowercase, valid chars) but doesn't match
	// any custom config and isn't in the embedded factory either. This
	// proves the factory-source branch only fires when the factory
	// actually has the provider — typos don't accidentally fall into
	// the same code path as legitimate skill-installed providers.
	_, ok := LookupKnownProvider("definitely-not-a-real-provider-xyz-9999")
	if ok {
		t.Error("expected LookupKnownProvider to return ok=false for a provider that has neither custom nor factory config")
	}
}
