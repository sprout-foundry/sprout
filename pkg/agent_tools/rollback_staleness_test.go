package tools

import (
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// TestRollbackChangesSingleFileStaleSkip verifies that the single-file
// rollback path in RollbackChanges skips a file that was modified after
// the snapshot was taken (stale), returning action="stale_skip" without
// touching the file on disk.
func TestRollbackChangesSingleFileStaleSkip(t *testing.T) {
	cleanup := withTempWorkspace(t)
	defer cleanup()

	// Re-initialize history paths so changes are stored in the temp dir,
	// not whatever directory a prior test left configured.
	history.InitializeHistoryPaths(nil)

	filename := "stale_single.go"
	originalContent := "package main\n"
	newContent := "package main\n// updated\n"

	// Record a change (this also writes newContent to disk via recordSampleChange)
	revisionID := recordSampleChange(t, filename, originalContent, newContent)

	// Simulate external modification (e.g. git commit, manual edit)
	staleContent := "package main\n// committed externally\n"
	if err := os.WriteFile(filename, []byte(staleContent), 0644); err != nil {
		t.Fatalf("failed to write stale content: %v", err)
	}

	// Attempt rollback — should detect staleness and skip
	result, err := RollbackChanges(revisionID, filename, true)
	if err != nil {
		t.Fatalf("RollbackChanges returned error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected stale_skip to return success=true")
	}
	if result.Metadata["action"] != "stale_skip" {
		t.Fatalf("expected action=stale_skip, got: %#v", result.Metadata["action"])
	}

	// The file on disk should NOT have been changed (still has stale content)
	afterContent, err := filesystem.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file after rollback: %v", err)
	}
	if string(afterContent) != staleContent {
		t.Fatalf("file should still have stale content (not reverted), got: %s", string(afterContent))
	}
}

// TestRollbackChangesSingleFileNotStale verifies that the single-file
// rollback path in RollbackChanges proceeds normally when the file on
// disk still matches the recorded NewCode, restoring the original content.
func TestRollbackChangesSingleFileNotStale(t *testing.T) {
	cleanup := withTempWorkspace(t)
	defer cleanup()

	history.InitializeHistoryPaths(nil)

	filename := "fresh_single.go"
	originalContent := "console.log('original')\n"
	newContent := "console.log('updated')\n"

	// Record a change (writes newContent to disk)
	revisionID := recordSampleChange(t, filename, originalContent, newContent)

	// Rollback WITHOUT modifying the file (not stale)
	result, err := RollbackChanges(revisionID, filename, true)
	if err != nil {
		t.Fatalf("RollbackChanges returned error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected rollback success")
	}
	if result.Metadata["action"] != "file_rollback" {
		t.Fatalf("expected action=file_rollback, got: %#v", result.Metadata["action"])
	}

	content, err := filesystem.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file after rollback: %v", err)
	}
	if string(content) != originalContent {
		t.Fatalf("file should be restored to original content, got: %s", string(content))
	}
}
