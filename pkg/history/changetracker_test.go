package history

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// ---------------------------------------------------------------------------
// formatRevision
// ---------------------------------------------------------------------------

// TestFormatRevision_BasicFields verifies that formatRevision includes
// the revision ID, timestamp, model, filename, and status in its output.
func TestFormatRevision_BasicFields(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	group := RevisionGroup{
		RevisionID: "rev-format-1",
		Timestamp:  ts,
		AgentModel: "test-model-v2",
		Changes: []ChangeLog{
			{
				Filename:         "main.go",
				FileRevisionHash: "hash-abc",
				Status:           "active",
				Description:      "fixed a critical bug",
			},
		},
	}

	out := formatRevision(group)
	require.NotEmpty(t, out)
	assert.Contains(t, out, "rev-format-1")
	assert.Contains(t, out, "test-model-v2")
	assert.Contains(t, out, "main.go")
	assert.Contains(t, out, "hash-abc")
	assert.Contains(t, out, "active")
	assert.Contains(t, out, "fixed a critical bug")
}

// TestFormatRevision_MultipleChanges verifies that all file changes
// are included when a revision has multiple changes.
func TestFormatRevision_MultipleChanges(t *testing.T) {
	group := RevisionGroup{
		RevisionID: "rev-multi",
		Timestamp:  time.Now(),
		Changes: []ChangeLog{
			{Filename: "a.go", FileRevisionHash: "ha", Status: "active", Description: "change a"},
			{Filename: "b.go", FileRevisionHash: "hb", Status: "reverted", Description: "change b"},
			{Filename: "c.go", FileRevisionHash: "hc", Status: "restored", Description: "change c"},
		},
	}

	out := formatRevision(group)
	assert.Contains(t, out, "File Changes (3):")
	for _, c := range group.Changes {
		assert.Contains(t, out, c.Filename, "filename %q missing from output", c.Filename)
		assert.Contains(t, out, c.Description, "description %q missing", c.Description)
	}
}

// TestFormatRevision_EmptyModel shows "Not specified".
func TestFormatRevision_EmptyModel(t *testing.T) {
	group := RevisionGroup{
		RevisionID: "rev-no-model",
		Timestamp:  time.Now(),
		Changes:    []ChangeLog{},
	}
	out := formatRevision(group)
	assert.Contains(t, out, "Model: Not specified")
}

// TestFormatRevision_IncludesNote verifies that a change's note is
// rendered when present.
func TestFormatRevision_IncludesNote(t *testing.T) {
	group := RevisionGroup{
		RevisionID: "rev-note",
		Timestamp:  time.Now(),
		Changes: []ChangeLog{
			{
				Filename:         "noted.go",
				FileRevisionHash: "hn",
				Status:           "active",
				Description:      "with note",
				Note:             sql.NullString{String: "important note text", Valid: true},
			},
		},
	}
	out := formatRevision(group)
	assert.Contains(t, out, "important note text")
}

// ---------------------------------------------------------------------------
// GetFilesForRevision
// ---------------------------------------------------------------------------

// TestGetFilesForRevision_ReturnsActiveFiles verifies that the function
// returns the filenames of active changes for a given revision.
func TestGetFilesForRevision_ReturnsActiveFiles(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	revID := "getfiles-rev-1"
	createRevisionDir(t, revisionsDir, revID)

	now := time.Now()
	// Two active files, one reverted (should be excluded).
	createChangeDirWithContent(t, changesDir, "hash-a", revID, "active_a.go", "old", "new", now)
	createChangeDirWithContent(t, changesDir, "hash-b", revID, "active_b.go", "old", "new", now.Add(-time.Minute))
	createChangeDirWithContent(t, changesDir, "hash-c", revID, "reverted_c.go", "old", "new", now.Add(-2*time.Minute))
	// Mark the third as reverted.
	updateChangeStatus("hash-c", "reverted")

	files, err := GetFilesForRevision(revID)
	require.NoError(t, err)

	// Should include the two active files, not the reverted one.
	require.Len(t, files, 2)
	sort.Strings(files) // make order deterministic for assertion
	assert.Equal(t, []string{"active_a.go", "active_b.go"}, files)
}

// TestGetFilesForRevision_NoActiveChanges returns empty slice.
func TestGetFilesForRevision_NoActiveChanges(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	revID := "getfiles-rev-2"
	createRevisionDir(t, revisionsDir, revID)

	createChangeDirWithContent(t, changesDir, "hash-r", revID, "file.go", "old", "new", time.Now())
	updateChangeStatus("hash-r", "reverted")

	files, err := GetFilesForRevision(revID)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// TestGetFilesForRevision_UnknownRevision returns empty slice, no error.
func TestGetFilesForRevision_UnknownRevision(t *testing.T) {
	setupHistoryDirs(t)

	files, err := GetFilesForRevision("nonexistent-rev-xyz")
	require.NoError(t, err)
	assert.Empty(t, files)
}

// TestGetFilesForRevision_DoesNotReturnFilesFromOtherRevisions verifies
// that files belonging to a different revision are not included.
func TestGetFilesForRevision_DoesNotReturnFilesFromOtherRevisions(t *testing.T) {
	changesDir, revisionsDir := setupHistoryDirs(t)

	createRevisionDir(t, revisionsDir, "target-rev")
	createRevisionDir(t, revisionsDir, "other-rev")

	now := time.Now()
	createChangeDirWithContent(t, changesDir, "t1", "target-rev", "target.go", "o", "n", now)
	createChangeDirWithContent(t, changesDir, "o1", "other-rev", "other.go", "o", "n", now)

	files, err := GetFilesForRevision("target-rev")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "target.go", files[0])
}
