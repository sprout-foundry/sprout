package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// TestClearEmbeddingFiles_Code — removes only code index files
// =============================================================================

func TestClearEmbeddingFiles_Code(t *testing.T) {
	dir := t.TempDir()

	// Create all code files
	codeFiles := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
		"embedding_index_onnx.jsonl",
		".embedding_index_onnx.jsonl.meta.json",
	}
	for _, f := range codeFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	// Create conversation files (should NOT be removed)
	convFiles := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := ClearEmbeddingFiles(dir, "code")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(code) error: %v", err)
	}

	// All 4 code files should be deleted
	if count != 4 {
		t.Errorf("expected 4 files deleted, got %d", count)
	}

	// Verify code files are gone
	for _, f := range codeFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}

	// Verify conversation files still exist
	for _, f := range convFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist, but it was deleted", f)
		}
	}
}

// =============================================================================
// TestClearEmbeddingFiles_ConversationTurn — removes only conversation files
// =============================================================================

func TestClearEmbeddingFiles_ConversationTurn(t *testing.T) {
	dir := t.TempDir()

	// Create all conversation files
	convFiles := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
		"conversation_turns_onnx.jsonl",
		".conversation_turns_onnx.jsonl.meta.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	// Create code files (should NOT be removed)
	codeFiles := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
	}
	for _, f := range codeFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := ClearEmbeddingFiles(dir, "conversation_turn")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(conversation_turn) error: %v", err)
	}

	// All 4 conversation files should be deleted
	if count != 4 {
		t.Errorf("expected 4 files deleted, got %d", count)
	}

	// Verify conversation files are gone
	for _, f := range convFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}

	// Verify code files still exist
	for _, f := range codeFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist, but it was deleted", f)
		}
	}
}

// =============================================================================
// TestClearEmbeddingFiles_Memory — should clear same files as conversation_turn
// =============================================================================

func TestClearEmbeddingFiles_Memory(t *testing.T) {
	dir := t.TempDir()

	// Create conversation files (memories are stored in these)
	convFiles := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
		"conversation_turns_onnx.jsonl",
		".conversation_turns_onnx.jsonl.meta.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := ClearEmbeddingFiles(dir, "memory")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(memory) error: %v", err)
	}

	// All 4 conversation files should be deleted (memory uses same files)
	if count != 4 {
		t.Errorf("expected 4 files deleted, got %d", count)
	}

	// Verify all conversation files are gone
	for _, f := range convFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

// =============================================================================
// TestClearEmbeddingFiles_All — removes both code and conversation files
// =============================================================================

func TestClearEmbeddingFiles_All(t *testing.T) {
	dir := t.TempDir()

	// Create all code files
	codeFiles := []string{
		"index.jsonl",
		".index.jsonl.meta.json",
		"embedding_index_onnx.jsonl",
		".embedding_index_onnx.jsonl.meta.json",
	}
	for _, f := range codeFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	// Create all conversation files
	convFiles := []string{
		"conversation_turns.jsonl",
		".conversation_turns.jsonl.meta.json",
		"conversation_turns_onnx.jsonl",
		".conversation_turns_onnx.jsonl.meta.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := ClearEmbeddingFiles(dir, "all")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(all) error: %v", err)
	}

	// All 8 files should be deleted
	if count != 8 {
		t.Errorf("expected 8 files deleted, got %d", count)
	}

	// Verify all files are gone
	allFiles := append(codeFiles, convFiles...)
	for _, f := range allFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

// =============================================================================
// TestClearEmbeddingFiles_InvalidType — returns error for invalid type
// =============================================================================

func TestClearEmbeddingFiles_InvalidType(t *testing.T) {
	dir := t.TempDir()

	_, err := ClearEmbeddingFiles(dir, "invalid_type")
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}

	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// =============================================================================
// TestClearEmbeddingFiles_MissingFiles — no error when files don't exist
// =============================================================================

func TestClearEmbeddingFiles_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	// Don't create any files

	count, err := ClearEmbeddingFiles(dir, "code")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(code) error when no files exist: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted when none exist, got %d", count)
	}

	// Same for conversation_turn
	count, err = ClearEmbeddingFiles(dir, "conversation_turn")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(conversation_turn) error when no files exist: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted when none exist, got %d", count)
	}

	// Same for all
	count, err = ClearEmbeddingFiles(dir, "all")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(all) error when no files exist: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted when none exist, got %d", count)
	}
}

// =============================================================================
// TestClearEmbeddingFiles_MixedPresent — some files exist, some don't
// =============================================================================

func TestClearEmbeddingFiles_MixedPresent(t *testing.T) {
	dir := t.TempDir()

	// Create only 2 of 4 code files
	if err := os.WriteFile(filepath.Join(dir, "index.jsonl"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".index.jsonl.meta.json"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// embedding_index_onnx.jsonl and .embedding_index_onnx.jsonl.meta.json are NOT created

	count, err := ClearEmbeddingFiles(dir, "code")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(code) error with mixed files: %v", err)
	}

	// Only 2 files should be deleted (the ones that existed)
	if count != 2 {
		t.Errorf("expected 2 files deleted, got %d", count)
	}

	// Verify the existing ones are gone
	if _, err := os.Stat(filepath.Join(dir, "index.jsonl")); !os.IsNotExist(err) {
		t.Error("expected index.jsonl to be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".index.jsonl.meta.json")); !os.IsNotExist(err) {
		t.Error("expected .index.jsonl.meta.json to be deleted")
	}
}

// =============================================================================
// TestRemoveFilesSilently — direct unit test for the helper
// =============================================================================

func TestRemoveFilesSilently_AllExist(t *testing.T) {
	dir := t.TempDir()

	files := []string{
		filepath.Join(dir, "file1.txt"),
		filepath.Join(dir, "file2.txt"),
		filepath.Join(dir, "file3.txt"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := removeFilesSilently(files)
	if err != nil {
		t.Fatalf("removeFilesSilently error: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 files deleted, got %d", count)
	}

	// Verify all are gone
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
}

func TestRemoveFilesSilently_NoneExist(t *testing.T) {
	dir := t.TempDir()

	files := []string{
		filepath.Join(dir, "nonexistent1.txt"),
		filepath.Join(dir, "nonexistent2.txt"),
	}

	count, err := removeFilesSilently(files)
	if err != nil {
		t.Fatalf("removeFilesSilently error when files don't exist: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted, got %d", count)
	}
}

func TestRemoveFilesSilently_MixedExist(t *testing.T) {
	dir := t.TempDir()

	// Create only one of three
	existingFile := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	files := []string{
		existingFile,
		filepath.Join(dir, "missing1.txt"),
		filepath.Join(dir, "missing2.txt"),
	}

	count, err := removeFilesSilently(files)
	if err != nil {
		t.Fatalf("removeFilesSilently error with mixed files: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 file deleted, got %d", count)
	}

	// Verify the existing one is gone
	if _, err := os.Stat(existingFile); !os.IsNotExist(err) {
		t.Error("expected existing file to be deleted")
	}
}

func TestRemoveFilesSilently_EmptyList(t *testing.T) {
	count, err := removeFilesSilently([]string{})
	if err != nil {
		t.Fatalf("removeFilesSilently error on empty list: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted on empty list, got %d", count)
	}
}

func TestRemoveFilesSilently_NilList(t *testing.T) {
	count, err := removeFilesSilently(nil)
	if err != nil {
		t.Fatalf("removeFilesSilently error on nil list: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 files deleted on nil list, got %d", count)
	}
}
