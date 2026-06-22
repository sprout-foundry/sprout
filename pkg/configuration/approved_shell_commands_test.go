package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestApprovedShellCommandsRoundTrip pins down JSON serialization of the
// new persistent allowlist — the field's omitempty must NOT collapse a
// populated list, and the JSON key must remain stable across releases
// (users can hand-edit the file).
func TestApprovedShellCommandsRoundTrip(t *testing.T) {
	cfg := Config{
		ApprovedShellCommands: []string{"rm -rf /tmp/build", "git push origin main --force-with-lease"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !contains(string(data), `"approved_shell_commands":["rm -rf /tmp/build","git push origin main --force-with-lease"]`) {
		t.Errorf("unexpected JSON form: %s", data)
	}

	var roundTripped Config
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(roundTripped.ApprovedShellCommands) != 2 {
		t.Errorf("after round-trip: got %d entries, want 2", len(roundTripped.ApprovedShellCommands))
	}
}

func TestApprovedShellCommandsEmptyOmitted(t *testing.T) {
	cfg := Config{}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if contains(string(data), "approved_shell_commands") {
		t.Errorf("empty list should be omitted via omitempty, got: %s", data)
	}
}

// TestApprovedShellCommandsMerge_Union confirms workspace-layer entries
// add to (not replace) the global-layer list, with de-dup.
func TestApprovedShellCommandsMerge_Union(t *testing.T) {
	tmpRoot := t.TempDir()
	globalDir := filepath.Join(tmpRoot, "global")
	workspaceDir := filepath.Join(tmpRoot, "workspace", ".sprout")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	global := Config{ApprovedShellCommands: []string{"rm -rf /tmp/build", "kubectl delete pod foo"}}
	workspace := Config{ApprovedShellCommands: []string{"kubectl delete pod foo", "git push --force"}}

	globalData, _ := json.Marshal(global)
	workspaceData, _ := json.Marshal(workspace)
	globalPath := filepath.Join(globalDir, "config.json")
	workspacePath := filepath.Join(workspaceDir, "config.json")
	if err := os.WriteFile(globalPath, globalData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspacePath, workspaceData, 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}
	got := merged.ApprovedShellCommands
	if len(got) != 3 {
		t.Errorf("expected 3 unique entries after merge, got %d: %v", len(got), got)
	}
	wantSet := map[string]bool{
		"rm -rf /tmp/build":      true,
		"kubectl delete pod foo": true,
		"git push --force":       true,
	}
	for _, cmd := range got {
		if !wantSet[cmd] {
			t.Errorf("unexpected merged entry %q", cmd)
		}
		delete(wantSet, cmd)
	}
	if len(wantSet) > 0 {
		t.Errorf("missing entries after merge: %v", wantSet)
	}
}

// TestApprovedShellCommandPatternsMerge_Union mirrors the
// ApprovedShellCommands merge test for the pattern list added in the
// ApprovedShellCommandPatterns extension. Verifies workspace-layer
// patterns are unioned with (not replaced by) global-layer patterns.
func TestApprovedShellCommandPatternsMerge_Union(t *testing.T) {
	tmpRoot := t.TempDir()
	globalDir := filepath.Join(tmpRoot, "global")
	workspaceDir := filepath.Join(tmpRoot, "workspace", ".sprout")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	global := Config{ApprovedShellCommandPatterns: []string{"rm -rf /tmp/*", "go test *"}}
	workspace := Config{ApprovedShellCommandPatterns: []string{"go test *", "git push --force *"}}

	globalData, _ := json.Marshal(global)
	workspaceData, _ := json.Marshal(workspace)
	globalPath := filepath.Join(globalDir, "config.json")
	workspacePath := filepath.Join(workspaceDir, "config.json")
	if err := os.WriteFile(globalPath, globalData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspacePath, workspaceData, 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}
	got := merged.ApprovedShellCommandPatterns
	if len(got) != 3 {
		t.Fatalf("expected 3 unique patterns after merge, got %d: %v", len(got), got)
	}
	wantSet := map[string]bool{
		"rm -rf /tmp/*":      true,
		"go test *":          true,
		"git push --force *": true,
	}
	for _, p := range got {
		if !wantSet[p] {
			t.Errorf("unexpected merged pattern %q", p)
		}
		delete(wantSet, p)
	}
	if len(wantSet) > 0 {
		t.Errorf("missing patterns after merge: %v", wantSet)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
