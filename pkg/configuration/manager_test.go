package configuration

import (
	"testing"
)

func TestSaveConfig_AppliesDeletionAndScalarUpdates(t *testing.T) {
	// Set CI mode and HOME before creating managers
	t.Setenv("CI", "1")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	m1, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 1: %v", err)
	}

	m2, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}

	if err := m1.UpdateConfig(func(cfg *Config) error {
		cfg.ProviderPriority = []string{"openrouter", "deepinfra"}
		cfg.ResourceDirectory = "resources-a"
		return nil
	}); err != nil {
		t.Fatalf("save manager 1 config: %v", err)
	}

	// Stale manager updates one scalar and clears provider priority (deletion/change).
	cfg2 := m2.GetConfig()
	t.Logf("m2 config before: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)
	if err := m2.UpdateConfig(func(cfg *Config) error {
		cfg.ResourceDirectory = "resources-b"
		cfg.ProviderPriority = nil
		return nil
	}); err != nil {
		t.Fatalf("save manager 2 config: %v", err)
	}
	cfg2 = m2.GetConfig()
	t.Logf("m2 config after: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("reload merged config: %v", err)
	}
	t.Logf("loaded config: ResourceDirectory=%q, ProviderPriority=%v", loaded.ResourceDirectory, loaded.ProviderPriority)
	if loaded.ResourceDirectory != "resources-b" {
		t.Fatalf("expected latest scalar from manager2, got %q", loaded.ResourceDirectory)
	}
	if len(loaded.ProviderPriority) != 0 {
		t.Fatalf("expected provider_priority to be cleared, got %#v", loaded.ProviderPriority)
	}
}
