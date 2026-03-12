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

	cfg1 := m1.GetConfig()
	cfg1.ProviderPriority = []string{"openrouter", "deepinfra"}
	cfg1.ResourceDirectory = "resources-a"
	if err := m1.SaveConfig(); err != nil {
		t.Fatalf("save manager 1 config: %v", err)
	}

	// Stale manager updates one scalar and clears provider priority (deletion/change).
	cfg2 := m2.GetConfig()
	t.Logf("m2 config before: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)
	cfg2.ResourceDirectory = "resources-b"
	cfg2.ProviderPriority = nil
	t.Logf("m2 config after: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)
	if err := m2.SaveConfig(); err != nil {
		t.Fatalf("save manager 2 config: %v", err)
	}

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
