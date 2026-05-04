package webui

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleAPISupportBundleMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/support-bundle", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISupportBundle(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPISupportBundleNilAgent(t *testing.T) {
	ws := &ReactWebServer{
		agent: nil,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/support-bundle", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISupportBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (best-effort), got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("expected Content-Type application/zip, got %s", rec.Header().Get("Content-Type"))
	}

	// Verify it's a valid zip
	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("expected valid zip, got error: %v", err)
	}
	if len(reader.File) == 0 {
		t.Fatal("expected at least one file in zip")
	}

	// Check that the config error file is present
	found := false
	for _, f := range reader.File {
		if f.Name == "config-error.txt" {
			found = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("failed to open zip entry: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("failed to read zip entry: %v", err)
			}
			if string(data) == "" {
				t.Fatal("expected non-empty config-error.txt")
			}
		}
	}
	if !found {
		t.Fatal("expected config-error.txt in zip when agent is nil")
	}
}

func TestWriteBundleBytes(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	zw := zip.NewWriter(f)

	err = writeBundleBytes(zw, "test.txt", []byte("hello"))
	zw.Close()
	f.Close()

	if err != nil {
		t.Fatalf("writeBundleBytes failed: %v", err)
	}

	reader, err := zip.OpenReader(tmpFile)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer reader.Close()

	if len(reader.File) != 1 {
		t.Fatalf("expected 1 file in zip, got %d", len(reader.File))
	}
	if reader.File[0].Name != "test.txt" {
		t.Fatalf("expected test.txt, got %s", reader.File[0].Name)
	}
	rc, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("failed to open zip entry: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("failed to read zip entry: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(data))
	}
}

func TestWriteBundleText(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	zw := zip.NewWriter(f)

	writeBundleText(zw, "error.txt", "some error message")
	zw.Close()
	f.Close()

	if err != nil {
		t.Fatalf("writeBundleText failed: %v", err)
	}

	reader, err := zip.OpenReader(tmpFile)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer reader.Close()

	if len(reader.File) != 1 {
		t.Fatalf("expected 1 file in zip, got %d", len(reader.File))
	}
	if reader.File[0].Name != "error.txt" {
		t.Fatalf("expected error.txt, got %s", reader.File[0].Name)
	}
	rc, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("failed to open zip entry: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("failed to read zip entry: %v", err)
	}
	if string(data) != "some error message" {
		t.Fatalf("expected 'some error message', got %q", string(data))
	}
}

func TestWriteBundleLogsIgnoresNonLogFiles(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a .log file
	os.WriteFile(filepath.Join(tmpDir, "app.log"), []byte("log content"), 0644)
	// Create a .jsonl file
	os.WriteFile(filepath.Join(tmpDir, "data.jsonl"), []byte(`{"key":"value"}`), 0644)
	// Create a .txt file (should be ignored)
	os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("notes"), 0644)

	tmpZip := filepath.Join(t.TempDir(), "bundle.zip")
	f, err := os.Create(tmpZip)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	writeBundleLogs(zw, tmpDir)
	zw.Close()
	f.Close()

	if err != nil {
		t.Fatalf("writeBundleLogs failed: %v", err)
	}

	reader, err := zip.OpenReader(tmpZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()

	// Should have exactly 2 entries (log + jsonl), not the .txt
	if len(reader.File) != 2 {
		t.Fatalf("expected 2 files in zip, got %d", len(reader.File))
	}

	names := make([]string, 0, len(reader.File))
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	found := false
	for _, n := range names {
		if n == "logs/notes.txt" {
			found = true
		}
	}
	if found {
		t.Fatal("should not include .txt files in bundle logs")
	}
}

func TestWriteBundleLogFileTruncatesLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.log")
	// Write a file larger than supportBundleMaxLogBytes (512KB)
	data := make([]byte, supportBundleMaxLogBytes+1000)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	os.WriteFile(largeFile, data, 0644)

	tmpZip := filepath.Join(t.TempDir(), "bundle.zip")
	f, err := os.Create(tmpZip)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	err = writeBundleLogFile(zw, "large.log", largeFile)
	zw.Close()
	f.Close()

	if err != nil {
		t.Fatalf("writeBundleLogFile failed: %v", err)
	}

	reader, err := zip.OpenReader(tmpZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()

	if len(reader.File) != 1 {
		t.Fatalf("expected 1 file, got %d", len(reader.File))
	}
	rc, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("open entry: %v", err)
	}
	readData, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	// Should be truncated to supportBundleMaxLogBytes
	if len(readData) > supportBundleMaxLogBytes {
		t.Fatalf("expected max %d bytes, got %d", supportBundleMaxLogBytes, len(readData))
	}
}

func TestSupportBundleFilename(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/support-bundle", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISupportBundle(rec, req)

	contentDisp := rec.Header().Get("Content-Disposition")
	if contentDisp == "" {
		t.Fatal("expected Content-Disposition header")
	}
	if len(contentDisp) < 10 {
		t.Fatalf("Content-Disposition too short: %s", contentDisp)
	}
}

func TestWriteBundleConfigNilAgent(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	tmpFile := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	defer func() {
		zw.Close()
		f.Close()
	}()

	err = writeBundleConfig(zw, ws)
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
	if err.Error() != "agent not available" {
		t.Fatalf("expected 'agent not available', got %v", err)
	}
}
