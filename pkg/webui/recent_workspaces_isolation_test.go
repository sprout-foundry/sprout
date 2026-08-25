//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"testing"
)

// Dead entries must not reach the picker: a recent workspace that no longer
// exists is indistinguishable from a real one in the UI and cannot be selected.
// In practice the user's picker showed ten leaked test temp paths and nothing
// else, with no way to tell they were stale.
func TestGetRecentWorkspacesFiltersMissingPaths(t *testing.T) {
	defer setupRecentWorkspaces(t)()

	live := t.TempDir()
	RecordWorkspace(live)

	gone := filepath.Join(t.TempDir(), "deleted-project")
	if err := os.MkdirAll(gone, 0755); err != nil {
		t.Fatal(err)
	}
	RecordWorkspace(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	got := GetRecentWorkspaces()
	for _, w := range got {
		if w.Path == gone {
			t.Errorf("a workspace that no longer exists should not be offered: %s", gone)
		}
	}
	found := false
	for _, w := range got {
		if w.Path == live {
			found = true
		}
	}
	if !found {
		t.Errorf("an existing workspace must still be listed; got %v", got)
	}
}

// A file (rather than a directory) at the recorded path is equally unusable.
func TestGetRecentWorkspacesFiltersNonDirectories(t *testing.T) {
	defer setupRecentWorkspaces(t)()

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	RecordWorkspace(file)

	for _, w := range GetRecentWorkspaces() {
		if w.Path == file {
			t.Errorf("a non-directory path should not be offered: %s", file)
		}
	}
}
