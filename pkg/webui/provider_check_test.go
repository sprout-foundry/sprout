//go:build !js

package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsProviderAvailable(t *testing.T) {
	t.Run("returns false when provider is editor", func(t *testing.T) {
		// Create isolated config with LastUsedProvider = "editor"
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		cfgData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "editor",
		})
		if err := os.WriteFile(cfgPath, cfgData, 0644); err != nil {
			t.Fatal(err)
		}

		// Point configuration to our temp dir
		t.Setenv("SPROUT_CONFIG", dir)

		result := isProviderAvailable()
		if result {
			t.Error("expected false when provider is editor")
		}
	})

	t.Run("returns true with empty provider", func(t *testing.T) {
		// Create isolated config with empty LastUsedProvider
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		cfgData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "",
		})
		if err := os.WriteFile(cfgPath, cfgData, 0644); err != nil {
			t.Fatal(err)
		}

		t.Setenv("SPROUT_CONFIG", dir)

		result := isProviderAvailable()
		if !result {
			t.Error("expected true for empty provider")
		}
	})

	t.Run("global editor does not gate project folder with real provider", func(t *testing.T) {
		// Global says "editor"; project folder has its own provider. The chat
		// gate must honor the project folder layer — agents resolve providers
		// through the layered (global+project folder) config, so a global
		// "editor" must not block a project folder that configures a provider.
		globalDir := t.TempDir()
		cfgData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "editor",
		})
		if err := os.WriteFile(filepath.Join(globalDir, "config.json"), cfgData, 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SPROUT_CONFIG", globalDir)

		projectFolder := t.TempDir()
		wsDir := filepath.Join(projectFolder, ".sprout")
		if err := os.MkdirAll(wsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		wsData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "openrouter",
		})
		if err := os.WriteFile(filepath.Join(wsDir, "workspace.json"), wsData, 0644); err != nil {
			t.Fatal(err)
		}

		if !isProviderAvailableInProjectFolder(projectFolder) {
			t.Error("expected true when project folder layer configures a real provider")
		}
		if isProviderAvailableInProjectFolder("") {
			t.Error("expected false for global-only check when global says editor")
		}
	})

	t.Run("project folder editor gates that project folder only", func(t *testing.T) {
		globalDir := t.TempDir()
		cfgData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "openrouter",
		})
		if err := os.WriteFile(filepath.Join(globalDir, "config.json"), cfgData, 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SPROUT_CONFIG", globalDir)

		projectFolder := t.TempDir()
		wsDir := filepath.Join(projectFolder, ".sprout")
		if err := os.MkdirAll(wsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		wsData, _ := json.Marshal(map[string]interface{}{
			"last_used_provider": "editor",
		})
		if err := os.WriteFile(filepath.Join(wsDir, "workspace.json"), wsData, 0644); err != nil {
			t.Fatal(err)
		}

		if isProviderAvailableInProjectFolder(projectFolder) {
			t.Error("expected false when project folder layer says editor")
		}
		if !isProviderAvailableInProjectFolder("") {
			t.Error("expected true for global check when global has a real provider")
		}
	})
}
