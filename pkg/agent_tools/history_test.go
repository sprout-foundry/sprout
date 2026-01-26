package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/history"
)

func withTempWorkspace(t *testing.T) func() {
	t.Helper()

	tempDir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	return func() {
		_ = os.Chdir(oldDir)
	}
}

func recordSampleChange(t *testing.T, filename, original, updated string) string {
	t.Helper()

	revisionID, err := history.RecordBaseRevision("test-revision", "Update file", "Changes applied", []history.APIMessage{})
	if err != nil {
		t.Fatalf("failed to record base revision: %v", err)
	}

	if err := history.RecordChangeWithDetails(revisionID, filename, original, updated, "sample change", "", "sample prompt", "sample message", "test-model"); err != nil {
		t.Fatalf("failed to record change: %v", err)
	}

	// ensure file exists with updated content to simulate applied change
	if err := filesystem.WriteFileWithDir(filename, []byte(updated), 0644); err != nil {
		t.Fatalf("failed to write updated file: %v", err)
	}

	return revisionID
}

func TestViewHistoryWhenEmpty(t *testing.T) {
	cleanup := withTempWorkspace(t)
	defer cleanup()

	res, err := ViewHistory(5, "", nil, false)
	if err != nil {
		t.Fatalf("ViewHistory returned error: %v", err)
	}

	if !strings.Contains(res.Output, "No changes") {
		t.Fatalf("expected message about no changes, got: %s", res.Output)
	}

	if count, ok := res.Metadata["entry_count"].(int); !ok || count != 0 {
		t.Fatalf("expected entry_count=0, got: %#v", res.Metadata["entry_count"])
	}
}

func TestViewHistoryWithFilter(t *testing.T) {
	cleanup := withTempWorkspace(t)
	defer cleanup()

	revisionID := recordSampleChange(t, "sample/file.go", "package main\n", "package main\nfunc x() {}\n")

	res, err := ViewHistory(10, "file.go", nil, false)
	if err != nil {
		t.Fatalf("ViewHistory returned error: %v", err)
	}

	if !strings.Contains(res.Output, revisionID) {
		t.Fatalf("expected output to contain revision id %s, got: %s", revisionID, res.Output)
	}

	if count, ok := res.Metadata["entry_count"].(int); !ok || count != 1 {
		t.Fatalf("expected entry_count=1, got: %#v", res.Metadata["entry_count"])
	}
}

func TestRollbackChangesFlow(t *testing.T) {
	t.Run("ListRevisions", func(t *testing.T) {
		cleanup := withTempWorkspace(t)
		defer cleanup()

		recordSampleChange(t, "foo.txt", "original", "updated")

		res, err := RollbackChanges("", "", false)
		if err != nil {
			t.Fatalf("RollbackChanges returned error: %v", err)
		}

		if !strings.Contains(res.Output, "Available revisions") {
			t.Fatalf("expected list output, got: %s", res.Output)
		}

		if res.Metadata["action"] != "list_revisions" {
			t.Fatalf("expected action=list_revisions, got: %#v", res.Metadata["action"])
		}
	})

	t.Run("FileRollback", func(t *testing.T) {
		cleanup := withTempWorkspace(t)
		defer cleanup()

		filename := filepath.Join("dir", "bar.go")
		revisionID := recordSampleChange(t, filename, "package main\n", "package main\n// updated\n")

		// Preview file rollback
		preview, err := RollbackChanges(revisionID, filename, false)
		if err != nil {
			t.Fatalf("preview rollback returned error: %v", err)
		}
		if !strings.Contains(preview.Output, "Would rollback file") {
			t.Fatalf("expected preview message, got: %s", preview.Output)
		}

		// Execute file rollback
		result, err := RollbackChanges(revisionID, filename, true)
		if err != nil {
			t.Fatalf("rollback returned error: %v", err)
		}
		if !result.Success {
			t.Fatalf("expected rollback success")
		}

		content, err := filesystem.ReadFile(filename)
		if err != nil {
			t.Fatalf("failed to read file after rollback: %v", err)
		}
		if string(content) != "package main\n" {
			t.Fatalf("expected file to be restored, got: %s", string(content))
		}
	})

	t.Run("RevisionRollback", func(t *testing.T) {
		cleanup := withTempWorkspace(t)
		defer cleanup()

		revisionID := recordSampleChange(t, "baz.js", "console.log('hi')\n", "console.log('bye')\n")

		preview, err := RollbackChanges(revisionID, "", false)
		if err != nil {
			t.Fatalf("preview returned error: %v", err)
		}
		if !strings.Contains(preview.Output, "Would rollback revision") {
			t.Fatalf("expected revision preview, got: %s", preview.Output)
		}

		result, err := RollbackChanges(revisionID, "", true)
		if err != nil {
			t.Fatalf("rollback returned error: %v", err)
		}
		if !result.Success {
			t.Fatalf("expected revision rollback success")
		}

		content, err := filesystem.ReadFile("baz.js")
		if err != nil {
			t.Fatalf("failed to read file after revision rollback: %v", err)
		}
		if string(content) != "console.log('hi')\n" {
			t.Fatalf("expected revision rollback to restore original content, got: %s", string(content))
		}

		changes, err := history.GetAllChanges()
		if err != nil {
			t.Fatalf("failed to fetch changes: %v", err)
		}
		foundReverted := false
		for _, change := range changes {
			if change.RequestHash == revisionID && strings.EqualFold(change.Status, "reverted") {
				foundReverted = true
				break
			}
		}
		if !foundReverted {
			t.Fatalf("expected change to be marked as reverted")
		}
	})
}
