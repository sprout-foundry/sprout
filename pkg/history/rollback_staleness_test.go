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

// ---------------------------------------------------------------------------
// RESTORE staleness guard regression tests
//
// handleRevisionRestore was missing the staleness guard that
// handleRevisionRollback has. This silently clobbered files that were
// modified after the snapshot (committed, manually edited, or touched
// by another session). These tests lock in the fix.
// ---------------------------------------------------------------------------

// TestRestoreSkipsStaleFile verifies that the staleness guard in
// handleRevisionRestore prevents restoring a file that was modified
// after the snapshot. The file on disk has neither the originalCode
// nor the newCode — it was modified by a commit/manual edit.
func TestRestoreSkipsStaleFile(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("restore-stale-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "restore_stale.go"
	originalContent := "v1-original"
	newContent := "v2-agent-edit"

	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}

	// Simulate: the file was committed/edited AFTER the snapshot. It now
	// holds content that matches NEITHER originalCode NOR newCode.
	externalContent := "v3-committed-externally"
	if err := os.WriteFile(filename, []byte(externalContent), 0600); err != nil {
		t.Fatalf("Failed to write stale file: %v", err)
	}

	// Fetch the revision group and call handleRevisionRestore.
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 revision group, got %d", len(groups))
	}

	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	// The file should STILL have the external content — restore must
	// NOT have clobbered it.
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file after restore: %v", err)
	}
	if string(restored) != externalContent {
		t.Errorf("Stale file should NOT be restored. Expected: %s, Got: %s",
			externalContent, string(restored))
	}

	// The change status should NOT have been updated to "restored".
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	for _, change := range changes {
		if change.RequestHash == revisionID && change.Filename == filename {
			if change.Status == "restored" {
				t.Errorf("Change status should NOT be 'restored' for stale file, got: '%s'", change.Status)
			}
		}
	}
}

// TestRestoreProceedsWhenAtNewCode verifies that restore proceeds
// when the file on disk matches newCode (already in target state — a
// no-op write). This is safe: nothing else changed it.
func TestRestoreProceedsWhenAtNewCode(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("restore-newcode-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "restore_at_newcode.go"
	originalContent := "v1"
	newContent := "v2"

	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}

	// File on disk matches newCode exactly.
	if err := os.WriteFile(filename, []byte(newContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	list, _ := GetAllChanges()
	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	restored, _ := os.ReadFile(filename)
	if string(restored) != newContent {
		t.Errorf("Expected newContent %s, got: %s", newContent, string(restored))
	}

	// Status should be "restored".
	changes, _ := GetAllChanges()
	for _, change := range changes {
		if change.RequestHash == revisionID && change.Filename == filename {
			if change.Status != "restored" {
				t.Errorf("Expected status 'restored', got '%s'", change.Status)
			}
		}
	}
}

// TestRestoreProceedsWhenAtOriginalCode verifies that restore proceeds
// when the file on disk matches originalCode (rolled-back state).
// Restoring re-applies the agent's edit, which is safe: the only thing
// that changed the file was our own rollback.
func TestRestoreProceedsWhenAtOriginalCode(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("restore-orig-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "restore_at_orig.go"
	originalContent := "v1-original"
	newContent := "v2-edited"

	if err := RecordChangeWithDetails(revisionID, filename, originalContent, newContent, "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}

	// File on disk matches originalCode (was rolled back).
	if err := os.WriteFile(filename, []byte(originalContent), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	list, _ := GetAllChanges()
	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	restored, _ := os.ReadFile(filename)
	if string(restored) != newContent {
		t.Errorf("Expected newContent %s after restore, got: %s", newContent, string(restored))
	}
}

// TestRestoreMixedStaleAndFresh verifies that when a revision has both
// stale and non-stale files, only the non-stale ones are restored.
func TestRestoreMixedStaleAndFresh(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("restore-mixed-revision", "Update files", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	// File A: fresh (disk matches newCode)
	fileA := "fresh_restore.go"
	if err := RecordChangeWithDetails(revisionID, fileA, "old A", "new A", "update A", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails A failed: %v", err)
	}
	if err := os.WriteFile(fileA, []byte("new A"), 0600); err != nil {
		t.Fatalf("WriteFile A: %v", err)
	}

	// File B: stale (disk has external content)
	fileB := "stale_restore.go"
	if err := RecordChangeWithDetails(revisionID, fileB, "old B", "new B", "update B", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails B failed: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("external B"), 0600); err != nil {
		t.Fatalf("WriteFile B: %v", err)
	}

	list, _ := GetAllChanges()
	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	// File A should have been restored (newContent).
	contentA, _ := os.ReadFile(fileA)
	if string(contentA) != "new A" {
		t.Errorf("Fresh file A should be restored. Expected 'new A', got: %s", string(contentA))
	}

	// File B should NOT have been touched (still external).
	contentB, _ := os.ReadFile(fileB)
	if string(contentB) != "external B" {
		t.Errorf("Stale file B should NOT be restored. Expected 'external B', got: %s", string(contentB))
	}
}

// TestRestoreSkipsWhenFileDeleted verifies that restore proceeds
// (re-creates the file) when the file doesn't exist on disk.
func TestRestoreSkipsWhenFileDeleted(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("restore-deleted-revision", "Update file", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "restore_deleted.go"
	if err := RecordChangeWithDetails(revisionID, filename, "original", "new content", "update", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails failed: %v", err)
	}

	// File doesn't exist on disk (was deleted after snapshot).

	list, _ := GetAllChanges()
	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	// File should have been re-created with newCode.
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("File should have been restored (created), but ReadFile failed: %v", err)
	}
	if string(restored) != "new content" {
		t.Errorf("Expected 'new content', got: %s", string(restored))
	}
}

// TestIsFileStaleForRestoreHelper directly tests the helper across all
// its decision branches.
func TestIsFileStaleForRestoreHelper(t *testing.T) {
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
		if err := os.WriteFile("empty.txt", []byte("whatever"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStaleForRestore("empty.txt", "orig", "") {
			t.Error("should return false for empty newCode")
		}
	})

	t.Run("RedactedMarkerNewCode_ReturnsFalse", func(t *testing.T) {
		if err := os.WriteFile("redacted.txt", []byte("whatever"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStaleForRestore("redacted.txt", "orig", RedactedContentMarker) {
			t.Error("should return false for redacted marker newCode")
		}
	})

	t.Run("FileDoesNotExist_ReturnsFalse", func(t *testing.T) {
		if isFileStaleForRestore("nonexistent.txt", "orig", "new") {
			t.Error("should return false when file doesn't exist")
		}
	})

	t.Run("FileMatchesOriginalCode_ReturnsFalse", func(t *testing.T) {
		if err := os.WriteFile("at_orig.txt", []byte("orig"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStaleForRestore("at_orig.txt", "orig", "new") {
			t.Error("should return false when file matches originalCode")
		}
	})

	t.Run("FileMatchesNewCode_ReturnsFalse", func(t *testing.T) {
		if err := os.WriteFile("at_new.txt", []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		if isFileStaleForRestore("at_new.txt", "orig", "new") {
			t.Error("should return false when file matches newCode")
		}
	})

	t.Run("FileMatchesNeither_ReturnsTrue", func(t *testing.T) {
		if err := os.WriteFile("neither.txt", []byte("external"), 0600); err != nil {
			t.Fatal(err)
		}
		if !isFileStaleForRestore("neither.txt", "orig", "new") {
			t.Error("should return true when file matches neither original nor new")
		}
	})
}

// ---------------------------------------------------------------------------
// Multi-edit regression tests
//
// When a file is edited multiple times in one session:
//   Edit 1: v0 → v1  (OriginalCode=v0, NewCode=v1)
//   Edit 2: v1 → v2  (OriginalCode=v1, NewCode=v2)
//
// The dedupChangesByFilename helper collapses these into one entry:
//   OriginalCode=v0 (earliest), NewCode=v2 (latest)
//
// Rollback must restore v0 (the true original), and the staleness check
// must compare disk against v2 (the latest NewCode), not v1.
// ---------------------------------------------------------------------------

func TestRollback_MultiEdit_SameFile(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("multi-edit-rollback-revision", "Multi-edit test", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "multi_edit.go"
	v0 := "v0-true-original"
	v1 := "v1-first-edit"
	v2 := "v2-second-edit"

	// Record two changes for the same file: v0→v1, then v1→v2.
	if err := RecordChangeWithDetails(revisionID, filename, v0, v1, "edit 1", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails edit 1 failed: %v", err)
	}
	if err := RecordChangeWithDetails(revisionID, filename, v1, v2, "edit 2", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails edit 2 failed: %v", err)
	}

	// Write the latest (v2) to disk — this is what the agent's second edit produced.
	if err := os.WriteFile(filename, []byte(v2), 0600); err != nil {
		t.Fatalf("Failed to write v2: %v", err)
	}

	// Rollback — dedup should produce one entry (OriginalCode=v0, NewCode=v2).
	// Staleness check: disk=v2 == NewCode=v2 → NOT stale → proceed.
	// Result: file should be restored to v0 (the TRUE original).
	if err := RevertChangeByRevisionID(revisionID); err != nil {
		t.Fatalf("RevertChangeByRevisionID failed: %v", err)
	}

	// File should contain v0 — not v1 (an intermediate state).
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file after rollback: %v", err)
	}
	if string(restored) != v0 {
		t.Errorf("Multi-edit rollback should restore to TRUE original v0. Expected: %s, Got: %s",
			v0, string(restored))
	}
}

func TestRestore_MultiEdit_SameFile(t *testing.T) {
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

	revisionID, err := RecordBaseRevision("multi-edit-restore-revision", "Multi-edit test", "Response", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision failed: %v", err)
	}

	filename := "multi_edit_restore.go"
	v0 := "v0-true-original"
	v1 := "v1-first-edit"
	v2 := "v2-second-edit"

	// Record two changes for the same file: v0→v1, then v1→v2.
	if err := RecordChangeWithDetails(revisionID, filename, v0, v1, "edit 1", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails edit 1 failed: %v", err)
	}
	if err := RecordChangeWithDetails(revisionID, filename, v1, v2, "edit 2", "", "", "", "test-model"); err != nil {
		t.Fatalf("RecordChangeWithDetails edit 2 failed: %v", err)
	}

	// Simulate: file was rolled back to v0 (the original).
	// Restoring should re-apply the agent's edits to get back to v2.
	if err := os.WriteFile(filename, []byte(v0), 0600); err != nil {
		t.Fatalf("Failed to write v0: %v", err)
	}

	// Fetch the revision group and call handleRevisionRestore.
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges failed: %v", err)
	}
	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 revision group, got %d", len(groups))
	}

	// Dedup produces one entry (OriginalCode=v0, NewCode=v2).
	// Staleness check: disk=v0 == OriginalCode=v0 → NOT stale → proceed.
	// Result: file should be restored to v2 (the latest intended state).
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore failed: %v", err)
	}

	// File should contain v2 — not v1 (an intermediate state).
	restored, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file after restore: %v", err)
	}
	if string(restored) != v2 {
		t.Errorf("Multi-edit restore should restore to LATEST state v2. Expected: %s, Got: %s",
			v2, string(restored))
	}
}
