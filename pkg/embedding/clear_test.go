package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearEmbeddingFiles_Code(t *testing.T) {
	dir := t.TempDir()

	codeFiles := []string{
		"index.hnsw",
		"index.hnsw.meta",
		"index.hnsw.records.json",
	}
	for _, f := range codeFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	convFiles := []string{
		"conversation_turns.hnsw",
		"conversation_turns.hnsw.meta",
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
	if count != len(codeFiles) {
		t.Errorf("expected %d files deleted, got %d", len(codeFiles), count)
	}
	for _, f := range codeFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
	for _, f := range convFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist, but it was deleted", f)
		}
	}
}

func TestClearEmbeddingFiles_ConversationTurn(t *testing.T) {
	dir := t.TempDir()

	convFiles := []string{
		"conversation_turns.hnsw",
		"conversation_turns.hnsw.meta",
		"conversation_turns.hnsw.records.json",
	}
	for _, f := range convFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	codeFiles := []string{"index.hnsw"}
	for _, f := range codeFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}

	count, err := ClearEmbeddingFiles(dir, "conversation_turn")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(conversation_turn) error: %v", err)
	}
	if count != len(convFiles) {
		t.Errorf("expected %d files deleted, got %d", len(convFiles), count)
	}
	for _, f := range convFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, but it still exists", f)
		}
	}
	for _, f := range codeFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to still exist, but it was deleted", f)
		}
	}
}

func TestClearEmbeddingFiles_Memory(t *testing.T) {
	dir := t.TempDir()
	convFiles := []string{
		"conversation_turns.hnsw",
		"conversation_turns.hnsw.meta",
		"conversation_turns.hnsw.records.json",
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
	if count != len(convFiles) {
		t.Errorf("expected %d files deleted, got %d", len(convFiles), count)
	}
}

func TestClearEmbeddingFiles_All(t *testing.T) {
	dir := t.TempDir()
	allFiles := []string{
		"index.hnsw", "index.hnsw.meta", "index.hnsw.records.json",
		"conversation_turns.hnsw", "conversation_turns.hnsw.meta", "conversation_turns.hnsw.records.json",
	}
	for _, f := range allFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", f, err)
		}
	}
	count, err := ClearEmbeddingFiles(dir, "all")
	if err != nil {
		t.Fatalf("ClearEmbeddingFiles(all) error: %v", err)
	}
	if count != len(allFiles) {
		t.Errorf("expected %d files deleted, got %d", len(allFiles), count)
	}
	for _, f := range allFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", f)
		}
	}
}

func TestClearEmbeddingFiles_InvalidType(t *testing.T) {
	_, err := ClearEmbeddingFiles(t.TempDir(), "invalid_type")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestClearEmbeddingFiles_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	for _, ft := range []string{"code", "conversation_turn", "memory", "all"} {
		count, err := ClearEmbeddingFiles(dir, ft)
		if err != nil {
			t.Fatalf("ClearEmbeddingFiles(%s) error: %v", ft, err)
		}
		if count != 0 {
			t.Errorf("ClearEmbeddingFiles(%s): expected 0, got %d", ft, count)
		}
	}
}

func TestClearEmbeddingFiles_MixedPresent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.hnsw"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "index.hnsw.meta"), []byte("test"), 0644)

	count, err := ClearEmbeddingFiles(dir, "code")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 files deleted, got %d", count)
	}
}

func TestRemoveFilesSilently_AllExist(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c"),
	}
	for _, f := range files {
		os.WriteFile(f, []byte("x"), 0644)
	}
	count, err := removeFilesSilently(files)
	if err != nil || count != 3 {
		t.Errorf("expected 3,nil got %d,%v", count, err)
	}
}

func TestRemoveFilesSilently_NoneExist(t *testing.T) {
	count, err := removeFilesSilently([]string{"/nonexistent"})
	if err != nil || count != 0 {
		t.Errorf("expected 0,nil got %d,%v", count, err)
	}
}

func TestRemoveFilesSilently_EmptyList(t *testing.T) {
	count, err := removeFilesSilently([]string{})
	if err != nil || count != 0 {
		t.Errorf("expected 0,nil got %d,%v", count, err)
	}
}
