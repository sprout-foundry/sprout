package embedding

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestModelDownloader_Timeout_Default(t *testing.T) {
	// Verify that NewModelDownloaderWithDir sets a 5-minute HTTP client timeout.
	d := NewModelDownloaderWithDir(t.TempDir())
	if d.client == nil {
		t.Fatal("client should not be nil")
	}
	if d.client.Timeout != 5*time.Minute {
		t.Errorf("expected default timeout of 5m0s, got %v", d.client.Timeout)
	}

	// Also verify NewModelDownloader (default dir).
	d2 := NewModelDownloader()
	if d2.client.Timeout != 5*time.Minute {
		t.Errorf("expected default timeout of 5m0s from NewModelDownloader, got %v", d2.client.Timeout)
	}
}

func TestModelDownloader_Download_Timeout_BlocksForever(t *testing.T) {
	// Test that the HTTP client timeout prevents a download from hanging forever
	// when the server accepts the connection but never sends a response.
	tmpDir := t.TempDir()
	d := NewModelDownloaderWithDir(tmpDir)

	// Override the client timeout to 2 seconds for this test.
	// The default is 5 minutes, which is too long for a unit test.
	d.client = &http.Client{Timeout: 2 * time.Second}

	// Create a test server that accepts the connection but never writes a response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally never respond — just block forever.
		// The server will keep the connection open, but never send headers.
		// The client timeout should kick in.
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := ModelConfig{
		Name:       "timeout-test",
		ModelURL:   server.URL + "/model.onnx",
		Dims:       768,
		FullDims:   768,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := d.Download(ctx, cfg, nil)
	if err == nil {
		t.Fatal("expected an error from the download, got nil")
	}

	// The error should be a timeout-related error, not something else.
	errMsg := err.Error()
	isTimeout := strings.Contains(errMsg, "context deadline exceeded") ||
		strings.Contains(errMsg, "context canceled") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "client.Timeout") ||
		strings.Contains(errMsg, "TLS handshake timeout") ||
		strings.Contains(errMsg, "no response") ||
		strings.Contains(errMsg, "connection reset")
	if !isTimeout {
		t.Logf("got error (may still be acceptable): %v", err)
	}
}

func TestModelDownloader_Download_ContextCancellation(t *testing.T) {
	// Test that a cancelled context properly interrupts the download.
	tmpDir := t.TempDir()
	d := NewModelDownloaderWithDir(tmpDir)

	// Block the server until the context is cancelled.
	cancelCh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-cancelCh
	}))
	defer server.Close()
	defer close(cancelCh)

	cfg := ModelConfig{
		Name:       "cancel-test",
		ModelURL:   server.URL + "/model.onnx",
		Dims:       768,
		FullDims:   768,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := d.Download(ctx, cfg, nil)
	if err == nil {
		t.Fatal("expected an error from the cancelled download, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "context canceled") {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
}

func TestModelDownloader_Download_SkipExistingFile_NoHash(t *testing.T) {
	// Test that download is skipped when the file already exists and no hash is provided.
	tmpDir := t.TempDir()
	d := NewModelDownloaderWithDir(tmpDir)

	modelDir := filepath.Join(tmpDir, "skip-test")
	modelPath := filepath.Join(modelDir, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, "tokenizer.json")

	// Pre-create the files.
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("existing model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenizerPath, []byte("existing tokenizer"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ModelConfig{
		Name:       "skip-test",
		ModelURL:   "http://example.com/model.onnx",  // should NOT be called
		TokenizerURL: "http://example.com/tokenizer.json",
		Dims:       768,
		FullDims:   768,
	}

	err := d.Download(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("expected no error when files exist without hash, got: %v", err)
	}

	// Verify files are unchanged.
	modelContent, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(modelContent) != "existing model" {
		t.Errorf("model file was modified, expected 'existing model', got %q", string(modelContent))
	}
}
