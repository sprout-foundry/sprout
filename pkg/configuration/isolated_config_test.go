package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapIsolatedConfig_ClonesWithoutHistory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LEDIT_CONFIG", "")

	mainDir := filepath.Join(homeDir, ".ledit")
	if err := os.MkdirAll(mainDir, 0700); err != nil {
		t.Fatalf("mkdir main dir: %v", err)
	}

	source := NewConfig()
	source.LastUsedProvider = "openrouter"
	source.CommandHistory = []string{"legacy"}
	source.HistoryIndex = 5
	source.CommandHistoryByPath = map[string][]string{"/tmp/project": {"cmd1"}}
	source.HistoryIndexByPath = map[string]int{"/tmp/project": 1}

	cfgData, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatalf("marshal source config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, ConfigFileName), cfgData, 0600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, APIKeysFileName), []byte(`{"openrouter":"k"}`), 0600); err != nil {
		t.Fatalf("write source api keys: %v", err)
	}

	isolatedDir := filepath.Join(t.TempDir(), ".ledit")
	if err := BootstrapIsolatedConfig(isolatedDir); err != nil {
		t.Fatalf("bootstrap isolated config: %v", err)
	}

	isolatedData, err := os.ReadFile(filepath.Join(isolatedDir, ConfigFileName))
	if err != nil {
		t.Fatalf("read isolated config: %v", err)
	}
	var isolatedCfg Config
	if err := json.Unmarshal(isolatedData, &isolatedCfg); err != nil {
		t.Fatalf("parse isolated config: %v", err)
	}

	if isolatedCfg.LastUsedProvider != "openrouter" {
		t.Fatalf("expected provider preserved, got %q", isolatedCfg.LastUsedProvider)
	}
	if len(isolatedCfg.CommandHistory) != 0 {
		t.Fatalf("expected command history cleared, got %#v", isolatedCfg.CommandHistory)
	}
	if isolatedCfg.HistoryIndex != 0 {
		t.Fatalf("expected history index cleared, got %d", isolatedCfg.HistoryIndex)
	}
	if len(isolatedCfg.CommandHistoryByPath) != 0 {
		t.Fatalf("expected path history cleared, got %#v", isolatedCfg.CommandHistoryByPath)
	}
	if len(isolatedCfg.HistoryIndexByPath) != 0 {
		t.Fatalf("expected path history index cleared, got %#v", isolatedCfg.HistoryIndexByPath)
	}
	if _, err := os.Stat(filepath.Join(isolatedDir, APIKeysFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected api keys to remain global-only, got err=%v", err)
	}
}

func TestBootstrapIsolatedConfig_NoOverwriteWhenConfigExists(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LEDIT_CONFIG", "")

	isolatedDir := filepath.Join(t.TempDir(), ".ledit")
	if err := os.MkdirAll(isolatedDir, 0700); err != nil {
		t.Fatalf("mkdir isolated dir: %v", err)
	}
	existing := []byte(`{"version":"2.0","last_used_provider":"zai"}`)
	isolatedConfigPath := filepath.Join(isolatedDir, ConfigFileName)
	if err := os.WriteFile(isolatedConfigPath, existing, 0600); err != nil {
		t.Fatalf("write existing isolated config: %v", err)
	}

	if err := BootstrapIsolatedConfig(isolatedDir); err != nil {
		t.Fatalf("bootstrap isolated config: %v", err)
	}
	got, err := os.ReadFile(isolatedConfigPath)
	if err != nil {
		t.Fatalf("read isolated config: %v", err)
	}
	if string(got) != string(existing) {
		t.Fatalf("expected existing config untouched")
	}
}
