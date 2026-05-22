//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupEmbeddingTestEnv creates a temp config dir with an embedding index dir,
// sets SPROUT_CONFIG to point at it, and return the index dir path.
// It also writes a minimal config.json with the custom index_dir.
func setupEmbeddingTestEnv(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	indexDir := filepath.Join(tmpDir, "embeddings")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		t.Fatalf("failed to create index dir: %v", err)
	}

	// Write a minimal config.json that points at our temp index dir
	configData := fmt.Sprintf(`{"version":"2.0","embedding_index":{"index_dir":%q}}`, indexDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	return indexDir
}

// resetEmbeddingsClearFlags resets the package-level flags to defaults between tests.
func resetEmbeddingsClearFlags() {
	embeddingsClearType = "all"
	embeddingsClearYes = false
	embeddingsClearDryRun = false
}

// HNSW embedding file names — must match pkg/embedding/manager.go clearCodeEmbeddingFiles/clearConversationEmbeddingFiles
var codeEmbeddingFiles = []string{
	"index.hnsw",
	"index.hnsw.meta",
	"index.hnsw.records.json",
	"embedding_index_onnx.hnsw",
	"embedding_index_onnx.hnsw.meta",
	"embedding_index_onnx.hnsw.records.json",
}

var conversationEmbeddingFiles = []string{
	"conversation_turns.hnsw",
	"conversation_turns.hnsw.meta",
	"conversation_turns.hnsw.records.json",
	"conversation_turns_onnx.hnsw",
	"conversation_turns_onnx.hnsw.meta",
	"conversation_turns_onnx.hnsw.records.json",
}

// =============================================================================
// TestEmbeddingsClear_DefaultAll — no --type flag, defaults to "all"
// =============================================================================

func TestEmbeddingsClear_DefaultAll(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some code files (3 of 6)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)
	// Create some conversation files (3 of 6)
	createTestFiles(t, indexDir, conversationEmbeddingFiles[:3]...)

	// Default is "all" — should clear everything
	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should have cleared all 6 files
	if !strings.Contains(out, "Cleared 6 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 6 embedding file(s)', got: %q", out)
	}

	// Verify all files are gone
	assertFilesDeleted(t, indexDir, codeEmbeddingFiles[:3]...)
	assertFilesDeleted(t, indexDir, conversationEmbeddingFiles[:3]...)
}

// =============================================================================
// TestEmbeddingsClear_TypeCode — only code files cleared
// =============================================================================

func TestEmbeddingsClear_TypeCode(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files (only base 3 of 6)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)

	// Create conversation files (should NOT be removed)
	createTestFiles(t, indexDir, conversationEmbeddingFiles[:3]...)

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 3 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 3 embedding file(s)', got: %q", out)
	}

	// Verify code files are gone
	assertFilesDeleted(t, indexDir, codeEmbeddingFiles[:3]...)

	// Verify conversation files still exist
	assertFilesExist(t, indexDir, conversationEmbeddingFiles[:3]...)
}

// =============================================================================
// TestEmbeddingsClear_TypeConversationTurn — only conversation files cleared
// =============================================================================

func TestEmbeddingsClear_TypeConversationTurn(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files (should NOT be removed)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)

	// Create conversation files
	createTestFiles(t, indexDir, conversationEmbeddingFiles[:3]...)

	embeddingsClearType = "conversation_turn"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 3 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 3 embedding file(s)', got: %q", out)
	}

	// Verify conversation files are gone
	assertFilesDeleted(t, indexDir, conversationEmbeddingFiles[:3]...)

	// Verify code files still exist
	assertFilesExist(t, indexDir, codeEmbeddingFiles[:3]...)
}

// =============================================================================
// TestEmbeddingsClear_TypeMemory — should behave same as conversation_turn
// =============================================================================

func TestEmbeddingsClear_TypeMemory(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create conversation files (memories are stored in these)
	createTestFiles(t, indexDir, conversationEmbeddingFiles[:3]...)

	// Create code files (should NOT be removed)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:1]...)

	embeddingsClearType = "memory"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 3 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 3 embedding file(s)', got: %q", out)
	}

	// Verify conversation files are gone
	assertFilesDeleted(t, indexDir, conversationEmbeddingFiles[:3]...)

	// Verify code files still exist
	assertFilesExist(t, indexDir, codeEmbeddingFiles[:1]...)
}

// =============================================================================
// TestEmbeddingsClear_InvalidType — returns error for invalid --type
// =============================================================================

func TestEmbeddingsClear_InvalidType(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	_ = setupEmbeddingTestEnv(t)

	embeddingsClearType = "not_a_real_type"
	err := runEmbeddingsClear()
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}

	if !strings.Contains(err.Error(), "invalid --type") {
		t.Errorf("expected error message to contain 'invalid --type', got: %v", err)
	}
}

// =============================================================================
// TestEmbeddingsClear_NoIndexDir — clean exit when dir doesn't exist
// =============================================================================

func TestEmbeddingsClear_NoIndexDir(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Point config at a non-existent index dir
	nonExistentDir := filepath.Join(tmpDir, "nonexistent", "embeddings")
	configData := fmt.Sprintf(`{"embedding_index":{"index_dir":%q}}`, nonExistentDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "No embedding index found") {
		t.Errorf("expected output to contain 'No embedding index found', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_EmptyIndexDir — clean exit when dir has no matching files
// =============================================================================

func TestEmbeddingsClear_EmptyIndexDir(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	_ = setupEmbeddingTestEnv(t)
	// Don't create any embedding files — leave the dir empty

	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "No embedding files found") {
		t.Errorf("expected output to contain 'No embedding files found', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_TypeCode_NoONNXFiles — only 3 of 6 code files exist
// =============================================================================

func TestEmbeddingsClear_TypeCode_NoONNXFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create only the non-ONNX code files (3 of 6)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)
	// ONNX files are NOT created

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should only clear the 3 files that exist
	if !strings.Contains(out, "Cleared 3 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 3 embedding file(s)', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_FullCodeFiles — all 6 code files present including ONNX
// =============================================================================

func TestEmbeddingsClear_FullCodeFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create all 6 code files
	createTestFiles(t, indexDir, codeEmbeddingFiles...)

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 6 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 6 embedding file(s)', got: %q", out)
	}

	// Verify all are gone
	assertFilesDeleted(t, indexDir, codeEmbeddingFiles...)
}

// =============================================================================
// TestEmbeddingsClear_FullConversationFiles — all 6 conversation files present
// =============================================================================

func TestEmbeddingsClear_FullConversationFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create all 6 conversation files
	createTestFiles(t, indexDir, conversationEmbeddingFiles...)

	embeddingsClearType = "conversation_turn"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 6 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 6 embedding file(s)', got: %q", out)
	}

	// Verify all are gone
	assertFilesDeleted(t, indexDir, conversationEmbeddingFiles...)
}

// =============================================================================
// TestEmbeddingsClear_All_Mixed — some code + all conversation, mixed presence
// =============================================================================

func TestEmbeddingsClear_All_Mixed(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create only some code files (3 of 6)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)

	// Create all 6 conversation files
	createTestFiles(t, indexDir, conversationEmbeddingFiles...)

	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should clear 3 code + 6 conversation = 9 total
	if !strings.Contains(out, "Cleared 9 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 9 embedding file(s)', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_UseDefaultConfigDir — no config.json, falls back to default
// =============================================================================

func TestEmbeddingsClear_UseDefaultConfigDir(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	// No config.json — should fall back to $SPROUT_CONFIG/embeddings

	indexDir := filepath.Join(tmpDir, "embeddings")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		t.Fatalf("failed to create index dir: %v", err)
	}

	// Create one code file
	createTestFiles(t, indexDir, codeEmbeddingFiles[0])

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should have cleared the 1 file that existed
	if !strings.Contains(out, "Cleared 1 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 1 embedding file(s)', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_DryRun_All — dry-run with --type=all
// =============================================================================

func TestEmbeddingsClear_DryRun_All(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some code files
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)
	// Create some conversation files
	createTestFiles(t, indexDir, conversationEmbeddingFiles[:3]...)

	embeddingsClearType = "all"
	embeddingsClearDryRun = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should say "Would clear" not "Cleared"
	if !strings.Contains(out, "Would clear 6 embedding file(s)") {
		t.Errorf("expected output to contain 'Would clear 6 embedding file(s)', got: %q", out)
	}

	// Verify nothing was deleted
	assertFilesExist(t, indexDir, codeEmbeddingFiles[:3]...)
	assertFilesExist(t, indexDir, conversationEmbeddingFiles[:3]...)
}

// =============================================================================
// TestEmbeddingsClear_DryRun_Code — dry-run with --type=code
// =============================================================================

func TestEmbeddingsClear_DryRun_Code(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files
	createTestFiles(t, indexDir, codeEmbeddingFiles[:3]...)

	embeddingsClearType = "code"
	embeddingsClearDryRun = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Would clear 3 embedding file(s)") {
		t.Errorf("expected output to contain 'Would clear 3 embedding file(s)', got: %q", out)
	}

	// Verify files still exist
	assertFilesExist(t, indexDir, codeEmbeddingFiles[:3]...)
}

// =============================================================================
// TestEmbeddingsClear_DryRun_NoFiles — dry-run when no files exist
// =============================================================================

func TestEmbeddingsClear_DryRun_NoFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	_ = setupEmbeddingTestEnv(t)
	// Don't create any embedding files

	embeddingsClearType = "all"
	embeddingsClearDryRun = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "No embedding files found") {
		t.Errorf("expected output to contain 'No embedding files found', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_RequiresYes — aborts when --yes not provided
// =============================================================================

func TestEmbeddingsClear_RequiresYes(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some files
	createTestFiles(t, indexDir, codeEmbeddingFiles[0])

	// StdinIsTerminal() returns false in tests (not a TTY), so it should
	// return an error about needing --yes.
	embeddingsClearType = "all"
	embeddingsClearYes = false
	err := runEmbeddingsClear()
	if err == nil {
		t.Fatal("expected error when --yes is not provided, got nil")
	}
	if !strings.Contains(err.Error(), "this command requires confirmation") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Verify file was NOT deleted
	assertFilesExist(t, indexDir, codeEmbeddingFiles[0])
}

// =============================================================================
// TestEmbeddingsClear_YesSkipsConfirm — --yes bypasses the prompt
// =============================================================================

func TestEmbeddingsClear_YesSkipsConfirm(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some files (2 code files)
	createTestFiles(t, indexDir, codeEmbeddingFiles[:2]...)

	// --yes should skip the confirmation prompt entirely
	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 2 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 2 embedding file(s)', got: %q", out)
	}

	// Verify files are gone
	assertFilesDeleted(t, indexDir, codeEmbeddingFiles[:2]...)
}

// =============================================================================
// TestEmbeddingsClear_DryRun_NoConfirm — dry-run skips confirmation
// =============================================================================

func TestEmbeddingsClear_DryRun_NoConfirm(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some code files
	createTestFiles(t, indexDir, codeEmbeddingFiles[:2]...)

	// Set dry-run WITHOUT --yes. Should NOT prompt for confirmation.
	embeddingsClearType = "code"
	embeddingsClearDryRun = true
	embeddingsClearYes = false

	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should NOT contain a confirmation prompt
	if strings.Contains(out, "[y/N]") {
		t.Errorf("dry-run should not show confirmation prompt, got: %q", out)
	}

	// Should show "Would clear" output
	if !strings.Contains(out, "Would clear") {
		t.Errorf("expected output to contain 'Would clear', got: %q", out)
	}

	// Verify files still exist (nothing was deleted)
	assertFilesExist(t, indexDir, codeEmbeddingFiles[:2]...)
}

// =============================================================================
// Test helpers
// =============================================================================

func createTestFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, f := range names {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}
}

func assertFilesDeleted(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, f := range names {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

func assertFilesExist(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, f := range names {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist", f)
		}
	}
}
