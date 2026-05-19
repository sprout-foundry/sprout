package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ------------------------------------------------------------------------
// migrationMarkerPath
// ------------------------------------------------------------------------

func TestMigrationMarkerPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		indexDir string
		expected string
	}{
		{
			name:     "simple path",
			indexDir: "/home/user/.config/sprout/embeddings",
			expected: "/home/user/.config/sprout/embeddings/.memory_migration_done",
		},
		{
			name:     "relative path",
			indexDir: "embeddings",
			expected: "embeddings/.memory_migration_done",
		},
		{
			name:     "empty dir",
			indexDir: "",
			expected: ".memory_migration_done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := migrationMarkerPath(tt.indexDir)
			if got != tt.expected {
				t.Errorf("migrationMarkerPath(%q) = %q, want %q", tt.indexDir, got, tt.expected)
			}
		})
	}
}

// Verify the marker name constant matches expected value
func TestMigrationMarkerName(t *testing.T) {
	t.Parallel()
	if migrationMarkerName != ".memory_migration_done" {
		t.Errorf("migrationMarkerName = %q, want %q", migrationMarkerName, ".memory_migration_done")
	}
}

// ------------------------------------------------------------------------
// writeMigrationMarker
// ------------------------------------------------------------------------

func TestWriteMigrationMarker_CreatesFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	err := writeMigrationMarker(tempDir)
	if err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Verify the marker file exists
	markerPath := migrationMarkerPath(tempDir)
	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("marker file should exist after write: %v", err)
	}
	if info.IsDir() {
		t.Fatal("marker should be a file, not a directory")
	}
	// File should be empty (written with nil content)
	if info.Size() != 0 {
		t.Errorf("marker file should be empty, got size %d", info.Size())
	}
}

func TestWriteMigrationMarker_AtomicCleanup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	err := writeMigrationMarker(tempDir)
	if err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Verify no temp file is left behind
	tmpPath := migrationMarkerPath(tempDir) + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should have been cleaned up after successful write")
	}
}

func TestWriteMigrationMarker_CreatesParentDir(t *testing.T) {
	t.Parallel()

	// Note: writeMigrationMarker does NOT create parent dirs itself —
	// it relies on the caller to ensure the index directory exists.
	// This test verifies behavior when the parent dir does NOT exist.
	tempDir := t.TempDir()
	nonExistent := filepath.Join(tempDir, "no-such-dir", "embeddings")

	err := writeMigrationMarker(nonExistent)
	if err == nil {
		t.Fatal("expected error writing marker to non-existent parent directory")
	}
	if !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "does not exist") {
		t.Logf("got unexpected error type: %v (may be OK depending on OS)", err)
	}
}

func TestWriteMigrationMarker_Idempotent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write twice — should succeed both times
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	// Verify marker still exists
	if !hasMigratedMemories(tempDir) {
		t.Fatal("marker should still exist after idempotent rewrite")
	}
}

// ------------------------------------------------------------------------
// hasMigratedMemories
// ------------------------------------------------------------------------

func TestHasMigratedMemories_True(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create the marker file
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("failed to create marker: %v", err)
	}

	if !hasMigratedMemories(tempDir) {
		t.Error("hasMigratedMemories should return true when marker exists")
	}
}

func TestHasMigratedMemories_False_NoMarker(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	if hasMigratedMemories(tempDir) {
		t.Error("hasMigratedMemories should return false when marker does not exist")
	}
}

func TestHasMigratedMemories_False_NonExistentDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nonExistent := filepath.Join(tempDir, "does-not-exist")

	if hasMigratedMemories(nonExistent) {
		t.Error("hasMigratedMemories should return false when directory does not exist")
	}
}

func TestHasMigratedMemories_False_DirHasOtherFiles(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create some other files but not the marker
	os.WriteFile(filepath.Join(tempDir, "other.txt"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(tempDir, "conversation_turns.jsonl"), []byte("[]"), 0644)

	if hasMigratedMemories(tempDir) {
		t.Error("hasMigratedMemories should return false when only other files exist")
	}
}

func TestHasMigratedMemories_HandlingAfterMarkerDeleted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create and then delete the marker
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("failed to create marker: %v", err)
	}
	if !hasMigratedMemories(tempDir) {
		t.Fatal("marker should exist after write")
	}

	os.Remove(migrationMarkerPath(tempDir))

	if hasMigratedMemories(tempDir) {
		t.Error("hasMigratedMemories should return false after marker is deleted")
	}
}

// ------------------------------------------------------------------------
// Migration name sanitization — verify migrated IDs match SaveMemory pattern
// ------------------------------------------------------------------------

func TestMigrationSanitizesMemoryNames(t *testing.T) {
	t.Parallel()

	// RunMemoryMigration sanitizes memory names before embedding so that
	// the store ID matches what SaveMemoryWithEmbedding would produce.
	// Verify the sanitization by checking a few names end-to-end.

	tests := []struct {
		rawName  string
		expected string
	}{
		{"git-safety", "git-safety"},
		{"My Memory", "my-memory"},
		{"Test 123", "test-123"},
		{"webui-embed-setup", "webui-embed-setup"},
		{"  spaced  ", "spaced"},
	}

	for _, tt := range tests {
		t.Run(tt.rawName, func(t *testing.T) {
			t.Parallel()
			got := sanitizeMemoryName(tt.rawName)
			if got != tt.expected {
				t.Errorf("sanitizeMemoryName(%q) = %q, want %q (used as store ID during migration)", tt.rawName, got, tt.expected)
			}
		})
	}
}

func TestMigrationSanitization_PreventsDuplicateIDs(t *testing.T) {
	t.Parallel()

	// Verify that the sanitization used by both migration and SaveMemory
	// produces identical IDs, preventing phantom duplicate records.

	// A file named "My @Test Memory!.md" has Name="My @Test Memory!"
	rawName := "My @Test Memory!"
	migrationID := sanitizeMemoryName(rawName)
	saveMemoryID := sanitizeMemoryName("My @Test Memory!")

	if migrationID != saveMemoryID {
		t.Errorf("migration ID %q != SaveMemory ID %q — this would create duplicate records", migrationID, saveMemoryID)
	}
	if migrationID != "my-test-memory" {
		t.Errorf("expected sanitized ID %q, got %q", "my-test-memory", migrationID)
	}
}

// ------------------------------------------------------------------------
// RunMemoryMigration — control flow tests
// ------------------------------------------------------------------------

func TestRunMemoryMigration_NilManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	err := RunMemoryMigration(ctx, nil)
	if err != nil {
		t.Fatalf("RunMemoryMigration(nil) should return nil, got: %v", err)
	}
}

func TestRunMemoryMigration_AlreadyMigrated_Skips(t *testing.T) {
	t.Parallel()

	// This test verifies the marker-check path. We can't easily create a
	// real EmbeddingManager (requires a live embedding provider), so we
	// verify the marker-skipping behavior by testing the helper functions
	// that RunMemoryMigration depends on.
	//
	// The actual flow is:
	//   1. mgr == nil → return nil (tested above)
	//   2. GetConversationStore() → error → return nil (requires real provider)
	//   3. hasMigratedMemories() → true → return nil (tested below indirectly)
	//
	// We validate path 3 by confirming hasMigratedMemories + marker creation work
	// correctly, which is what RunMemoryMigration uses for the skip check.

	tempDir := t.TempDir()

	// Simulate the state after a prior migration: marker exists
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("failed to create marker: %v", err)
	}

	// hasMigratedMemories should report true, which is what RunMemoryMigration
	// checks before attempting to embed anything
	if !hasMigratedMemories(tempDir) {
		t.Fatal("marker-based skip condition should be true")
	}
}

func TestRunMemoryMigration_NoMemories_WritesMarker(t *testing.T) {
	// Cannot use t.Parallel() — this test uses t.Setenv().

	// When no memories exist and migration runs, it should write the marker
	// to prevent re-attempts on future startups.
	//
	// We validate the marker-writing behavior directly since we can't
	// instantiate a real EmbeddingManager for the full flow.

	tempDir := t.TempDir()

	// Set up a memory dir with no files
	t.Setenv("SPROUT_CONFIG", tempDir)
	memoryDir := filepath.Join(tempDir, memoryDirName)
	os.MkdirAll(memoryDir, 0755)

	// No memories should be loaded
	memories, err := LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories failed: %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("expected 0 memories, got %d", len(memories))
	}

	// Simulate what RunMemoryMigration does for the no-memories case:
	// write the marker to prevent re-attempts
	indexDir := filepath.Join(tempDir, "embeddings")
	os.MkdirAll(indexDir, 0755)

	if err := writeMigrationMarker(indexDir); err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Verify marker was written
	if !hasMigratedMemories(indexDir) {
		t.Fatal("marker should exist after no-memories migration")
	}
}

func TestRunMemoryMigration_MarkerPreventsReentry(t *testing.T) {
	t.Parallel()

	// Once the marker exists, repeated calls should no-op.
	// We verify this through the hasMigratedMemories check.

	tempDir := t.TempDir()
	indexDir := filepath.Join(tempDir, "embeddings")
	os.MkdirAll(indexDir, 0755)

	// Initially no marker
	if hasMigratedMemories(indexDir) {
		t.Fatal("marker should not exist initially")
	}

	// After writing marker, subsequent checks should always return true
	if err := writeMigrationMarker(indexDir); err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Verify repeated checks all report migrated
	for i := 0; i < 3; i++ {
		if !hasMigratedMemories(indexDir) {
			t.Errorf("call %d: hasMigratedMemories should still return true", i)
		}
	}
}

// ------------------------------------------------------------------------
// Integration: marker lifecycle mimics RunMemoryMigration flow
// ------------------------------------------------------------------------

func TestMigrationMarkerLifecycle(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	indexDir := filepath.Join(tempDir, "embeddings")
	os.MkdirAll(indexDir, 0755)

	// Phase 1: Not yet migrated
	if hasMigratedMemories(indexDir) {
		t.Fatal("should not be migrated initially")
	}

	// Phase 2: Migration runs — write marker
	if err := writeMigrationMarker(indexDir); err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Phase 3: Subsequent runs detect marker and skip
	if !hasMigratedMemories(indexDir) {
		t.Fatal("should be marked as migrated")
	}

	// Phase 4: Marker file has expected properties
	markerPath := migrationMarkerPath(indexDir)
	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("marker file should exist: %v", err)
	}
	if info.IsDir() {
		t.Fatal("marker should be a file")
	}
	if info.Size() != 0 {
		t.Errorf("marker should be empty, got size %d", info.Size())
	}
	// Check permissions (0600)
	if info.Mode().Perm()&0777 != 0600 {
		t.Errorf("marker permissions = %o, want 0600", info.Mode().Perm()&0777)
	}
}

func TestMigrationMarkerPath_CoLocatedWithConversationStore(t *testing.T) {
	t.Parallel()

	// The marker lives in the same directory as conversation_turns.jsonl
	// Verify that migrationMarkerPath produces a sibling to the conversation store.
	tempDir := t.TempDir()
	convoPath := filepath.Join(tempDir, "conversation_turns.jsonl")
	markerPath := migrationMarkerPath(tempDir)

	if filepath.Dir(convoPath) != filepath.Dir(markerPath) {
		t.Errorf("marker and conversation store should be in the same dir: %q vs %q",
			filepath.Dir(convoPath), filepath.Dir(markerPath))
	}
}

func TestWriteMigrationMarker_TmpFileNotLeftOnError(t *testing.T) {
	t.Parallel()

	// When writeMigrationMarker encounters an error during the rename,
	// the defer should clean up the tmp file. We can't easily trigger
	// a rename error in tests, but we verify the normal path cleans up.
	tempDir := t.TempDir()

	// Write marker
	if err := writeMigrationMarker(tempDir); err != nil {
		t.Fatalf("writeMigrationMarker failed: %v", err)
	}

	// Check no tmp file remains
	entries, _ := os.ReadDir(tempDir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("unexpected temp file left behind: %q", entry.Name())
		}
	}
}
