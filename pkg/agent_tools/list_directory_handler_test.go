package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Compile-time interface check: *ListDirectoryHandler implements ToolHandler.
var _ ToolHandler = (*ListDirectoryHandler)(nil)

func TestListDirectoryHandler_Name(t *testing.T) {
	h := NewListDirectoryHandler()
	if h.Name() != "list_directory" {
		t.Errorf("Name() = %q; want %q", h.Name(), "list_directory")
	}
}

func TestListDirectoryHandler_ImplementsInterface(t *testing.T) {
	h := NewListDirectoryHandler()
	var _ ToolHandler = h
	// Exercise each method to ensure no panics.
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

func TestListDirectoryHandler_Validate_MissingPath(t *testing.T) {
	h := NewListDirectoryHandler()
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

func TestListDirectoryHandler_Validate_EmptyPath(t *testing.T) {
	h := NewListDirectoryHandler()
	err := h.Validate(map[string]any{"path": ""})
	if err == nil {
		t.Fatal("Validate with empty path should return error")
	}
}

func TestListDirectoryHandler_Validate_ValidPath(t *testing.T) {
	h := NewListDirectoryHandler()
	err := h.Validate(map[string]any{"path": "some/dir"})
	if err != nil {
		t.Fatalf("Validate with valid path should not error, got: %v", err)
	}
}

func TestListDirectoryHandler_Execute_ListsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "file2.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("failed to create nested file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewListDirectoryHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": tmpDir})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if result.ErrorMessage != "" {
		t.Errorf("unexpected ErrorMessage: %s", result.ErrorMessage)
	}

	// Verify output contains expected entries
	if !strings.Contains(result.Output, "[FILE] file1.txt") {
		t.Errorf("Output should contain '[FILE] file1.txt', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[FILE] file2.go") {
		t.Errorf("Output should contain '[FILE] file2.go', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "[DIR]  subdir") {
		t.Errorf("Output should contain '[DIR]  subdir', got: %s", result.Output)
	}
}

func TestListDirectoryHandler_Execute_NonExistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewListDirectoryHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": "/nonexistent/path"})
	if err == nil {
		t.Fatal("Execute on non-existent path should return error")
	}
	if result == nil {
		t.Fatal("Execute on non-existent path should return non-nil result")
	}
	if result.ErrorMessage == "" {
		t.Error("Execute on non-existent path should populate ErrorMessage")
	}
}

func TestListDirectoryHandler_Execute_NotADirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)

	h := NewListDirectoryHandler()
	result, err := h.Execute(ctx, nil, map[string]any{"path": testFile})
	if err == nil {
		t.Fatal("Execute on a file (not a directory) should return error")
	}
	if result == nil {
		t.Fatal("Execute on a file should return non-nil result")
	}
	if !strings.Contains(result.ErrorMessage, "not a directory") {
		t.Errorf("ErrorMessage should mention 'not a directory', got: %s", result.ErrorMessage)
	}
}
