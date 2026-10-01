package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPruneWorkspaceConfig(t *testing.T) {
	global := t.TempDir()
	writeJSONFile(t, filepath.Join(global, ConfigFileName), `{"last_used_provider":"openai","mcp":{"enabled":true,"timeout":45},"provider_priority":["openai","anthropic"]}`)

	wsFile := filepath.Join(t.TempDir(), ConfigDirName, WorkspaceConfigFileName)
	bloated := `{
  "version": "2.1",
  "last_used_provider": "openai",
  "provider_priority": ["openai", "anthropic"],
  "mcp": {"enabled": false, "timeout": 45},
  "provider_models": {"deepinfra": "some-model"},
  "computer_use": {},
  "future_setting": "kept"
}`
	writeJSONFile(t, wsFile, bloated)

	dry, err := PruneWorkspaceConfig(global, wsFile, true)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(wsFile); string(data) != bloated {
		t.Fatal("dry run modified the file")
	}

	res, err := PruneWorkspaceConfig(global, wsFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, dry.Removed) {
		t.Fatalf("dry run reported %v, real run removed %v", dry.Removed, res.Removed)
	}
	// mcp.timeout stays: a workspace mcp block without one loads the 30s
	// default, which would override the global 45.
	wantRemoved := []string{"computer_use", "last_used_provider", "provider_priority"}
	if !reflect.DeepEqual(res.Removed, wantRemoved) {
		t.Fatalf("removed %v, want %v", res.Removed, wantRemoved)
	}

	saved := readJSONFile(t, wsFile)
	mcp, _ := saved["mcp"].(map[string]interface{})
	if mcp["enabled"] != false || saved["provider_models"] == nil || saved["future_setting"] != "kept" || saved["version"] == nil {
		t.Fatalf("pruned file = %v, want the workspace's own settings kept", saved)
	}
	if backup, _ := os.ReadFile(wsFile + ".bak"); string(backup) != bloated {
		t.Fatal("original not kept as .bak")
	}

	m, err := NewManagerWithLayers(global, filepath.Dir(wsFile))
	if err != nil {
		t.Fatal(err)
	}
	if cfg := m.GetConfig(); cfg.MCP.Enabled || cfg.LastUsedProvider != "openai" {
		t.Fatalf("effective config changed by pruning: mcp.enabled=%v last_used_provider=%q", cfg.MCP.Enabled, cfg.LastUsedProvider)
	}
}

func TestPruneWorkspaceConfig_NothingToRemoveLeavesFileAlone(t *testing.T) {
	global := t.TempDir()
	writeJSONFile(t, filepath.Join(global, ConfigFileName), `{"last_used_provider":"openai"}`)
	wsFile := filepath.Join(t.TempDir(), WorkspaceConfigFileName)
	writeJSONFile(t, wsFile, `{"last_used_provider":"deepinfra"}`)

	res, err := PruneWorkspaceConfig(global, wsFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("removed %v from a file with only its own settings", res.Removed)
	}
	if _, err := os.Stat(wsFile + ".bak"); !os.IsNotExist(err) {
		t.Fatal("wrote a backup though nothing changed")
	}
}

func TestPruneWorkspaceConfig_KeepsSettingsThatReplaceWholesale(t *testing.T) {
	global := t.TempDir()
	writeJSONFile(t, filepath.Join(global, ConfigFileName), `{"command_history_by_path":{"/a":["ls"]},"last_used_provider":"openai"}`)
	wsFile := filepath.Join(t.TempDir(), ConfigDirName, WorkspaceConfigFileName)
	writeJSONFile(t, wsFile, `{"command_history_by_path":{"/a":["ls"],"/b":["make"]},"last_used_provider":"openai"}`)

	before, err := NewManagerWithLayers(global, filepath.Dir(wsFile))
	if err != nil {
		t.Fatal(err)
	}
	wantHistory := before.GetConfig().CommandHistoryByPath

	res, err := PruneWorkspaceConfig(global, wsFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{"last_used_provider"}) {
		t.Fatalf("removed %v, want only last_used_provider", res.Removed)
	}
	after, err := NewManagerWithLayers(global, filepath.Dir(wsFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := after.GetConfig().CommandHistoryByPath; !reflect.DeepEqual(got, wantHistory) {
		t.Fatalf("command history after pruning = %v, want %v", got, wantHistory)
	}
}
