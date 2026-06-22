package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGroupChangesByRevisionAndActive(t *testing.T) {
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	changes := []ChangeLog{
		{RequestHash: "r1", FileRevisionHash: "f1", Filename: "a.txt", Status: "active", Timestamp: t1},
		{RequestHash: "r1", FileRevisionHash: "f2", Filename: "b.txt", Status: "reverted", Timestamp: t2},
		{RequestHash: "r2", FileRevisionHash: "f3", Filename: "c.txt", Status: "active", Timestamp: t2},
	}
	groups := groupChangesByRevision(changes)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Ensure groups are sorted by timestamp desc (r2 first)
	if groups[0].RevisionID != "r2" {
		t.Fatalf("expected r2 first, got %s", groups[0].RevisionID)
	}
	// getActiveChanges returns only active entries
	act := getActiveChanges(groups[1].Changes)
	if len(act) != 1 || act[0].Filename != "a.txt" {
		t.Fatalf("unexpected active set: %v", act)
	}
}

func TestSortChangesByTimestamp(t *testing.T) {
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	arr := []ChangeLog{
		{Timestamp: t1}, {Timestamp: t2}, {Timestamp: t1},
	}
	sortChangesByTimestamp(arr)
	if !(arr[0].Timestamp.Equal(t2) && arr[1].Timestamp.Equal(t1)) {
		t.Fatalf("expected sorted desc by timestamp, got %v", arr)
	}
}

func TestRecordAndFetchChanges_Roundtrip(t *testing.T) {
	// Run in isolated temp working dir
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create base revision and a change
	revID, err := RecordBaseRevision("req1", "do x", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}
	if revID == "" {
		t.Fatalf("expected non-empty revision id")
	}

	if err := RecordChangeWithDetails(revID, "file.go", "old", "new", "desc", "note", "prompt", "llm-msg", "model-x"); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Fetch and validate
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}
	if list[0].Filename != "file.go" || list[0].Description != "desc" || list[0].OriginalCode != "old" || list[0].NewCode != "new" {
		t.Fatalf("unexpected change data: %+v", list[0])
	}

	// Update status and verify via underlying file
	if err := updateChangeStatus(list[0].FileRevisionHash, "reverted"); err != nil {
		t.Fatalf("updateChangeStatus: %v", err)
	}
	metaPath := filepath.Join(".sprout/changes", list[0].FileRevisionHash, "metadata.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if string(b) == "" {
		t.Fatalf("metadata unexpectedly empty")
	}
}

// ---------------------------------------------------------------------------
// Redaction-specific rollback/restore tests
// ---------------------------------------------------------------------------

func TestRollbackRedactedFile_SkipsWrite(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with a redacted file
	revID, err := RecordBaseRevision("redact-rollback", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// Record a change with redacted content
	if err := RecordChangeWithDetails(revID, "/tmp/external.txt", RedactedContentMarker, "new content", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Fetch the change and build a RevisionGroup for rollback
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Run rollback — should skip the redacted file without error
	if err := handleRevisionRollback(groups[0]); err != nil {
		t.Fatalf("handleRevisionRollback should not error on redacted file: %v", err)
	}

	// Verify the file was NOT written (rollback was skipped)
	if _, err := os.Stat("/tmp/external.txt"); err == nil {
		// File exists — might be pre-existing, check content
		content, _ := os.ReadFile("/tmp/external.txt")
		if string(content) == RedactedContentMarker {
			t.Errorf("rollback should not write redacted content to disk")
		}
	}
}

func TestRestoreRedactedFile_SkipsWrite(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with a redacted file
	revID, err := RecordBaseRevision("redact-restore", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// Record a change with redacted new content
	if err := RecordChangeWithDetails(revID, "/tmp/external2.txt", "old content", RedactedContentMarker, "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Fetch the change and build a RevisionGroup for restore
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Run restore — should skip the redacted file without error
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore should not error on redacted file: %v", err)
	}

	// Verify the file was NOT written with redacted content
	if _, err := os.Stat("/tmp/external2.txt"); err == nil {
		content, _ := os.ReadFile("/tmp/external2.txt")
		if string(content) == RedactedContentMarker {
			t.Errorf("restore should not write redacted content to disk")
		}
	}
}

func TestRollbackNormalFile_WritesOriginal(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with normal content
	revID, err := RecordBaseRevision("normal-rollback", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	originalContent := "func main() { println(\"hello\") }"
	newContent := "func main() { println(\"world\") }"
	filePath := "normal_file.go"

	if err := RecordChangeWithDetails(revID, filePath, originalContent, newContent, "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Create the file with the "new" content so rollback can restore it
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Fetch and rollback
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if err := handleRevisionRollback(groups[0]); err != nil {
		t.Fatalf("handleRevisionRollback: %v", err)
	}

	// Verify the file was restored to original content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile after rollback: %v", err)
	}
	if string(content) != originalContent {
		t.Errorf("file content after rollback = %q, want %q", string(content), originalContent)
	}
}

func TestRestoreNormalFile_WritesNewContent(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with normal content
	revID, err := RecordBaseRevision("normal-restore", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	originalContent := "func main() { println(\"hello\") }"
	newContent := "func main() { println(\"world\") }"
	filePath := "restore_file.go"

	if err := RecordChangeWithDetails(revID, filePath, originalContent, newContent, "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Create the file with original content so restore can overwrite it
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Fetch and restore
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore: %v", err)
	}

	// Verify the file was restored to new content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile after restore: %v", err)
	}
	if string(content) != newContent {
		t.Errorf("file content after restore = %q, want %q", string(content), newContent)
	}
}

func TestRollbackMixedRedactedAndNormal(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with both redacted and normal changes
	revID, err := RecordBaseRevision("mixed-rollback", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// Normal change
	normalFile := "normal.go"
	if err := RecordChangeWithDetails(revID, normalFile, "original", "updated", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails (normal): %v", err)
	}
	if err := os.WriteFile(normalFile, []byte("updated"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Redacted change
	if err := RecordChangeWithDetails(revID, "/tmp/secret.txt", RedactedContentMarker, RedactedContentMarker, "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails (redacted): %v", err)
	}

	// Fetch and rollback
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if err := handleRevisionRollback(groups[0]); err != nil {
		t.Fatalf("handleRevisionRollback: %v", err)
	}

	// Normal file should be restored
	content, err := os.ReadFile(normalFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("normal file after rollback = %q, want %q", string(content), "original")
	}
}

func TestRestoreMixedRedactedAndNormal(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a revision with both redacted and normal changes
	revID, err := RecordBaseRevision("mixed-restore", "test", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}

	// Normal change
	normalFile := "normal2.go"
	if err := RecordChangeWithDetails(revID, normalFile, "original", "updated", "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails (normal): %v", err)
	}
	if err := os.WriteFile(normalFile, []byte("original"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Redacted change
	if err := RecordChangeWithDetails(revID, "/tmp/secret2.txt", RedactedContentMarker, RedactedContentMarker, "edit", "", "", "", ""); err != nil {
		t.Fatalf("RecordChangeWithDetails (redacted): %v", err)
	}

	// Fetch and restore
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(list))
	}

	groups := groupChangesByRevision(list)
	if err := handleRevisionRestore(groups[0]); err != nil {
		t.Fatalf("handleRevisionRestore: %v", err)
	}

	// Normal file should be restored to new content
	content, err := os.ReadFile(normalFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "updated" {
		t.Errorf("normal file after restore = %q, want %q", string(content), "updated")
	}
}

// TestRedactedContentMarkerValue guards against accidental edits to the marker
// string. Rollback/restore guards and write-site guards all key off this exact
// value; changing it would silently break the redaction defense. If you
// intentionally change the marker, update every comparison and this test.
func TestRedactedContentMarkerValue(t *testing.T) {
	const want = "[REDACTED - external file]"
	if RedactedContentMarker != want {
		t.Errorf("RedactedContentMarker = %q, want %q (changing this breaks redaction guards)", RedactedContentMarker, want)
	}
}
