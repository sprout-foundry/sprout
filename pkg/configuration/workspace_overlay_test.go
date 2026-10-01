package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSONFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWorkspaceSave_WritesOnlyWhatChanged(t *testing.T) {
	global := t.TempDir()
	ws := filepath.Join(t.TempDir(), ConfigDirName)
	writeJSONFile(t, filepath.Join(global, ConfigFileName), `{"computer_use":{"enabled":true},"last_used_provider":"openai","provider_priority":["openai"]}`)
	writeJSONFile(t, filepath.Join(ws, WorkspaceConfigFileName), `{"provider_models":{"openai":"gpt-a"}}`)

	m, err := NewManagerWithLayers(global, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateConfig(func(c *Config) error { c.LastUsedProvider = "deepinfra"; return nil }); err != nil {
		t.Fatal(err)
	}

	saved := readJSONFile(t, filepath.Join(ws, WorkspaceConfigFileName))
	delete(saved, "version")
	if len(saved) != 2 || saved["last_used_provider"] != "deepinfra" || saved["provider_models"] == nil {
		t.Fatalf("workspace file = %v, want only its own provider_models plus the changed last_used_provider", saved)
	}
}

func TestWorkspaceSave_NestedChangeOverridesGlobal(t *testing.T) {
	global := t.TempDir()
	ws := filepath.Join(t.TempDir(), ConfigDirName)
	writeJSONFile(t, filepath.Join(global, ConfigFileName), `{"mcp":{"enabled":true,"timeout":45}}`)

	m, err := NewManagerWithLayers(global, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateConfig(func(c *Config) error { c.MCP.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}

	saved := readJSONFile(t, filepath.Join(ws, WorkspaceConfigFileName))
	mcpSaved, _ := saved["mcp"].(map[string]interface{})
	if mcpSaved["enabled"] != false || mcpSaved["timeout"] != nil {
		t.Fatalf("mcp in workspace = %v, want only enabled=false", mcpSaved)
	}

	reloaded, err := NewManagerWithLayers(global, ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetConfig().MCP; got.Enabled {
		t.Fatalf("reloaded mcp = %+v, want disabled in this workspace", got)
	}
}

func TestWorkspaceSave_LaterGlobalChangeStillApplies(t *testing.T) {
	global := t.TempDir()
	ws := filepath.Join(t.TempDir(), ConfigDirName)
	globalFile := filepath.Join(global, ConfigFileName)
	writeJSONFile(t, globalFile, `{"mcp":{"enabled":false}}`)

	m, err := NewManagerWithLayers(global, ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateConfig(func(c *Config) error { c.LastUsedProvider = "deepinfra"; return nil }); err != nil {
		t.Fatal(err)
	}

	writeJSONFile(t, globalFile, `{"mcp":{"enabled":true}}`)
	reloaded, err := NewManagerWithLayers(global, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.GetConfig().MCP.Enabled {
		t.Fatal("enabling MCP globally didn't reach a workspace that had saved an unrelated setting")
	}
}
