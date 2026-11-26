package configuration

import (
	"encoding/json"
	"testing"
)

func TestEarlierWorkspaceKeysAreRead(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"computer_use":{"workspace_allowlist":["/a"]},"shell":{"workspace_overlay":{"mode":"merge"}}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ComputerUse == nil || len(cfg.ComputerUse.ProjectFolderAllowlist) != 1 || cfg.ComputerUse.ProjectFolderAllowlist[0] != "/a" {
		t.Errorf("workspace_allowlist was not read: %+v", cfg.ComputerUse)
	}
	if cfg.Shell.ProjectFolderOverlay.Mode != "merge" {
		t.Errorf("workspace_overlay was not read: %+v", cfg.Shell.ProjectFolderOverlay)
	}
}

func TestCurrentKeysWinOverEarlierOnes(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"computer_use":{"project_folder_allowlist":["/new"],"workspace_allowlist":["/old"]}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ComputerUse.ProjectFolderAllowlist; len(got) != 1 || got[0] != "/new" {
		t.Errorf("allowlist = %v, want [/new]", got)
	}
	out, _ := json.Marshal(cfg.ComputerUse)
	if containsKey(out, "workspace_allowlist") {
		t.Errorf("the earlier key is written back: %s", out)
	}
}

func containsKey(data []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(data, &m)
	_, ok := m[key]
	return ok
}
