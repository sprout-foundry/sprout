package history

import (
	"os"
	"path/filepath"
	"testing"
)

// externalTempDir creates a temporary directory that is OUTSIDE both the
// current workspace and /tmp, so isWithinWorkspace returns false for it.
// This is needed because t.TempDir() returns a /tmp path on Linux, and the
// workspace boundary check intentionally allows all /tmp paths (same as
// SafeResolvePath).
//
// The directory is created under the user's home directory. If the home
// directory cannot be determined or is itself under /tmp, the test is skipped.
func externalTempDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir, skipping boundary test: %v", err)
	}
	if isTmpPath(base) {
		t.Skipf("home dir %s is under /tmp; cannot create a genuinely external dir", base)
	}
	dir, err := os.MkdirTemp(base, ".sprout-boundary-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir in home %s: %v", base, err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestRollbackSkipsFileOutsideWorkspace verifies that handleRevisionRollback
// skips (does not write) a file whose stored path resolves outside the current
// workspace root. This is the safety guard that prevents the history store from
// clobbering files elsewhere on the filesystem.
func TestRollbackSkipsFileOutsideWorkspace(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	// An absolute path that resolves OUTSIDE both the workspace (temp dir) and
	// /tmp. We can't use t.TempDir() for this because that lands under /tmp on
	// Linux, which is always allowed by the boundary check.
	externalDir := externalTempDir(t)
	externalFile := filepath.Join(externalDir, "should-not-be-written.txt")

	revID, err := RecordBaseRevision("boundary-rollback-skip", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// Record a change whose filename is the external absolute path. Status is
	// "active" by default so handleRevisionRollback will attempt it.
	if err := RecordChangeWithDetails(revID, externalFile, "old content", "new content", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Rollback must succeed (skip is not an error).
	if err := handleRevisionRollback(groups[0]); err != nil {
		t.Fatalf("handleRevisionRollback should skip external file without error: %v", err)
	}

	// The external file must NOT have been written with the rollback content.
	// (It may not exist at all, which is fine — the point is we didn't create
	// or overwrite it.)
	if content, err := os.ReadFile(externalFile); err == nil {
		if string(content) == "old content" {
			t.Errorf("rollback should not have written to external file %s, but it contains rolled-back content", externalFile)
		}
	}
}

// TestRollbackWritesFileInsideWorkspace verifies that the boundary check does
// not interfere with legitimate rollback of files within the workspace.
func TestRollbackWritesFileInsideWorkspace(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	revID, err := RecordBaseRevision("boundary-rollback-inside", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// A relative path inside the workspace (cwd == temp dir).
	insideFile := "inside.txt"
	if err := os.WriteFile(insideFile, []byte("modified content"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := RecordChangeWithDetails(revID, insideFile, "original content", "modified content", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if err := handleRevisionRollback(groups[0]); err != nil {
		t.Fatalf("handleRevisionRollback failed for inside-workspace file: %v", err)
	}

	restored, err := os.ReadFile(insideFile)
	if err != nil {
		t.Fatalf("expected file to be written during rollback: %v", err)
	}
	if string(restored) != "original content" {
		t.Errorf("expected 'original content', got %q", string(restored))
	}
}

// TestRestoreSkipsFileOutsideWorkspace verifies handleRevisionRestore has the
// same workspace boundary guard as rollback.
func TestRestoreSkipsFileOutsideWorkspace(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	externalDir := externalTempDir(t)
	externalFile := filepath.Join(externalDir, "should-not-be-restored.txt")

	revID, err := RecordBaseRevision("boundary-restore-skip", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	if err := RecordChangeWithDetails(revID, externalFile, "old content", "new content", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore should skip external file without error: %v", err)
	}

	// The external file must NOT contain the restored ("new") content.
	if content, err := os.ReadFile(externalFile); err == nil {
		if string(content) == "new content" {
			t.Errorf("restore should not have written to external file %s, but it contains restored content", externalFile)
		}
	}
}

// TestRestoreWritesFileInsideWorkspace verifies restore still works for files
// inside the workspace.
func TestRestoreWritesFileInsideWorkspace(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	revID, err := RecordBaseRevision("boundary-restore-inside", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	insideFile := "inside.txt"
	if err := os.WriteFile(insideFile, []byte("original content"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := RecordChangeWithDetails(revID, insideFile, "original content", "new content", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed for inside-workspace file: %v", err)
	}

	restored, err := os.ReadFile(insideFile)
	if err != nil {
		t.Fatalf("expected file to be written during restore: %v", err)
	}
	if string(restored) != "new content" {
		t.Errorf("expected 'new content', got %q", string(restored))
	}
}

// TestIsWithinWorkspaceDirect exercises the helper directly for the key cases.
func TestIsWithinWorkspaceDirect(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	// Determine a path that is genuinely outside /tmp for the "outside" case.
	// On Linux, os.TempDir() == /tmp, so we use the home directory instead.
	homeOutside := ""
	if home, err := os.UserHomeDir(); err == nil && !isTmpPath(home) {
		homeOutside = filepath.Join(home, "sprout-boundary-external-probe.txt")
	}

	cases := []struct {
		name   string
		path   string
		within bool
	}{
		{"relative inside", "foo.txt", true},
		{"nested relative inside", "sub/dir/foo.txt", true},
		{"absolute inside workspace", filepath.Join(dir, "abs.txt"), true},
		{"tmp path allowed", filepath.Join(os.TempDir(), "rollboundary-ok.txt"), true},
		{"empty rejected", "", false},
	}
	if homeOutside != "" {
		cases = append(cases, struct {
			name   string
			path   string
			within bool
		}{"absolute outside workspace and tmp", homeOutside, false})
	} else {
		t.Logf("could not find a non-/tmp external path; skipping 'outside workspace and tmp' case")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isWithinWorkspace(tc.path)
			if got != tc.within {
				t.Errorf("isWithinWorkspace(%q) = %v, want %v", tc.path, got, tc.within)
			}
		})
	}
}
