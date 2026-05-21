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
// sets SPROUT_CONFIG to point at it, and returns the index dir path.
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
	configData := fmt.Sprintf(`{"embedding_index":{"index_dir":%q}}`, indexDir)
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

// =============================================================================
// TestEmbeddingsClear_DefaultAll — no --type flag, defaults to "all"
// =============================================================================

func TestEmbeddingsClear_DefaultAll(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some code files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create some conversation files
	if err := os.WriteFile(filepath.Join(indexDir, "conversation_turns.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Default is "all" — should clear everything
	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should have cleared all 4 files
	if !strings.Contains(out, "Cleared 4 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 4 embedding file(s)', got: %q", out)
	}

	// Verify all files are gone
	files := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(indexDir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

// =============================================================================
// TestEmbeddingsClear_TypeCode — only code files cleared
// =============================================================================

func TestEmbeddingsClear_TypeCode(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create conversation files (should NOT be removed)
	if err := os.WriteFile(filepath.Join(indexDir, "conversation_turns.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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

	// Verify code files are gone
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); !os.IsNotExist(err) {
		t.Error("expected index.jsonl to be deleted")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".index.jsonl.meta.json")); !os.IsNotExist(err) {
		t.Error("expected .index.jsonl.meta.json to be deleted")
	}

	// Verify conversation files still exist
	if _, err := os.Stat(filepath.Join(indexDir, "conversation_turns.jsonl")); os.IsNotExist(err) {
		t.Error("expected conversation_turns.jsonl to still exist")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json")); os.IsNotExist(err) {
		t.Error("expected .conversation_turns.jsonl.meta.json to still exist")
	}
}

// =============================================================================
// TestEmbeddingsClear_TypeConversationTurn — only conversation files cleared
// =============================================================================

func TestEmbeddingsClear_TypeConversationTurn(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files (should NOT be removed)
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create conversation files
	if err := os.WriteFile(filepath.Join(indexDir, "conversation_turns.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	embeddingsClearType = "conversation_turn"
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

	// Verify conversation files are gone
	if _, err := os.Stat(filepath.Join(indexDir, "conversation_turns.jsonl")); !os.IsNotExist(err) {
		t.Error("expected conversation_turns.jsonl to be deleted")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json")); !os.IsNotExist(err) {
		t.Error("expected .conversation_turns.jsonl.meta.json to be deleted")
	}

	// Verify code files still exist
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); os.IsNotExist(err) {
		t.Error("expected index.jsonl to still exist")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".index.jsonl.meta.json")); os.IsNotExist(err) {
		t.Error("expected .index.jsonl.meta.json to still exist")
	}
}

// =============================================================================
// TestEmbeddingsClear_TypeMemory — should behave same as conversation_turn
// =============================================================================

func TestEmbeddingsClear_TypeMemory(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create conversation files (memories are stored in these)
	if err := os.WriteFile(filepath.Join(indexDir, "conversation_turns.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create code files (should NOT be removed)
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	embeddingsClearType = "memory"
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

	// Verify conversation files are gone
	if _, err := os.Stat(filepath.Join(indexDir, "conversation_turns.jsonl")); !os.IsNotExist(err) {
		t.Error("expected conversation_turns.jsonl to be deleted")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json")); !os.IsNotExist(err) {
		t.Error("expected .conversation_turns.jsonl.meta.json to be deleted")
	}

	// Verify code files still exist
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); os.IsNotExist(err) {
		t.Error("expected index.jsonl to still exist")
	}
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
// TestEmbeddingsClear_TypeCode_NoONNXFiles — only 2 of 4 code files exist
// =============================================================================

func TestEmbeddingsClear_TypeCode_NoONNXFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create only the non-ONNX code files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// ONNX files are NOT created

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should only clear the 2 files that exist
	if !strings.Contains(out, "Cleared 2 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 2 embedding file(s)', got: %q", out)
	}
}

// =============================================================================
// TestEmbeddingsClear_FullCodeFiles — all 4 code files present including ONNX
// =============================================================================

func TestEmbeddingsClear_FullCodeFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create all 4 code files
	files := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
		"embedding_index_onnx.jsonl",
		".embedding_index_onnx.jsonl.meta.json",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(indexDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	embeddingsClearType = "code"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 4 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 4 embedding file(s)', got: %q", out)
	}

	// Verify all are gone
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(indexDir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

// =============================================================================
// TestEmbeddingsClear_FullConversationFiles — all 4 conversation files present
// =============================================================================

func TestEmbeddingsClear_FullConversationFiles(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create all 4 conversation files
	files := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
		"conversation_turns_onnx.jsonl",
		".conversation_turns_onnx.jsonl.meta.json",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(indexDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	embeddingsClearType = "conversation_turn"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Cleared 4 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 4 embedding file(s)', got: %q", out)
	}

	// Verify all are gone
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(indexDir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

// =============================================================================
// TestEmbeddingsClear_All_Mixed — some code + all conversation, mixed presence
// =============================================================================

func TestEmbeddingsClear_All_Mixed(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create only some code files (2 of 4)
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create all 4 conversation files
	convFiles := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
		"conversation_turns_onnx.jsonl",
		".conversation_turns_onnx.jsonl.meta.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(indexDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	embeddingsClearType = "all"
	embeddingsClearYes = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should clear 2 code + 4 conversation = 6 total
	if !strings.Contains(out, "Cleared 6 embedding file(s)") {
		t.Errorf("expected output to contain 'Cleared 6 embedding file(s)', got: %q", out)
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
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create some conversation files
	if err := os.WriteFile(filepath.Join(indexDir, "conversation_turns.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".conversation_turns.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	embeddingsClearType = "all"
	embeddingsClearDryRun = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	// Should say "Would clear" not "Cleared"
	if !strings.Contains(out, "Would clear 4 embedding file(s)") {
		t.Errorf("expected output to contain 'Would clear 4 embedding file(s)', got: %q", out)
	}

	// Verify nothing was deleted
	files := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(indexDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist after dry-run", f)
		}
	}
}

// =============================================================================
// TestEmbeddingsClear_DryRun_Code — dry-run with --type=code
// =============================================================================

func TestEmbeddingsClear_DryRun_Code(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create code files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	embeddingsClearType = "code"
	embeddingsClearDryRun = true
	out := captureStdout(t, func() {
		err := runEmbeddingsClear()
		if err != nil {
			t.Fatalf("runEmbeddingsClear() error: %v", err)
		}
	})

	if !strings.Contains(out, "Would clear 2 embedding file(s)") {
		t.Errorf("expected output to contain 'Would clear 2 embedding file(s)', got: %q", out)
	}

	// Verify files still exist
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); os.IsNotExist(err) {
		t.Error("expected index.jsonl to still exist after dry-run")
	}
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
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); os.IsNotExist(err) {
		t.Error("index.jsonl should not have been deleted when user aborted")
	}
}

// =============================================================================
// TestEmbeddingsClear_YesSkipsConfirm — --yes bypasses the prompt
// =============================================================================

func TestEmbeddingsClear_YesSkipsConfirm(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); !os.IsNotExist(err) {
		t.Error("expected index.jsonl to be deleted")
	}
}

// =============================================================================
// TestEmbeddingsClear_DryRun_NoConfirm — dry-run skips confirmation
// =============================================================================

func TestEmbeddingsClear_DryRun_NoConfirm(t *testing.T) {
	defer resetEmbeddingsClearFlags()

	indexDir := setupEmbeddingTestEnv(t)

	// Create some code files
	if err := os.WriteFile(filepath.Join(indexDir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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
	if _, err := os.Stat(filepath.Join(indexDir, "index.jsonl")); os.IsNotExist(err) {
		t.Error("index.jsonl should still exist after dry-run")
	}
	if _, err := os.Stat(filepath.Join(indexDir, ".index.jsonl.meta.json")); os.IsNotExist(err) {
		t.Error(".index.jsonl.meta.json should still exist after dry-run")
	}
}
