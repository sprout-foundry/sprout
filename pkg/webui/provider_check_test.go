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
}
