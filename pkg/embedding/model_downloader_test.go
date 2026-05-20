package embedding

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestModelDownloader_IsDownloaded(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewModelDownloaderWithDir(tmpDir)
	// Should return false for non-existent model
	if d.IsDownloaded("nonexistent") {
		t.Error("expected IsDownloaded to return false for non-existent model")
	}
}

func TestModelDownloader_fileHash(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world")
	hash := sha256.Sum256(content)
	expected := fmt.Sprintf("%x", hash)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	d := NewModelDownloaderWithDir(tmpDir)
	actual, err := d.fileHash(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Errorf("expected hash %s, got %s", expected, actual)
	}
}

func TestModelDownloader_GetPaths(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewModelDownloaderWithDir(tmpDir)
	modelPath := d.GetModelPath("test-model")
	if modelPath != filepath.Join(tmpDir, "test-model", "model_q4.onnx") {
		t.Errorf("unexpected model path: %s", modelPath)
	}
	tokenizerPath := d.GetTokenizerPath("test-model")
	if tokenizerPath != filepath.Join(tmpDir, "test-model", "tokenizer.json") {
		t.Errorf("unexpected tokenizer path: %s", tokenizerPath)
	}
}
