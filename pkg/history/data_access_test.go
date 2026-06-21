package history

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func setupHistoryDirs(t *testing.T) (changesDir, revisionsDir string) {
	t.Helper()
	tmp := t.TempDir()
	changesDir = filepath.Join(tmp, ".sprout", "changes")
	revisionsDir = filepath.Join(tmp, ".sprout", "revisions")
	os.MkdirAll(changesDir, 0755)
	os.MkdirAll(revisionsDir, 0755)
	setPathsForTesting(changesDir, revisionsDir)
	return
}

// createChangeDir creates a change entry with the given hash, revision ID, and timestamp.
func createChangeDir(t *testing.T, changesDir, hash, revisionID string, ts time.Time) {
	t.Helper()
	dir := filepath.Join(changesDir, hash)
	os.MkdirAll(dir, 0755)

	metadata := ChangeMetadata{
		Version:          metadataVersion,
		Filename:         "test.go",
		FileRevisionHash: hash,
		RequestHash:      revisionID,
		Timestamp:        ts,
		Status:           activeStatus,
		Description:      "test change",
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, metadataFile), data, 0644); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}
}

// createRevisionDir creates a revision directory with the given ID.
func createRevisionDir(t *testing.T, revisionsDir, id string) {
	t.Helper()
	dir := filepath.Join(revisionsDir, id)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "instructions.txt"), []byte("test instructions"), 0644)
}

// dirExists checks if a directory exists.
func dirExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// countDirs counts the number of subdirectories in a directory.
func countDirs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("failed to read dir %s: %v", dir, err)
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// ClearOlderThan
// ---------------------------------------------------------------------------

func TestClearOlderThan_RemovesOnlyOldEntries(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()
	oldThreshold := now.Add(-48 * time.Hour)

	// Create two old changes (before threshold)
	createChangeDir(t, changesDir, "old-change-1", "rev-1", oldThreshold)
	createChangeDir(t, changesDir, "old-change-2", "rev-1", oldThreshold.Add(-time.Hour))

	// Create one new change (after threshold)
	createChangeDir(t, changesDir, "new-change-1", "rev-2", now.Add(-1*time.Hour))

	// Create revision dirs
	createRevisionDir(t, revisionsDir, "rev-1")
	createRevisionDir(t, revisionsDir, "rev-2")

	// Clear entries older than 24 hours ago
	since := now.Add(-24 * time.Hour)
	changesCleared, revisionsCleared, err := ClearOlderThan("", since)
	if err != nil {
		t.Fatalf("ClearOlderThan returned error: %v", err)
	}

	// Two old changes should be cleared
	if changesCleared != 2 {
		t.Errorf("expected 2 changes cleared, got %d", changesCleared)
	}

	// rev-1 should be cleared (no remaining changes reference it)
	// rev-2 should remain (new-change-1 still references it)
	if revisionsCleared != 1 {
		t.Errorf("expected 1 revision cleared, got %d", revisionsCleared)
	}

	// Verify old change dirs are gone
	if dirExists(t, filepath.Join(changesDir, "old-change-1")) {
		t.Error("old-change-1 should have been removed")
	}
	if dirExists(t, filepath.Join(changesDir, "old-change-2")) {
		t.Error("old-change-2 should have been removed")
	}

	// Verify new change dir still exists
	if !dirExists(t, filepath.Join(changesDir, "new-change-1")) {
		t.Error("new-change-1 should still exist")
	}

	// Verify rev-1 is gone, rev-2 remains
	if dirExists(t, filepath.Join(revisionsDir, "rev-1")) {
		t.Error("rev-1 should have been removed (no remaining changes reference it)")
	}
	if !dirExists(t, filepath.Join(revisionsDir, "rev-2")) {
		t.Error("rev-2 should still exist (referenced by new-change-1)")
	}
}

func TestClearOlderThan_NoEntries(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Create empty dirs
	_, _, err := ClearOlderThan("", time.Now())
	if err != nil {
		t.Fatalf("expected no error with empty dirs, got: %v", err)
	}

	// Dirs should still exist (just empty)
	if countDirs(t, changesDir) != 0 {
		t.Errorf("expected 0 change dirs, got %d", countDirs(t, changesDir))
	}
	if countDirs(t, revisionsDir) != 0 {
		t.Errorf("expected 0 revision dirs, got %d", countDirs(t, revisionsDir))
	}
}

func TestClearOlderThan_AllEntriesOld(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	oldTime := time.Now().Add(-72 * time.Hour)

	// Create multiple old changes
	createChangeDir(t, changesDir, "old-1", "rev-1", oldTime)
	createChangeDir(t, changesDir, "old-2", "rev-2", oldTime.Add(-time.Hour))
	createChangeDir(t, changesDir, "old-3", "rev-1", oldTime.Add(-2*time.Hour))

	createRevisionDir(t, revisionsDir, "rev-1")
	createRevisionDir(t, revisionsDir, "rev-2")

	since := time.Now().Add(-24 * time.Hour)
	changesCleared, revisionsCleared, err := ClearOlderThan("", since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if changesCleared != 3 {
		t.Errorf("expected 3 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 2 {
		t.Errorf("expected 2 revisions cleared, got %d", revisionsCleared)
	}

	// All change dirs should be gone
	if countDirs(t, changesDir) != 0 {
		t.Errorf("expected 0 change dirs remaining, got %d", countDirs(t, changesDir))
	}
	if countDirs(t, revisionsDir) != 0 {
		t.Errorf("expected 0 revision dirs remaining, got %d", countDirs(t, revisionsDir))
	}
}

func TestClearOlderThan_NoEntriesOld(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()

	// All changes are recent
	createChangeDir(t, changesDir, "new-1", "rev-1", now.Add(-1*time.Hour))
	createChangeDir(t, changesDir, "new-2", "rev-2", now.Add(-30*time.Minute))

	createRevisionDir(t, revisionsDir, "rev-1")
	createRevisionDir(t, revisionsDir, "rev-2")

	// Clear entries older than 24 hours — none qualify
	since := now.Add(-24 * time.Hour)
	changesCleared, revisionsCleared, err := ClearOlderThan("", since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if changesCleared != 0 {
		t.Errorf("expected 0 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}

	// Both change dirs should remain
	if countDirs(t, changesDir) != 2 {
		t.Errorf("expected 2 change dirs remaining, got %d", countDirs(t, changesDir))
	}
	if countDirs(t, revisionsDir) != 2 {
		t.Errorf("expected 2 revision dirs remaining, got %d", countDirs(t, revisionsDir))
	}
}

func TestClearOlderThan_NonExistentChangesDir(t *testing.T) {
	changesDir, _ := setupHistoryDirs(t)

	// Remove the changes dir entirely
	os.RemoveAll(changesDir)

	changesCleared, revisionsCleared, err := ClearOlderThan("", time.Now())
	if err != nil {
		t.Fatalf("expected no error when changes dir doesn't exist, got: %v", err)
	}
	if changesCleared != 0 {
		t.Errorf("expected 0 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}
}

func TestClearOlderThan_SharedRevisionKept(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()
	oldTime := now.Add(-48 * time.Hour)

	// Two changes reference the same revision
	// One is old, one is new — the revision should be kept
	createChangeDir(t, changesDir, "old-change", "shared-rev", oldTime)
	createChangeDir(t, changesDir, "new-change", "shared-rev", now.Add(-1*time.Hour))

	// Another change references a different revision that's old
	createChangeDir(t, changesDir, "old-change-2", "orphan-rev", oldTime)

	createRevisionDir(t, revisionsDir, "shared-rev")
	createRevisionDir(t, revisionsDir, "orphan-rev")

	since := now.Add(-24 * time.Hour)
	changesCleared, revisionsCleared, err := ClearOlderThan("", since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if changesCleared != 2 {
		t.Errorf("expected 2 changes cleared, got %d", changesCleared)
	}

	// shared-rev should remain (referenced by new-change)
	// orphan-rev should be removed (only referenced by old-change-2 which was cleared)
	if revisionsCleared != 1 {
		t.Errorf("expected 1 revision cleared, got %d", revisionsCleared)
	}

	if !dirExists(t, filepath.Join(revisionsDir, "shared-rev")) {
		t.Error("shared-rev should still exist (referenced by remaining change)")
	}
	if dirExists(t, filepath.Join(revisionsDir, "orphan-rev")) {
		t.Error("orphan-rev should have been removed")
	}
}

func TestClearOlderThan_NonExistentRevisionsDir(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Remove the revisions dir entirely
	os.RemoveAll(revisionsDir)

	oldTime := time.Now().Add(-48 * time.Hour)
	createChangeDir(t, changesDir, "old-change", "rev-1", oldTime)

	since := time.Now().Add(-24 * time.Hour)
	changesCleared, revisionsCleared, err := ClearOlderThan("", since)
	if err != nil {
		t.Fatalf("expected no error when revisions dir doesn't exist, got: %v", err)
	}
	if changesCleared != 1 {
		t.Errorf("expected 1 change cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}
}

// ---------------------------------------------------------------------------
// ClearAll
// ---------------------------------------------------------------------------

func TestClearAll_RemovesEverything(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Create multiple changes and revisions
	createChangeDir(t, changesDir, "change-1", "rev-1", time.Now())
	createChangeDir(t, changesDir, "change-2", "rev-2", time.Now())
	createChangeDir(t, changesDir, "change-3", "rev-1", time.Now())

	createRevisionDir(t, revisionsDir, "rev-1")
	createRevisionDir(t, revisionsDir, "rev-2")

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("ClearAll returned error: %v", err)
	}

	if changesCleared != 3 {
		t.Errorf("expected 3 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 2 {
		t.Errorf("expected 2 revisions cleared, got %d", revisionsCleared)
	}

	if countDirs(t, changesDir) != 0 {
		t.Errorf("expected 0 change dirs remaining, got %d", countDirs(t, changesDir))
	}
	if countDirs(t, revisionsDir) != 0 {
		t.Errorf("expected 0 revision dirs remaining, got %d", countDirs(t, revisionsDir))
	}
}

func TestClearAll_NoEntries(t *testing.T) {
	_, _ = setupHistoryDirs(t)

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("expected no error with empty dirs, got: %v", err)
	}
	if changesCleared != 0 {
		t.Errorf("expected 0 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}
}

func TestClearAll_NonExistentChangesDir(t *testing.T) {
	_, revisionsDir := setupHistoryDirs(t)

	// Remove the changes dir
	changesDirPath := filepath.Join(filepath.Dir(revisionsDir), "changes")
	os.RemoveAll(changesDirPath)

	// Create a revision dir
	createRevisionDir(t, revisionsDir, "rev-1")

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("expected no error when changes dir doesn't exist, got: %v", err)
	}
	if changesCleared != 0 {
		t.Errorf("expected 0 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 1 {
		t.Errorf("expected 1 revision cleared, got %d", revisionsCleared)
	}
}

func TestClearAll_NonExistentRevisionsDir(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Remove the revisions dir
	os.RemoveAll(revisionsDir)

	createChangeDir(t, changesDir, "change-1", "rev-1", time.Now())

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("expected no error when revisions dir doesn't exist, got: %v", err)
	}
	if changesCleared != 1 {
		t.Errorf("expected 1 change cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}
}

func TestClearAll_NonExistentBothDirs(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Remove both dirs
	os.RemoveAll(changesDir)
	os.RemoveAll(revisionsDir)

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("expected no error when both dirs don't exist, got: %v", err)
	}
	if changesCleared != 0 {
		t.Errorf("expected 0 changes cleared, got %d", changesCleared)
	}
	if revisionsCleared != 0 {
		t.Errorf("expected 0 revisions cleared, got %d", revisionsCleared)
	}
}

func TestClearAll_SkipsNonDirectoryEntries(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	// Create a file (not a directory) in the changes dir
	os.WriteFile(filepath.Join(changesDir, "not-a-dir.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(revisionsDir, "not-a-dir.txt"), []byte("hello"), 0644)

	createChangeDir(t, changesDir, "change-1", "rev-1", time.Now())
	createRevisionDir(t, revisionsDir, "rev-1")

	changesCleared, revisionsCleared, err := ClearAll("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changesCleared != 1 {
		t.Errorf("expected 1 change cleared, got %d", changesCleared)
	}
	if revisionsCleared != 1 {
		t.Errorf("expected 1 revision cleared, got %d", revisionsCleared)
	}

	// The non-directory files should still be there
	if _, err := os.Stat(filepath.Join(changesDir, "not-a-dir.txt")); os.IsNotExist(err) {
		t.Error("non-directory file in changes dir should not be removed")
	}
	if _, err := os.Stat(filepath.Join(revisionsDir, "not-a-dir.txt")); os.IsNotExist(err) {
		t.Error("non-directory file in revisions dir should not be removed")
	}
}

// ---------------------------------------------------------------------------
// H2: Metadata-only scan (GetAllChangesMetadata)
// ---------------------------------------------------------------------------

// createChangeDirWithContent creates a change entry including the
// .original/.updated content files (base64-encoded). The default
// createChangeDir helper only writes metadata.json.
func createChangeDirWithContent(t *testing.T, changesDir, hash, revisionID, filename, originalCode, newCode string, ts time.Time) {
	t.Helper()
	dir := filepath.Join(changesDir, hash)
	os.MkdirAll(dir, 0755)

	metadata := ChangeMetadata{
		Version:          metadataVersion,
		Filename:         filename,
		FileRevisionHash: hash,
		RequestHash:      revisionID,
		Timestamp:        ts,
		Status:           activeStatus,
		Description:      "test change with content",
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, metadataFile), data, 0644); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	// Write content files (base64-encoded, as RecordChangeWithDetails does).
	safeFilename := strings.ReplaceAll(filename, "/", "_")
	safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")
	origEncoded := base64.StdEncoding.EncodeToString([]byte(originalCode))
	newEncoded := base64.StdEncoding.EncodeToString([]byte(newCode))
	os.WriteFile(filepath.Join(dir, safeFilename+originalSuffix), []byte(origEncoded), 0644)
	os.WriteFile(filepath.Join(dir, safeFilename+updatedSuffix), []byte(newEncoded), 0644)
}

// TestGetAllChangesMetadata_ReturnsMetadataWithoutContent (H2) verifies
// that the metadata-only scan returns the correct count and metadata
// fields, and that it does NOT fail when content files are missing.
func TestGetAllChangesMetadata_ReturnsMetadataWithoutContent(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()
	revID := "meta-rev-1"
	createRevisionDir(t, revisionsDir, revID)

	// Create 3 change entries with content.
	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("file_%d.go", i)
		hash := fmt.Sprintf("hash-%d", i)
		createChangeDirWithContent(t, changesDir, hash, revID, filename, "old code", "new code", now.Add(-time.Duration(i)*time.Minute))
	}

	// Fetch via metadata-only scan.
	results, err := GetAllChangesMetadata()
	if err != nil {
		t.Fatalf("GetAllChangesMetadata: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(results))
	}

	// Verify metadata fields are correct.
	for _, ch := range results {
		if ch.RequestHash != revID {
			t.Errorf("RequestHash = %q, want %q", ch.RequestHash, revID)
		}
		if ch.Status != activeStatus {
			t.Errorf("Status = %q, want %q", ch.Status, activeStatus)
		}
		if !strings.HasPrefix(ch.Filename, "file_") || !strings.HasSuffix(ch.Filename, ".go") {
			t.Errorf("unexpected Filename: %q", ch.Filename)
		}
		// The sentinels should be non-empty (content files exist).
		if ch.OriginalCode == "" {
			t.Errorf("OriginalCode sentinel should be non-empty when .original exists, for %q", ch.Filename)
		}
		if ch.NewCode == "" {
			t.Errorf("NewCode sentinel should be non-empty when .updated exists, for %q", ch.Filename)
		}
		// The sentinels must NOT be the actual decoded content.
		if ch.OriginalCode == "old code" {
			t.Errorf("OriginalCode should be a sentinel, not decoded content, for %q", ch.Filename)
		}
	}

	// Results should be sorted by timestamp descending (most recent first).
	for i := 1; i < len(results); i++ {
		if results[i].Timestamp.After(results[i-1].Timestamp) {
			t.Errorf("results not sorted by timestamp descending at index %d", i)
		}
	}
}

// TestGetAllChangesMetadata_WorksWhenContentFilesMissing (H2) verifies
// that the metadata-only scan returns entries even when .original/
// .updated files are absent. The full fetchAllChanges would SKIP such
// entries (it treats missing content as a hard error); the metadata
// scan treats it as "content not available" and reports the presence
// via empty OriginalCode/NewCode sentinels.
func TestGetAllChangesMetadata_WorksWhenContentFilesMissing(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()
	revID := "meta-rev-2"
	createRevisionDir(t, revisionsDir, revID)

	// Create a change entry WITHOUT content files (only metadata).
	createChangeDir(t, changesDir, "no-content-hash", revID, now)

	results, err := GetAllChangesMetadata()
	if err != nil {
		t.Fatalf("GetAllChangesMetadata: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(results))
	}

	ch := results[0]
	if ch.OriginalCode != "" {
		t.Errorf("OriginalCode should be empty when .original is missing, got %q", ch.OriginalCode)
	}
	if ch.NewCode != "" {
		t.Errorf("NewCode should be empty when .updated is missing, got %q", ch.NewCode)
	}
	// Metadata fields should still be present.
	if ch.Filename != "test.go" {
		t.Errorf("Filename = %q, want %q", ch.Filename, "test.go")
	}
	if ch.RequestHash != revID {
		t.Errorf("RequestHash = %q, want %q", ch.RequestHash, revID)
	}
}

// TestGetAllChangesMetadata_MixedContentPresence (H2) verifies correct
// sentinel behavior when some entries have content and some don't.
func TestGetAllChangesMetadata_MixedContentPresence(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	now := time.Now()
	revID := "meta-rev-3"
	createRevisionDir(t, revisionsDir, revID)

	// Entry WITH content.
	createChangeDirWithContent(t, changesDir, "with-content", revID, "with.go", "orig", "upd", now)
	// Entry WITHOUT content (metadata-only on disk).
	createChangeDir(t, changesDir, "without-content", revID, now.Add(-time.Minute))

	results, err := GetAllChangesMetadata()
	if err != nil {
		t.Fatalf("GetAllChangesMetadata: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(results))
	}

	// Find both entries.
	var withContent, withoutContent *ChangeLog
	for i := range results {
		switch results[i].FileRevisionHash {
		case "with-content":
			withContent = &results[i]
		case "without-content":
			withoutContent = &results[i]
		}
	}
	if withContent == nil || withoutContent == nil {
		t.Fatalf("expected to find both entries, got: %+v", results)
	}

	// Entry with content should have non-empty sentinels.
	if withContent.OriginalCode == "" {
		t.Error("entry with content should have non-empty OriginalCode sentinel")
	}
	// Entry without content should have empty sentinels.
	if withoutContent.OriginalCode != "" {
		t.Error("entry without content should have empty OriginalCode")
	}
}
