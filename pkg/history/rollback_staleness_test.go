package history

import (
	"os"
	"testing"
)

// TestRollbackSkipsStaleFile verifies that the staleness guard prevents
// rolling back a file that was intentionally modified after the snapshot
// (e.g. by a git commit, manual edit, or another session).
func TestRollbackSkipsStaleFile(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	revisionID, err := RecordBaseRevision("stale-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "stale.go"
	originalContent := "v1"
	newContent := "v2"

	// Record the change and write the "new" content to disk
	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}
	if err := os.WriteFile(filename, []byte(newContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Simulate external modification after the snapshot (e.g. git commit,
	// manual edit, or another session writing to the file).
	externalContent := "v3-committed"
	if err := os.WriteFile(filename, []byte(externalContent), 0600); err != nil {
		t.Fatalf("Failed to write stale file: %v", err)
	}

	// Attempt rollback — should skip the file because it's stale
	if err := RevertChangeByRevisionID(revisionID); err != nil {
		t.Fatalf("RevertChangeByRevisionID failed: %v", err)
	}

	// The file should still have the external content (NOT reverted to v1)
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file after rollback: %v", err)
	}
	if string(restored) != externalContent {
		t.Errorf("Stale file should NOT be reverted. Expected: %s, Got: %s",
			externalContent, string(restored))
	}

	// The change status should still be "active" (NOT marked as reverted)
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	for _, change := range changes {
		if change.RequestHash == revisionID {
			if change.Status != "active" {
				t.Errorf("Change status should still be 'active', got: '%s' for file %s",
					change.Status, change.Filename)
			}
		}
	}
}

// TestRollbackProceedsWhenNotStale verifies that a normal rollback proceeds
// when the file on disk still matches the recorded NewCode.
func TestRollbackProceedsWhenNotStale(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	revisionID, err := RecordBaseRevision("fresh-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "fresh.go"
	originalContent := "v1"
	newContent := "v2"

	// Record the change and write the "new" content to disk
	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}
	if err := os.WriteFile(filename, []byte(newContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Rollback without modifying the file (not stale)
	if err := RevertChangeByRevisionID(revisionID); err != nil {
		t.Fatalf("RevertChangeByRevisionID failed: %v", err)
	}

	// File should be reverted to original content
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file after rollback: %v", err)
	}
	if string(restored) != originalContent {
		t.Errorf("File should be reverted. Expected: %s, Got: %s",
			originalContent, string(restored))
	}

	// Status should be "reverted"
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	foundReverted := false
	for _, change := range changes {
		if change.RequestHash == revisionID && change.Status == "reverted" {
			foundReverted = true
		}
	}
	if !foundReverted {
		t.Errorf("Expected change status to be 'reverted'")
	}
}

// TestRollbackMixedStaleAndFresh verifies that within a single revision
// rollback, stale files are skipped while fresh files are reverted.
func TestRollbackMixedStaleAndFresh(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	revisionID, err := RecordBaseRevision("mixed-stale-revision", "Update files", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	// File A: will be made stale (modified externally)
	fileA := "stale_file.go"
	if err := RecordChangeWithDetails(revisionID, fileA, "original A", "updated A", "update A", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails (A) failed: %v", err)
	}
	if err := os.WriteFile(fileA, []byte("updated A"), 0600); err != nil {
		t.Fatalf("Failed to write file A: %v", err)
	}
	// Simulate external modification
	staleContentA := "committed externally A"
	if err := os.WriteFile(fileA, []byte(staleContentA), 0600); err != nil {
		t.Fatalf("Failed to write stale file A: %v", err)
	}

	// File B: will remain fresh (matches NewCode)
	fileB := "fresh_file.go"
	if err := RecordChangeWithDetails(revisionID, fileB, "original B", "updated B", "update B", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails (B) failed: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("updated B"), 0600); err != nil {
		t.Fatalf("Failed to write file B: %v", err)
	}

	// Rollback the entire revision
	if err := RevertChangeByRevisionID(revisionID); err != nil {
		t.Fatalf("RevertChangeByRevisionID failed: %v", err)
	}

	// File A should NOT be reverted (keeps external content)
	contentA, err := os.ReadFile(fileA)
	if err != nil {
		t.Fatalf("Failed to read file A after rollback: %v", err)
	}
	if string(contentA) != staleContentA {
		t.Errorf("Stale file A should keep external content. Expected: %s, Got: %s",
			staleContentA, string(contentA))
	}

	// File B SHOULD be reverted to original
	contentB, err := os.ReadFile(fileB)
	if err != nil {
		t.Fatalf("Failed to read file B after rollback: %v", err)
	}
	if string(contentB) != "original B" {
		t.Errorf("Fresh file B should be reverted. Expected: %s, Got: %s",
			"original B", string(contentB))
	}

	// Verify status: A should still be active, B should be reverted
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	for _, change := range changes {
		if change.RequestHash == revisionID {
			switch change.Filename {
			case fileA:
				if change.Status != "active" {
					t.Errorf("Stale file A should still be 'active', got: '%s'", change.Status)
				}
			case fileB:
				if change.Status != "reverted" {
					t.Errorf("Fresh file B should be 'reverted', got: '%s'", change.Status)
				}
			}
		}
	}
}

// TestRollbackSkipsWhenFileDeleted verifies that when the file has been
// deleted from disk after the snapshot, isFileStale returns false (not stale)
// and the rollback proceeds, recreating the file with the original content.
func TestRollbackSkipsWhenFileDeleted(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	revisionID, err := RecordBaseRevision("deleted-file-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "deleted.go"
	originalContent := "v1"
	newContent := "v2"

	// Record change and write new content
	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}
	if err := os.WriteFile(filename, []byte(newContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Delete the file (simulating it being removed after the snapshot)
	if err := os.Remove(filename); err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// Rollback — isFileStale returns false for missing files, so the
	// rollback should proceed and recreate the file with original content.
	if err := RevertChangeByRevisionID(revisionID); err != nil {
		t.Fatalf("RevertChangeByRevisionID failed: %v", err)
	}

	// The file should be recreated with the original content
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("File should be recreated after rollback, but ReadFile failed: %v", err)
	}
	if string(restored) != originalContent {
		t.Errorf("Deleted file should be restored to original. Expected: %s, Got: %s",
			originalContent, string(restored))
	}
}

// TestIsFileStaleHelper directly tests the isFileStale helper function
// across all its decision branches.
func TestIsFileStaleHelper(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	t.Run("EmptyNewCode_ReturnsFalse", func(t *testing.T) {
		// Even if a file exists, empty newCode means no baseline to compare
		if err := os.WriteFile("empty_newcode.txt", []byte("some content"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStale("empty_newcode.txt", "") {
			t.Error("isFileStale should return false for empty newCode")
		}
	})

	t.Run("RedactedMarkerNewCode_ReturnsFalse", func(t *testing.T) {
		if err := os.WriteFile("redacted_newcode.txt", []byte("some content"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStale("redacted_newcode.txt", RedactedContentMarker) {
			t.Error("isFileStale should return false for RedactedContentMarker newCode")
		}
	})

	t.Run("FileDoesNotExist_ReturnsFalse", func(t *testing.T) {
		// isFileStale returns false when os.ReadFile errors (file missing)
		if isFileStale("nonexistent.txt", "some content") {
			t.Error("isFileStale should return false when file doesn't exist")
		}
	})

	t.Run("FileMatchesNewCode_ReturnsFalse", func(t *testing.T) {
		content := "matching content"
		if err := os.WriteFile("matching.txt", []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStale("matching.txt", content) {
			t.Error("isFileStale should return false when file matches newCode")
		}
	})

	t.Run("FileDiffersFromNewCode_ReturnsTrue", func(t *testing.T) {
		if err := os.WriteFile("differing.txt", []byte("current content"), 0600); err != nil {
			t.Fatal(err)
		}
		if !isFileStale("differing.txt", "expected content") {
			t.Error("isFileStale should return true when file differs from newCode")
		}
	})
}
