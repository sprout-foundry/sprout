package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Compile-time interface check: *ReadFileHandler implements ToolHandler.
var _ ToolHandler = (*ReadFileHandler)(nil)

func TestReadFileHandler_Name(t *testing.T) {
	h := NewReadFileHandler()
	if h.Name() != "read_file" {
		t.Errorf("Name() = %q; want %q", h.Name(), "read_file")
	}
}

func TestReadFileHandler_ImplementsInterface(t *testing.T) {
	// If this compiles, the interface is satisfied (var _ check above).
	h := NewReadFileHandler()
	var _ ToolHandler = h
	// Exercize each method to ensure no panics.
	if h.Name() == "" {
		t.Error("Name() returned empty string")
	}
	if h.Definition().Type == "" {
		t.Error("Definition().Type is empty")
	}
	if h.Validate(nil) == nil {
		t.Error("Validate(nil) should return error (missing path)")
	}
	// Execute with a bad path is expected to error.
	_, err := h.Execute(context.Background(), nil, nil)
	if err == nil {
		t.Error("Execute with nil args should return error")
	}
}

func TestReadFileHandler_Validate_MissingPath(t *testing.T) {
	h := NewReadFileHandler()
	err := h.Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) should return error")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("error should mention 'path', got: %v", err)
	}

	err = h.Validate(map[string]any{})
	if err == nil {
		t.Fatal("Validate(empty map) should return error")
	}
}

func TestReadFileHandler_Validate_EmptyPath(t *testing.T) {
	h := NewReadFileHandler()
	err := h.Validate(map[string]any{"path": ""})
	if err == nil {
		t.Fatal("Validate with empty path should return error")
	}
}

func TestReadFileHandler_Validate_InvalidViewRange(t *testing.T) {
	h := NewReadFileHandler()

	// Not an array.
	err := h.Validate(map[string]any{"path": "f.txt", "view_range": "1,5"})
	if err == nil {
		t.Fatal("Validate with string view_range should return error")
	}

	// Wrong length (only 1 element).
	err = h.Validate(map[string]any{"path": "f.txt", "view_range": []any{1.0}})
	if err == nil {
		t.Fatal("Validate with 1-element view_range should return error")
	}

	// Wrong length (3 elements).
	err = h.Validate(map[string]any{"path": "f.txt", "view_range": []any{1.0, 5.0, 10.0}})
	if err == nil {
		t.Fatal("Validate with 3-element view_range should return error")
	}

	// Contains non-numeric element.
	err = h.Validate(map[string]any{"path": "f.txt", "view_range": []any{1.0, "five"}})
	if err == nil {
		t.Fatal("Validate with string in view_range should return error")
	}

	// Valid view_range should not error.
	err = h.Validate(map[string]any{"path": "f.txt", "view_range": []any{1.0, 10.0}})
	if err != nil {
		t.Fatalf("Validate with valid view_range should not error, got: %v", err)
	}
}

func TestReadFileHandler_Execute_ReadsFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "hello.txt")
	expected := "Hello, World!\nThis is a test."
	if err := os.WriteFile(testFile, []byte(expected), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": testFile})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if result.Output != expected {
		t.Errorf("Output = %q; want %q", result.Output, expected)
	}
	if result.ErrorMessage != "" {
		t.Errorf("unexpected ErrorMessage: %s", result.ErrorMessage)
	}
}

func TestReadFileHandler_Execute_ReadsWithRange(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "lines.txt")

	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Line %d\n", i))
	}
	if err := os.WriteFile(testFile, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{
		"path":       testFile,
		"view_range": []any{5.0, 8.0},
	})
	if err != nil {
		t.Fatalf("Execute with view_range failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if !strings.Contains(result.Output, "Line 5") {
		t.Errorf("Output should contain 'Line 5', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Line 8") {
		t.Errorf("Output should contain 'Line 8', got: %s", result.Output)
	}
	if strings.Contains(result.Output, "Line 4") {
		t.Errorf("Output should not contain 'Line 4', got: %s", result.Output)
	}
	if strings.Contains(result.Output, "Line 9") {
		t.Errorf("Output should not contain 'Line 9', got: %s", result.Output)
	}
}

func TestReadFileHandler_Execute_FilePathAlias(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "alias.txt")
	expected := "Alias test content"
	if err := os.WriteFile(testFile, []byte(expected), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"file_path": testFile})
	if err != nil {
		t.Fatalf("Execute with file_path alias failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if result.Output != expected {
		t.Errorf("Output = %q; want %q", result.Output, expected)
	}
}

func TestReadFileHandler_Execute_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": "/nonexistent/file.txt"})
	if err == nil {
		t.Fatal("Execute on non-existent file should return error")
	}
	if result == nil {
		t.Fatal("Execute on non-existent file should return non-nil result")
	}
	if result.ErrorMessage == "" {
		t.Error("Execute on non-existent file should populate ErrorMessage")
	}
}

func TestReadFileHandler_Execute_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": tmpDir})
	if err == nil {
		t.Fatal("Execute on directory should return error")
	}
	if result == nil {
		t.Fatal("Execute on directory should return non-nil result")
	}
	if !strings.Contains(result.ErrorMessage, "directory") {
		t.Errorf("ErrorMessage should mention 'directory', got: %s", result.ErrorMessage)
	}
}

func TestReadFileHandler_Execute_IntViewRange(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "lines.txt")

	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString(fmt.Sprintf("Line %d\n", i))
	}
	if err := os.WriteFile(testFile, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewReadFileHandler()
	result, err := h.Execute(ctx, nil, map[string]any{
		"path":       testFile,
		"view_range": []any{5, 8}, // int values, not float64
	})
	if err != nil {
		t.Fatalf("Execute with int view_range failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if !strings.Contains(result.Output, "Line 5") {
		t.Errorf("Output should contain 'Line 5', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Line 8") {
		t.Errorf("Output should contain 'Line 8', got: %s", result.Output)
	}
	if strings.Contains(result.Output, "Line 4") {
		t.Errorf("Output should not contain 'Line 4', got: %s", result.Output)
	}
	if strings.Contains(result.Output, "Line 9") {
		t.Errorf("Output should not contain 'Line 9', got: %s", result.Output)
	}
}

func TestReadFileHandler_Validate_FilePathAlias(t *testing.T) {
	h := NewReadFileHandler()

	// file_path alias should be accepted.
	err := h.Validate(map[string]any{"file_path": "some/file.txt"})
	if err != nil {
		t.Fatalf("Validate with file_path alias should not error, got: %v", err)
	}

	// Empty file_path should be rejected.
	err = h.Validate(map[string]any{"file_path": ""})
	if err == nil {
		t.Fatal("Validate with empty file_path should return error")
	}
}
