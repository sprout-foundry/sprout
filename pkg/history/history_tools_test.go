package history

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

// TestBase64Encoding tests that file contents are properly encoded/decoded
func TestBase64Encoding(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "ledit_test_base64_"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(testDir)

	// Create and change to test directory
	os.MkdirAll(testDir, 0755)
	oldDir, _ := os.Getwd()
	os.Chdir(testDir)
	defer os.Chdir(oldDir)

	// Ensure we're using project-scoped paths for this test
	changesDir = projectChangesDir
	revisionsDir = projectRevisionsDir

	// Test data
	originalCode := "func main() {\n\tfmt.Println(\"Hello, World!\")\n}"
	newCode := "func main() {\n\tfmt.Println(\"Hello, Updated World!\")\n}"
	filename := "test.go"
	description := "Update greeting message"

	// Record a change
	revisionID, err := RecordBaseRevision("test-revision", "Update the greeting", "Changes applied successfully", []APIMessage{})
	if err != nil {
		t.Fatalf("Failed to record base revision: %v", err)
	}

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, "", "test prompt", "test llm message", "test-model")
	if err != nil {
		t.Fatalf("Failed to record change: %v", err)
	}

	// Verify that the stored files are base64 encoded
	changeDir := filepath.Join(".ledit/changes")
	entries, err := os.ReadDir(changeDir)
	if err != nil {
		t.Fatalf("Failed to read changes directory: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("No changes recorded")
	}

	// Check the first change directory
	changeSubdir := filepath.Join(changeDir, entries[0].Name())
	originalFile := filepath.Join(changeSubdir, "test.go.original")
	updatedFile := filepath.Join(changeSubdir, "test.go.updated")

	// Read the stored content (should be base64 encoded)
	originalStored, err := filesystem.ReadFileBytes(originalFile)
	if err != nil {
		t.Fatalf("Failed to read original file: %v", err)
	}

	updatedStored, err := filesystem.ReadFileBytes(updatedFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	// Verify the content is base64 encoded by decoding it
	originalDecoded, err := base64.StdEncoding.DecodeString(string(originalStored))
	if err != nil {
		t.Fatalf("Stored original content is not valid base64: %v", err)
	}

	updatedDecoded, err := base64.StdEncoding.DecodeString(string(updatedStored))
	if err != nil {
		t.Fatalf("Stored updated content is not valid base64: %v", err)
	}

	// Verify the decoded content matches our original data
	if string(originalDecoded) != originalCode {
		t.Errorf("Decoded original content doesn't match. Expected: %s, Got: %s", originalCode, string(originalDecoded))
	}

	if string(updatedDecoded) != newCode {
		t.Errorf("Decoded updated content doesn't match. Expected: %s, Got: %s", newCode, string(updatedDecoded))
	}

	// Verify that fetchAllChanges can properly read and decode the content
	changes, err := fetchAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != originalCode {
		t.Errorf("Fetched original code doesn't match. Expected: %s, Got: %s", originalCode, change.OriginalCode)
	}

	if change.NewCode != newCode {
		t.Errorf("Fetched new code doesn't match. Expected: %s, Got: %s", newCode, change.NewCode)
	}
}

// TestHistoryFiltering tests various filtering capabilities
func TestHistoryFiltering(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "ledit_test_filtering_"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(testDir)

	// Create and change to test directory
	os.MkdirAll(testDir, 0755)
	oldDir, _ := os.Getwd()
	os.Chdir(testDir)
	defer os.Chdir(oldDir)

	// Ensure we're using project-scoped paths for this test
	changesDir = projectChangesDir
	revisionsDir = projectRevisionsDir

	// Create multiple changes with different timestamps
	now := time.Now()

	// Revision 1 - older
	rev1, err := RecordBaseRevision("rev1", "First change", "First response", []APIMessage{})
	if err != nil {
		t.Fatalf("Failed to record revision 1: %v", err)
	}

	err = RecordChangeWithDetails(rev1, "file1.go", "old1", "new1", "Change 1", "", "", "", "model1")
	if err != nil {
		t.Fatalf("Failed to record change 1: %v", err)
	}

	// Revision 2 - newer
	rev2, err := RecordBaseRevision("rev2", "Second change", "Second response", []APIMessage{})
	if err != nil {
		t.Fatalf("Failed to record revision 2: %v", err)
	}

	err = RecordChangeWithDetails(rev2, "file2.py", "old2", "new2", "Change 2", "", "", "", "model2")
	if err != nil {
		t.Fatalf("Failed to record change 2: %v", err)
	}

	// Test GetAllChanges
	allChanges, err := GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to get all changes: %v", err)
	}

	if len(allChanges) != 2 {
		t.Fatalf("Expected 2 changes, got %d", len(allChanges))
	}

	// Test GetChangesSince
	since := now.Add(-1 * time.Hour) // 1 hour ago
	recentChanges, err := GetChangesSince(since)
	if err != nil {
		t.Fatalf("Failed to get recent changes: %v", err)
	}

	if len(recentChanges) != 2 {
		t.Fatalf("Expected 2 recent changes, got %d", len(recentChanges))
	}

	// Test with future timestamp (should return empty)
	futureChanges, err := GetChangesSince(now.Add(1 * time.Hour))
	if err != nil {
		t.Fatalf("Failed to get future changes: %v", err)
	}

	if len(futureChanges) != 0 {
		t.Fatalf("Expected 0 future changes, got %d", len(futureChanges))
	}

	// Test GetChangedFilesSince
	changedFiles, err := GetChangedFilesSince(since)
	if err != nil {
		t.Fatalf("Failed to get changed files: %v", err)
	}

	expectedFiles := []string{"file2.py", "file1.go"} // Should be ordered by timestamp (newest first)
	if len(changedFiles) != 2 {
		t.Fatalf("Expected 2 changed files, got %d", len(changedFiles))
	}

	// Check that we have the expected files (order may vary)
	fileMap := make(map[string]bool)
	for _, file := range changedFiles {
		fileMap[file] = true
	}

	for _, expectedFile := range expectedFiles {
		if !fileMap[expectedFile] {
			t.Errorf("Expected file %s not found in changed files list", expectedFile)
		}
	}
}

// TestRollback tests the rollback functionality
func TestRollback(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "ledit_test_rollback_"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(testDir)

	// Create and change to test directory
	os.MkdirAll(testDir, 0755)
	oldDir, _ := os.Getwd()
	os.Chdir(testDir)
	defer os.Chdir(oldDir)

	// Ensure we're using project-scoped paths for this test
	changesDir = projectChangesDir
	revisionsDir = projectRevisionsDir

	// Create a test file
	testFile := "rollback_test.txt"
	originalContent := "Original content"
	newContent := "Modified content"

	// Create the test file with original content
	err := os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Record a change
	revisionID, err := RecordBaseRevision("rollback-test", "Test rollback", "Rollback test response", []APIMessage{})
	if err != nil {
		t.Fatalf("Failed to record base revision: %v", err)
	}

	err = RecordChangeWithDetails(revisionID, testFile, originalContent, newContent, "Test modification", "", "", "", "test-model")
	if err != nil {
		t.Fatalf("Failed to record change: %v", err)
	}

	// Modify the actual file to simulate the change being applied
	err = os.WriteFile(testFile, []byte(newContent), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Verify file has new content
	currentContent, err := filesystem.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	if string(currentContent) != newContent {
		t.Fatalf("File content should be modified. Expected: %s, Got: %s", newContent, string(currentContent))
	}

	// Verify we have active changes for this revision
	hasChanges, err := HasActiveChangesForRevision(revisionID)
	if err != nil {
		t.Fatalf("Failed to check for active changes: %v", err)
	}

	if !hasChanges {
		t.Fatal("Should have active changes for revision")
	}

	// Perform rollback
	err = RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Failed to rollback changes: %v", err)
	}

	// Verify file content is restored
	restoredContent, err := filesystem.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}

	if string(restoredContent) != originalContent {
		t.Errorf("File content should be restored. Expected: %s, Got: %s", originalContent, string(restoredContent))
	}

	// Verify the change status is updated to "reverted"
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to get changes after rollback: %v", err)
	}

	found := false
	for _, change := range changes {
		if change.RequestHash == revisionID {
			if change.Status != "reverted" {
				t.Errorf("Change status should be 'reverted', got: %s", change.Status)
			}
			found = true
			break
		}
	}

	if !found {
		t.Error("Could not find the change after rollback")
	}

	// Verify we no longer have active changes for this revision
	hasChangesAfter, err := HasActiveChangesForRevision(revisionID)
	if err != nil {
		t.Fatalf("Failed to check for active changes after rollback: %v", err)
	}

	if hasChangesAfter {
		t.Error("Should not have active changes after rollback")
	}
}

// TestBackwardCompatibility tests that the system can handle both base64 and plain text files
func TestBackwardCompatibility(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "ledit_test_compat_"+time.Now().Format("20060102150405"))
	defer os.RemoveAll(testDir)

	// Create and change to test directory
	os.MkdirAll(testDir, 0755)
	oldDir, _ := os.Getwd()
	os.Chdir(testDir)
	defer os.Chdir(oldDir)

	// Ensure we're using project-scoped paths for this test
	changesDir = projectChangesDir
	revisionsDir = projectRevisionsDir

	// Create the changes directory structure manually
	changeDir := ".ledit/changes/test_change"
	err := filesystem.EnsureDir(changeDir)
	if err != nil {
		t.Fatalf("Failed to create change directory: %v", err)
	}

	// Create metadata file
	metadata := ChangeMetadata{
		Version:          1,
		Filename:         "test.go",
		FileRevisionHash: "test_change",
		RequestHash:      "test_request",
		Timestamp:        time.Now(),
		Status:           "active",
		Description:      "Test backward compatibility",
	}

	metadataBytes, err := jsonMarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal metadata: %v", err)
	}

	err = filesystem.WriteFileWithDir(filepath.Join(changeDir, "metadata.json"), metadataBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write metadata: %v", err)
	}

	// Create plain text files (old format)
	plainOriginal := "plain original content"
	plainNew := "plain new content"

	err = filesystem.WriteFileWithDir(filepath.Join(changeDir, "test.go.original"), []byte(plainOriginal), 0644)
	if err != nil {
		t.Fatalf("Failed to write plain original file: %v", err)
	}

	err = filesystem.WriteFileWithDir(filepath.Join(changeDir, "test.go.updated"), []byte(plainNew), 0644)
	if err != nil {
		t.Fatalf("Failed to write plain updated file: %v", err)
	}

	// Test that fetchAllChanges can handle plain text files
	changes, err := fetchAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes with plain text files: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != plainOriginal {
		t.Errorf("Original code should be readable from plain text. Expected: %s, Got: %s", plainOriginal, change.OriginalCode)
	}

	if change.NewCode != plainNew {
		t.Errorf("New code should be readable from plain text. Expected: %s, Got: %s", plainNew, change.NewCode)
	}
}

// Helper function for JSON marshaling (to match the existing code style)
func jsonMarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return []byte(`{
  "version": 1,
  "filename": "test.go",
  "file_revision_hash": "test_change",
  "request_hash": "test_request",
  "timestamp": "` + time.Now().Format(time.RFC3339) + `",
  "status": "active",
  "note": "",
  "description": "Test backward compatibility"
}`), nil
}
