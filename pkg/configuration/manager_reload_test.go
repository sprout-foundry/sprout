package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManager_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".sprout")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write initial config as JSON (the actual file format)
	initialCfg := map[string]interface{}{
		"last_used_provider": "openai",
		"default_provider":   "openai",
	}
	writeJSON(t, filepath.Join(configDir, "config.json"), initialCfg)
	writeJSON(t, filepath.Join(configDir, "api_keys.json"), map[string]interface{}{})

	// Create manager — will load our initial config
	mgr, err := NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir() failed: %v", err)
	}

	// Verify initial state
	if mgr.GetConfig().LastUsedProvider != "openai" {
		t.Fatalf("expected initial provider 'openai', got %q", mgr.GetConfig().LastUsedProvider)
	}

	// Modify config on disk
	updatedCfg := map[string]interface{}{
		"last_used_provider": "anthropic",
		"default_provider":   "anthropic",
	}
	writeJSON(t, filepath.Join(configDir, "config.json"), updatedCfg)

	// Reload
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	// Verify reloaded state
	if mgr.GetConfig().LastUsedProvider != "anthropic" {
		t.Errorf("expected reloaded provider 'anthropic', got %q", mgr.GetConfig().LastUsedProvider)
	}
}

func TestManager_Reload_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".sprout")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir() failed: %v", err)
	}

	origProvider := mgr.GetConfig().LastUsedProvider

	// Double reload should be safe
	if err := mgr.Reload(); err != nil {
		t.Fatalf("first Reload() failed: %v", err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("second Reload() failed: %v", err)
	}

	if mgr.GetConfig().LastUsedProvider != origProvider {
		t.Errorf("expected provider %q after double reload, got %q", origProvider, mgr.GetConfig().LastUsedProvider)
	}
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
