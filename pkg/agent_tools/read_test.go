package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadLargeFileWithLineRange tests that line ranges work correctly on files larger than the default 100KB limit
func TestReadLargeFileWithLineRange(t *testing.T) {
	// Create a temporary file larger than 100KB with many lines
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.txt")

	// Create a file with 1000 lines, each ~150 bytes (total ~150KB)
	var content strings.Builder
	for i := 1; i <= 1000; i++ {
		content.WriteString(fmt.Sprintf("Line %d: %s\n", i, strings.Repeat("x", 130)))
	}

	err := os.WriteFile(largeFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test reading lines 500-510 (should succeed even though file is >100KB)
	result, err := ReadFileWithRange(ctx, largeFile, 500, 510)
	if err != nil {
		t.Fatalf("ReadFileWithRange failed: %v", err)
	}

	// Verify we got the correct lines
	if !strings.Contains(result, "Line 500:") {
		t.Errorf("Expected result to contain 'Line 500:', got: %s", result)
	}
	if !strings.Contains(result, "Line 510:") {
		t.Errorf("Expected result to contain 'Line 510:', got: %s", result)
	}
	if strings.Contains(result, "⚠️") {
		t.Errorf("Should not truncate for line range requests on 150KB file, got warning: %s", result)
	}
}

// TestReadFileWithInvalidLineRange tests that invalid line ranges return an error
func TestReadFileWithInvalidLineRange(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a small test file with 10 lines
	var content strings.Builder
	for i := 1; i <= 10; i++ {
		content.WriteString(fmt.Sprintf("Line %d\n", i))
	}

	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test start line greater than end line
	_, err = ReadFileWithRange(ctx, testFile, 10, 5)
	if err == nil {
		t.Error("Expected error when start line > end line, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "start line") {
		t.Errorf("Expected error message about start line, got: %v", err)
	}
}

// TestReadFileWithLineRangeExceedingFileLength tests that line ranges exceeding file length are handled
func TestReadFileWithLineRangeExceedingFileLength(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a small test file with 10 lines
	var content strings.Builder
	for i := 1; i <= 10; i++ {
		content.WriteString(fmt.Sprintf("Line %d\n", i))
	}

	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test start line exceeding file length
	_, err = ReadFileWithRange(ctx, testFile, 20, 30)
	if err == nil {
		t.Error("Expected error when start line exceeds file length, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds file length") {
		t.Errorf("Expected error message about exceeding file length, got: %v", err)
	}
}

// TestReadFileWithZeroLines tests reading a file with zero lines (empty file)
func TestReadFileWithZeroLines(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	// Create an empty file
	err := os.WriteFile(testFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test reading empty file
	result, err := ReadFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ReadFile failed on empty file: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty result for empty file, got: %s", result)
	}
}

// TestReadFileWithNonTextFile tests that binary/non-text files are rejected
func TestReadFileWithNonTextFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "binary.bin")

	// Create a binary file with null bytes
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	err := os.WriteFile(testFile, binaryContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test reading binary file
	_, err = ReadFile(ctx, testFile)
	if err == nil {
		t.Error("Expected error when reading binary file, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "non-text") {
		t.Errorf("Expected error message about non-text file, got: %v", err)
	}
}

// TestReadFileNormalOperation tests normal file reading without line ranges
func TestReadFileNormalOperation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "normal.txt")

	// Create a normal text file
	content := "Hello, World!\nThis is a test file.\nWith multiple lines."
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test normal read
	result, err := ReadFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected result to match file content.\nExpected: %s\nGot: %s", content, result)
	}
}

// TestReadFileWithLineRangeNormalSize tests line range on a normal-sized file
func TestReadFileWithLineRangeNormalSize(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "normal.txt")

	// Create a text file with 20 lines
	var content strings.Builder
	for i := 1; i <= 20; i++ {
		content.WriteString(fmt.Sprintf("Line %d: Content here\n", i))
	}

	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test reading specific line range
	result, err := ReadFileWithRange(ctx, testFile, 5, 8)
	if err != nil {
		t.Fatalf("ReadFileWithRange failed: %v", err)
	}

	// Verify we got the correct lines
	if !strings.Contains(result, "Line 5:") {
		t.Errorf("Expected result to contain 'Line 5:', got: %s", result)
	}
	if !strings.Contains(result, "Line 8:") {
		t.Errorf("Expected result to contain 'Line 8:', got: %s", result)
	}
	if strings.Contains(result, "Line 4:") {
		t.Errorf("Did not expect result to contain 'Line 4:', got: %s", result)
	}
	if strings.Contains(result, "Line 9:") {
		t.Errorf("Did not expect result to contain 'Line 9:', got: %s", result)
	}
}

// TestReadFileWithStartLineOnly tests reading from a start line to end of file
func TestReadFileWithStartLineOnly(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a text file with 10 lines
	var content strings.Builder
	for i := 1; i <= 10; i++ {
		content.WriteString(fmt.Sprintf("Line %d\n", i))
	}

	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test reading from line 8 to end (endLine = 0 should default to end of file)
	result, err := ReadFileWithRange(ctx, testFile, 8, 0)
	if err != nil {
		t.Fatalf("ReadFileWithRange failed: %v", err)
	}

	// Verify we got lines 8-10
	if !strings.Contains(result, "Line 8") {
		t.Errorf("Expected result to contain 'Line 8', got: %s", result)
	}
	if !strings.Contains(result, "Line 10") {
		t.Errorf("Expected result to contain 'Line 10', got: %s", result)
	}
	if strings.Contains(result, "Line 7") {
		t.Errorf("Did not expect result to contain 'Line 7:', got: %s", result)
	}
}

// TestReadFileNonExistent tests that reading a non-existent file returns an error
func TestReadFileNonExistent(t *testing.T) {
	ctx := context.Background()

	// Test reading non-existent file
	_, err := ReadFile(ctx, "/nonexistent/path/to/file.txt")
	if err == nil {
		t.Error("Expected error when reading non-existent file, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "failed to resolve") {
		t.Errorf("Expected error message about file not existing, got: %v", err)
	}
}

// TestReadFileDirectory tests that reading a directory returns an error
func TestReadFileDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()

	// Test reading a directory
	_, err := ReadFile(ctx, tmpDir)
	if err == nil {
		t.Error("Expected error when reading a directory, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "directory") {
		t.Errorf("Expected error message about directory, got: %v", err)
	}
}