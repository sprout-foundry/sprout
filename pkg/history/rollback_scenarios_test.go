package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRollbackMultipleFiles tests rolling back a revision with multiple file changes
func TestRollbackMultipleFiles(t *testing.T) {
	// Setup test directory using t.TempDir() for automatic cleanup
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

	// Create revision with multiple files
	revisionID, _ := RecordBaseRevision("multi-revision", "Modify multiple files", "LLM response", []APIMessage{})

	files := map[string]string{
		"file1.go":         "original content 1",
		"file2.py":         "original content 2",
		"file3.txt":        "original content 3",
		"deep/dir/file.go": "original content deep",
	}

	// Write original files and track changes
	for f, content := range files {
		ensureDirs(t, f)
		os.WriteFile(f, []byte(content), 0600)

		newContent := "updated " + content
		RecordChangeWithDetails(revisionID, f, content, newContent, "update "+f, "", "", "", "test-model")

		// Simulate tool modifying the file
		os.WriteFile(f, []byte(newContent), 0600)
	}

	// Verify files are modified
	for f, originalContent := range files {
		afterRead, _ := os.ReadFile(f)
		if string(afterRead) != "updated "+originalContent {
			t.Fatalf("File %s should be modified before rollback", f)
		}
	}

	// Perform rollback
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify all files are restored to original state
	for f, originalContent := range files {
		restoredContent, _ := os.ReadFile(f)
		if string(restoredContent) != originalContent {
			t.Errorf("File %s not restored correctly. Expected: %s, Got: %s",
				f, originalContent, string(restoredContent))
		}
	}

	// Verify all changes are marked as reverted
	changes, _ := GetAllChanges()
	for _, change := range changes {
		if change.RequestHash == revisionID && change.Status != "reverted" {
			t.Errorf("Change status should be reverted, got: %s for file %s", change.Status, change.Filename)
		}
	}
}

// TestRollbackWithLargeFiles tests rollback with large file contents
func TestRollbackWithLargeFiles(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("large-revision", "Large file update", "Response", []APIMessage{})

	// Create a large original file (10KB)
	largeOriginal := string(make([]byte, 10240))
	for i := range largeOriginal {
		largeOriginal = largeOriginal[:i] + "a" + largeOriginal[i+1:]
	}
	filename := "large.go"
	os.WriteFile(filename, []byte(largeOriginal), 0600)

	// Modify to create large new file (15KB)
	largeNew := string(make([]byte, 15360))
	for i := range largeNew {
		largeNew = largeNew[:i] + "b" + largeNew[i+1:]
	}
	RecordChangeWithDetails(revisionID, filename, largeOriginal, largeNew, "Large file update", "", "", "", "test-model")
	os.WriteFile(filename, []byte(largeNew), 0600)

	// Verify modification
	current, _ := os.ReadFile(filename)
	if len(current) != 15360 {
		t.Fatalf("File should be 15KB, got %d bytes", len(current))
	}

	// Rollback
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify restoration
	restored, _ := os.ReadFile(filename)
	if string(restored) != largeOriginal {
		t.Errorf("Large file not restored correctly. Original length: %d, Restored length: %d",
			len(largeOriginal), len(restored))
	}
}

// TestRollbackWithSpecialCharacters tests rollback with files containing special characters
func TestRollbackWithSpecialCharacters(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("special-revision", "Special chars update", "Response", []APIMessage{})

	testCases := []struct {
		filename string
		original string
		modified string
	}{
		{"utf8.txt", "Hello 世界 👋", "Hello World 👋"},
		{"tabs.go", "func\tmain()\t{\n\tprintln()\n}", "func test() {\nlog()\n}"},
		{"mixed.js", "console.log(\"test\\nnewline\")", "console.log('changed')"},
		{"quotes.py", "print(\"test 'quoted'\")", "print('other \"quoted\"')"},
		{"null.bin", "\x00\x01\x02\x03", "\xFF\xFE\xFD\xFC"},
		{"emoji.md", "# 😮 Title\nText **bold**", "# 🎉 Changed\nText **italic**"},
	}

	for _, tc := range testCases {
		os.WriteFile(tc.filename, []byte(tc.original), 0600)
		RecordChangeWithDetails(revisionID, tc.filename, tc.original, tc.modified, "update", "", "", "", "test-model")
		os.WriteFile(tc.filename, []byte(tc.modified), 0600)
	}

	// Rollback all changes
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify all files restored correctly
	for _, tc := range testCases {
		restored, _ := os.ReadFile(tc.filename)
		if string(restored) != tc.original {
			t.Errorf("File %s not restored correctly. Expected: %q, Got: %q",
				tc.filename, tc.original, string(restored))
		}
	}
}

// TestRollbackAfterPartialState tests rollback when only some files were tracked
func TestRollbackAfterPartialState(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("partial-revision", "Partial changes", "Response", []APIMessage{})

	// File A: tracked with changes
	fileA := "tracked.go"
	os.WriteFile(fileA, []byte("original A"), 0600)
	RecordChangeWithDetails(revisionID, fileA, "original A", "modified A", "update", "", "", "", "test-model")
	os.WriteFile(fileA, []byte("modified A"), 0600)

	// File B: tracked but no changes
	fileB := "nochange.go"
	RecordChangeWithDetails(revisionID, fileB, "original B", "original B", "no change", "", "", "", "test-model")
	os.WriteFile(fileB, []byte("original B"), 0600)

	// Rollback
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// File A should be restored
	restoredA, _ := os.ReadFile(fileA)
	if string(restoredA) != "original A" {
		t.Errorf("File A not restored. Got: %s", string(restoredA))
	}

	// File B should still have original content
	restoredB, _ := os.ReadFile(fileB)
	if string(restoredB) != "original B" {
		t.Errorf("File B incorrect. Got: %s", string(restoredB))
	}
}

// TestMultipleRevisionsSameFile tests rollback when same file changes in multiple revisions
func TestMultipleRevisionsSameFile(t *testing.T) {
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

	filename := "config.txt"
	os.WriteFile(filename, []byte("v1"), 0600)

	// First revision
	rev1, _ := RecordBaseRevision("rev1", "Change to v2", "Response1", []APIMessage{})
	RecordChangeWithDetails(rev1, filename, "v1", "v2", "update to v2", "", "", "", "test-model")
	os.WriteFile(filename, []byte("v2"), 0600)

	// Second revision
	rev2, _ := RecordBaseRevision("rev2", "Change to v3", "Response2", []APIMessage{})
	RecordChangeWithDetails(rev2, filename, "v2", "v3", "update to v3", "", "", "", "test-model")
	os.WriteFile(filename, []byte("v3"), 0600)

	// Current state should be v3
	current, _ := os.ReadFile(filename)
	if string(current) != "v3" {
		t.Fatalf("Current should be v3, got: %s", string(current))
	}

	// Rollback rev2 (most recent) - should restore to v2
	err := RevertChangeByRevisionID(rev2)
	if err != nil {
		t.Fatalf("Rollback rev2 failed: %v", err)
	}
	restored, _ := os.ReadFile(filename)
	if string(restored) != "v2" {
		t.Errorf("After rollback, expected v2, got: %s", string(restored))
	}

	// Rollback rev1 (older, but still has active changes?)
	// rev1's change should still be active until reverted
	err = RevertChangeByRevisionID(rev1)
	if err != nil {
		t.Fatalf("Rollback rev1 failed: %v", err)
	}
	restored2, _ := os.ReadFile(filename)
	if string(restored2) != "v1" {
		t.Errorf("After second rollback, expected v1, got: %s", string(restored2))
	}
}

// TestRollbackEmptyFile tests handling of empty files
func TestRollbackEmptyFile(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("empty-revision", "Empty file test", "Response", []APIMessage{})

	filename := "empty.go"
	// Empty with content
	os.WriteFile(filename, []byte("some text"), 0600)
	RecordChangeWithDetails(revisionID, filename, "some text", "", "make empty", "", "", "", "test-model")
	os.WriteFile(filename, []byte{}, 0600)

	// Verify empty
	current, _ := os.ReadFile(filename)
	if len(current) != 0 {
		t.Fatalf("File should be empty, got %d bytes", len(current))
	}

	// Rollback
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Should restore content
	restored, _ := os.ReadFile(filename)
	if string(restored) != "some text" {
		t.Errorf("Empty file not restored. Expected 'some text', got: %q", string(restored))
	}
}

// TestRollbackBinaryFile tests rollback with binary content
func TestRollbackBinaryFile(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("binary-revision", "Binary file update", "Response", []APIMessage{})

	filename := "image.png"
	original := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	modified := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46} // JPEG header

	os.WriteFile(filename, original, 0600)
	RecordChangeWithDetails(revisionID, filename, string(original), string(modified), "change format", "", "", "", "test-model")
	os.WriteFile(filename, modified, 0600)

	// Rollback
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify binary content restored
	restored, _ := os.ReadFile(filename)
	for i, b := range original {
		if restored[i] != b {
			t.Errorf("Binary mismatch at byte %d. Expected: 0x%02X, Got: 0x%02X", i, b, restored[i])
		}
	}
}

// TestRollbackNonexistentFile tests rollback when original file didn't exist
func TestRollbackNonexistentFile(t *testing.T) {
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

	revisionID, _ := RecordBaseRevision("create-file", "Create new file", "Response", []APIMessage{})

	filename := "newfile.js"
	// File didn't exist originally (empty string for original)
	RecordChangeWithDetails(revisionID, filename, "", "new content", "create new file", "", "", "", "test-model")
	// Create the file (simulating tool action)
	os.WriteFile(filename, []byte("new content"), 0600)

	// Rollback - should delete the file since original was empty
	err := RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// File should still exist but with empty content (since we write empty string)
	content, _ := os.ReadFile(filename)
	if len(content) != 0 {
		t.Errorf("File should have empty content after rollback, got: %s", string(content))
	}
}

// Helper functions

func timestampSuffix() string {
	return time.Now().Format("20060102150405")
}

func ensureDirs(t *testing.T, filepathStr string) {
	// Extract directory part from filepath using standard library
	dir := filepath.Dir(filepathStr)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
}
