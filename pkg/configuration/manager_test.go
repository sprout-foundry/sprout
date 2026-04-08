package configuration

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/credentials"
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

// TestManager_RefreshAPIKeys tests that RefreshAPIKeys updates the in-memory cache
func TestManager_RefreshAPIKeys(t *testing.T) {
	// Set up a test environment
	t.Setenv("CI", "1")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Create a manager
	m, err := NewManager()
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Use a key that won't collide with anything already in the backend
	testKey := "test-key-refresh-" + t.Name()

	// Set the key directly in the backend
	if err := credentials.SetToActiveBackend("test", testKey); err != nil {
		t.Fatalf("failed to set test key: %v", err)
	}

	// Verify the backend has the new key
	backendValue, _, err := credentials.GetFromActiveBackend("test")
	if err != nil {
		t.Fatalf("failed to get backend key: %v", err)
	}
	if backendValue != testKey {
		t.Fatalf("backend key mismatch: expected %q, got %q", testKey, backendValue)
	}

	// Manager's in-memory cache may or may not have the key yet.
	// Set a DIFFERENT key in the backend to prove RefreshAPIKeys picks up the change.
	differentKey := testKey + "-updated"
	if err := credentials.SetToActiveBackend("test", differentKey); err != nil {
		t.Fatalf("failed to set different key: %v", err)
	}

	// Refresh the API keys
	if err := m.RefreshAPIKeys(); err != nil {
		t.Fatalf("RefreshAPIKeys failed: %v", err)
	}

	// Verify Manager's in-memory cache now has the latest key from the backend
	managerValue := m.GetAPIKeyForProvider(api.TestClientType)
	if managerValue != differentKey {
		t.Errorf("Manager cache not refreshed: expected %q, got %q", differentKey, managerValue)
	}

	// Clean up
	_ = credentials.DeleteFromActiveBackend("test")
}
