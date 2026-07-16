// Tests for shell_skip_dirs.json persistence + LRU eviction.
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// withShellSkipDirsFile redirects the config dir to a temp dir for the
// duration of the test, so tests don't touch the user's real
// shell_skip_dirs.json. Returns the temp dir path for inspection.
func withShellSkipDirsFile(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	// SPROUT_CONFIG (canonical) + LEDIT_CONFIG (legacy) take precedence
	// over $HOME in envutil.GetConfigDir — without these, the test's
	// pre-populated file in tmpDir is bypassed and saveAutoSkipDirsFor
	// writes to the test-runner's real config dir instead.
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	return tmpDir
}

// readShellSkipDirsFile reads the persisted file from the temp config
// dir and returns the parsed schema. Returns an empty schema if the
// file doesn't exist.
func readShellSkipDirsFile(t *testing.T, tmpDir string) shellSkipDirsFileSchema {
	t.Helper()
	// SPROUT_CONFIG takes precedence over HOME in envutil.GetConfigDir,
	// so the persisted file lives directly under tmpDir.
	path := filepath.Join(tmpDir, shellSkipDirsFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return shellSkipDirsFileSchema{
			Workspaces: map[string][]string{},
			LastUsed:   map[string]int64{},
		}
	}
	var schema shellSkipDirsFileSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if schema.Workspaces == nil {
		schema.Workspaces = map[string][]string{}
	}
	if schema.LastUsed == nil {
		schema.LastUsed = map[string]int64{}
	}
	return schema
}

// TestShellSkipDirs_EvictsLRUWhenOverCap verifies that saving with
// more than maxPersistedWorkspaces entries evicts the least-recently-used.
func TestShellSkipDirs_EvictsLRUWhenOverCap(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)

	// Pre-populate the file with maxPersistedWorkspaces + 2 entries,
	// each with a distinct LastUsed timestamp.
	schema := shellSkipDirsFileSchema{
		Version:    1,
		Workspaces: make(map[string][]string),
		LastUsed:   make(map[string]int64),
	}
	baseTime := time.Now().Unix() - int64(maxPersistedWorkspaces*100)
	for i := 0; i < maxPersistedWorkspaces+2; i++ {
		root := filepath.Join(tmpDir, "ws", string(rune('a'+i)))
		schema.Workspaces[root] = []string{"bigdir"}
		schema.LastUsed[root] = baseTime + int64(i*100)
	}
	// Write initial state.
	path := filepath.Join(tmpDir, shellSkipDirsFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(&schema, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Save a new workspace — should trigger eviction of the 2 oldest.
	newRoot := filepath.Join(tmpDir, "ws", "newest")
	newDirs := map[string]bool{"bigdir": true}
	if err := saveAutoSkipDirsFor(newRoot, newDirs); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := readShellSkipDirsFile(t, tmpDir)
	if len(got.Workspaces) > maxPersistedWorkspaces {
		t.Fatalf("expected at most %d workspaces, got %d", maxPersistedWorkspaces, len(got.Workspaces))
	}
	if _, ok := got.Workspaces[newRoot]; !ok {
		t.Errorf("new workspace %q not persisted", newRoot)
	}

	// The 2 oldest (by LastUsed) should have been evicted.
	roots := make([]string, 0, len(got.Workspaces))
	for r := range got.Workspaces {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	// First alphabetized root should NOT be 'a' (the oldest) or 'b'
	// (second oldest). Verify by checking LastUsed values.
	for _, r := range roots {
		if r == filepath.Join(tmpDir, "ws", "a") || r == filepath.Join(tmpDir, "ws", "b") {
			t.Errorf("oldest workspace %q should have been evicted", r)
		}
	}
}

// TestShellSkipDirs_BumpsLastUsedOnSave verifies that saving updates
// the LastUsed timestamp for the current workspace.
func TestShellSkipDirs_BumpsLastUsedOnSave(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)
	root := filepath.Join(tmpDir, "ws", "test")
	dirs := map[string]bool{"a": true, "b": true}

	before := time.Now().Unix()
	if err := saveAutoSkipDirsFor(root, dirs); err != nil {
		t.Fatalf("save: %v", err)
	}
	after := time.Now().Unix()

	got := readShellSkipDirsFile(t, tmpDir)
	ts, ok := got.LastUsed[root]
	if !ok {
		t.Fatalf("LastUsed[%q] not set", root)
	}
	if ts < before || ts > after {
		t.Errorf("LastUsed[%q] = %d, expected in [%d, %d]", root, ts, before, after)
	}
}

// TestShellSkipDirs_HandlesMissingLastUsed verifies backward
// compatibility with files written by older versions (no LastUsed
// field).
func TestShellSkipDirs_HandlesMissingLastUsed(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)

	// Write a file with only Workspaces (old format).
	path := filepath.Join(tmpDir, shellSkipDirsFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"version": 1, "workspaces": {"/old/ws": ["fatdir"]}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load should work and return the old workspace's dirs.
	loaded := loadAutoSkipDirsFor("/old/ws")
	if !loaded["fatdir"] {
		t.Errorf("expected fatdir in loaded set, got %v", loaded)
	}

	// Save should populate LastUsed for the current workspace.
	if err := saveAutoSkipDirsFor("/new/ws", map[string]bool{"x": true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := readShellSkipDirsFile(t, tmpDir)
	if ts, ok := got.LastUsed["/new/ws"]; !ok || ts == 0 {
		t.Errorf("LastUsed[/new/ws] not populated: %v", got.LastUsed)
	}
}

// TestShellSkipDirs_AtomicWriteProducesValidJSON verifies that the
// temp+rename pattern doesn't produce partial JSON.
func TestShellSkipDirs_AtomicWriteProducesValidJSON(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)
	root := filepath.Join(tmpDir, "ws", "atomic")
	for i := 0; i < 5; i++ {
		dirs := map[string]bool{"d" + string(rune('0'+i)): true}
		if err := saveAutoSkipDirsFor(root, dirs); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	got := readShellSkipDirsFile(t, tmpDir)
	if len(got.Workspaces[root]) != 5 {
		t.Errorf("expected 5 dirs, got %d: %v", len(got.Workspaces[root]), got.Workspaces[root])
	}
}

// TestShellSkipDirs_EmptyWorkspaceNoOp verifies that saving with an
// empty workspace or empty dirs is a no-op (no file written).
func TestShellSkipDirs_EmptyWorkspaceNoOp(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)
	if err := saveAutoSkipDirsFor("", map[string]bool{"a": true}); err != nil {
		t.Errorf("save with empty workspace: %v", err)
	}
	if err := saveAutoSkipDirsFor("/ws", map[string]bool{}); err != nil {
		t.Errorf("save with empty dirs: %v", err)
	}
	path := filepath.Join(tmpDir, shellSkipDirsFilename)
	if _, err := os.Stat(path); err == nil {
		t.Errorf("file should not exist for no-op saves")
	}
}

// TestShellSkipDirs_LoadRecordsWorkspaceForBump verifies that loading
// a workspace returns the persisted set without mutating it.
func TestShellSkipDirs_LoadRecordsWorkspaceForBump(t *testing.T) {
	tmpDir := withShellSkipDirsFile(t)
	root := filepath.Join(tmpDir, "ws", "loaded")

	// Pre-populate with an old LastUsed.
	path := filepath.Join(tmpDir, shellSkipDirsFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := shellSkipDirsFileSchema{
		Version:    1,
		Workspaces: map[string][]string{root: {"x"}},
		LastUsed:   map[string]int64{root: 1000}, // ancient
	}
	data, _ := json.MarshalIndent(&old, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Load the workspace — should return the persisted set.
	loaded := loadAutoSkipDirsFor(root)
	if !loaded["x"] {
		t.Fatalf("expected x in loaded set")
	}

	// Load must not mutate LastUsed on disk.
	got := readShellSkipDirsFile(t, tmpDir)
	if got.LastUsed[root] != 1000 {
		t.Errorf("LastUsed[%q] should still be 1000 after load, got %d", root, got.LastUsed[root])
	}
}
