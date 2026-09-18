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

func TestManager_Reload_LayeredManager_PreservesGlobalLayer(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	globalDir := filepath.Join(isolatedHome, ".config", "sprout")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(globalDir, "config.json"), map[string]interface{}{
		"last_used_provider": "openai",
		"reasoning_effort":   "low",
	})

	workspaceRoot := t.TempDir()
	workspaceDir := filepath.Join(workspaceRoot, ".sprout")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(workspaceDir, "workspace.json"), map[string]interface{}{
		"reasoning_effort": "high",
	})

	mgr, err := NewManagerWithLayers(globalDir, workspaceDir)
	if err != nil {
		t.Fatalf("NewManagerWithLayers() failed: %v", err)
	}

	// Merged view before reload: workspace reasoning_effort wins, global provider visible.
	if got := mgr.GetConfig().ReasoningEffort; got != "high" {
		t.Fatalf("expected merged reasoning_effort %q before reload, got %q", "high", got)
	}
	if got := mgr.GetConfig().LastUsedProvider; got != "openai" {
		t.Fatalf("expected global provider %q before reload, got %q", "openai", got)
	}

	// External edit to the workspace layer (what PUT ?layer=workspace does).
	writeJSON(t, filepath.Join(workspaceDir, "workspace.json"), map[string]interface{}{
		"reasoning_effort": "medium",
	})

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	if got := mgr.GetConfig().ReasoningEffort; got != "medium" {
		t.Errorf("expected reloaded reasoning_effort %q, got %q", "medium", got)
	}
	// The bug this pins: reloading from the workspace save file alone dropped
	// the global layer, so last_used_provider vanished from memory.
	if got := mgr.GetConfig().LastUsedProvider; got != "openai" {
		t.Errorf("expected global provider %q to survive layered reload, got %q", "openai", got)
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
