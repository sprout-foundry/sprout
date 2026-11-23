//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers for recent_workspaces
// ---------------------------------------------------------------------------

// setupRecentProjectFolders resets the global state to use a temp file,
// and returns a cleanup function. This prevents tests from polluting
// ~/.sprout/recent_workspaces.json on disk.
func setupRecentProjectFolders(t *testing.T) func() {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "recent_workspaces.json")
	recentProjectFolders.mu.Lock()
	oldFilePath := recentProjectFolders.filePath
	recentProjectFolders.filePath = tmpFile
	recentProjectFolders.projectFolders = nil
	recentProjectFolders.mu.Unlock()

	return func() {
		recentProjectFolders.mu.Lock()
		recentProjectFolders.filePath = oldFilePath
		recentProjectFolders.projectFolders = nil
		recentProjectFolders.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// GetRecentProjectFolders
// ---------------------------------------------------------------------------

func TestGetRecentProjectFolders_Empty(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	result := GetRecentProjectFolders()
	if result == nil {
		// nil is acceptable — no project folders recorded
		return
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

// ---------------------------------------------------------------------------
// RecordProjectFolder
// ---------------------------------------------------------------------------

func TestRecordProjectFolder_AddNew(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	dir := t.TempDir()
	// Plant a marker so IsProjectDirectory returns markers
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordProjectFolder(dir)

	result := GetRecentProjectFolders()
	if len(result) != 1 {
		t.Fatalf("expected 1 project folder, got %d", len(result))
	}
	ws := result[0]
	if ws.Path != dir {
		t.Errorf("path = %q, want %q", ws.Path, dir)
	}
	if ws.SessionCount != 1 {
		t.Errorf("session_count = %d, want 1", ws.SessionCount)
	}
}

func TestRecordProjectFolder_UpdateExisting(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordProjectFolder(dir)
	RecordProjectFolder(dir)

	result := GetRecentProjectFolders()
	if len(result) != 1 {
		t.Fatalf("expected 1 project folder, got %d", len(result))
	}
	ws := result[0]
	if ws.SessionCount != 2 {
		t.Errorf("session_count = %d, want 2", ws.SessionCount)
	}
}

func TestRecordProjectFolder_MaxEntries(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	// Record 12 distinct project folders (all with .git so they're valid projects)
	for i := 0; i < 12; i++ {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		RecordProjectFolder(dir)
	}

	result := GetRecentProjectFolders()
	if len(result) != maxRecentProjectFolders {
		t.Fatalf("expected %d project folders, got %d", maxRecentProjectFolders, len(result))
	}

	// The oldest (first) entry should have been dropped.
	// The most recent (12th) project folder should be at index 0.
}

// ---------------------------------------------------------------------------
// GetMostRecentProjectFolder
// ---------------------------------------------------------------------------

func TestGetMostRecentProjectFolder(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	dirA := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirA, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	dirB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirB, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordProjectFolder(dirA)
	RecordProjectFolder(dirB)

	mostRecent := GetMostRecentProjectFolder()
	if mostRecent != dirB {
		t.Errorf("most recent = %q, want %q", mostRecent, dirB)
	}
}

func TestGetMostRecentProjectFolder_Empty(t *testing.T) {
	cleanup := setupRecentProjectFolders(t)
	defer cleanup()

	result := GetMostRecentProjectFolder()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
