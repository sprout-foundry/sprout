package webui

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers for recent_workspaces
// ---------------------------------------------------------------------------

// setupRecentWorkspaces resets the global state to use a temp file,
// and returns a cleanup function. This prevents tests from polluting
// ~/.sprout/recent_workspaces.json on disk.
func setupRecentWorkspaces(t *testing.T) func() {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "recent_workspaces.json")
	recentWorkspaces.mu.Lock()
	oldFilePath := recentWorkspaces.filePath
	recentWorkspaces.filePath = tmpFile
	recentWorkspaces.workspaces = nil
	recentWorkspaces.mu.Unlock()

	return func() {
		recentWorkspaces.mu.Lock()
		recentWorkspaces.filePath = oldFilePath
		recentWorkspaces.workspaces = nil
		recentWorkspaces.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// GetRecentWorkspaces
// ---------------------------------------------------------------------------

func TestGetRecentWorkspaces_Empty(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	result := GetRecentWorkspaces()
	if result == nil {
		// nil is acceptable — no workspaces recorded
		return
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

// ---------------------------------------------------------------------------
// RecordWorkspace
// ---------------------------------------------------------------------------

func TestRecordWorkspace_AddNew(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	dir := t.TempDir()
	// Plant a marker so IsProjectDirectory returns markers
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordWorkspace(dir)

	result := GetRecentWorkspaces()
	if len(result) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(result))
	}
	ws := result[0]
	if ws.Path != dir {
		t.Errorf("path = %q, want %q", ws.Path, dir)
	}
	if ws.SessionCount != 1 {
		t.Errorf("session_count = %d, want 1", ws.SessionCount)
	}
}

func TestRecordWorkspace_UpdateExisting(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordWorkspace(dir)
	RecordWorkspace(dir)

	result := GetRecentWorkspaces()
	if len(result) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(result))
	}
	ws := result[0]
	if ws.SessionCount != 2 {
		t.Errorf("session_count = %d, want 2", ws.SessionCount)
	}
}

func TestRecordWorkspace_MaxEntries(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	// Record 12 distinct workspaces (all with .git so they're valid projects)
	for i := 0; i < 12; i++ {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		RecordWorkspace(dir)
	}

	result := GetRecentWorkspaces()
	if len(result) != maxRecentWorkspaces {
		t.Fatalf("expected %d workspaces, got %d", maxRecentWorkspaces, len(result))
	}

	// The oldest (first) entry should have been dropped.
	// The most recent (12th) workspace should be at index 0.
}

// ---------------------------------------------------------------------------
// GetMostRecentWorkspace
// ---------------------------------------------------------------------------

func TestGetMostRecentWorkspace(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	dirA := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirA, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	dirB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirB, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordWorkspace(dirA)
	RecordWorkspace(dirB)

	mostRecent := GetMostRecentWorkspace()
	if mostRecent != dirB {
		t.Errorf("most recent = %q, want %q", mostRecent, dirB)
	}
}

func TestGetMostRecentWorkspace_Empty(t *testing.T) {
	cleanup := setupRecentWorkspaces(t)
	defer cleanup()

	result := GetMostRecentWorkspace()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
