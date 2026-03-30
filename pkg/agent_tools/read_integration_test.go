package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadFileTruncationBehavior tests that:
// 1. Full file reads truncate to 80KB with head+tail strategy
// 2. Line range reads use 10MB limit
// 3. Truncation warnings are shown appropriately
func TestReadFileTruncationBehavior(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file larger than 80KB but smaller than 10MB
	// 2000 lines, each ~150 bytes = ~300KB
	largeFile := filepath.Join(tmpDir, "large.txt")
	var content strings.Builder
	for i := 1; i <= 2000; i++ {
		content.WriteString(fmt.Sprintf("Line %d: %s\n", i, strings.Repeat("x", 130)))
	}

	err := os.WriteFile(largeFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test 1: Full file read should truncate and warn
	// With 80KB limit on a ~300KB file: head ~48KB (60%), tail ~32KB (40%)
	// Yields ~580 lines out of 2000 (first ~349 + last ~231)
	t.Run("FullFileReadTruncates", func(t *testing.T) {
		result, err := ReadFile(ctx, largeFile)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		// Should contain truncation warning
		if !strings.Contains(result, "[WARN]") {
			t.Errorf("Expected truncation warning for full file read, got: %s", result[:min(200, len(result))])
		}

		// Should mention view_range parameter
		if !strings.Contains(result, "view_range") {
			t.Errorf("Expected hint to use view_range parameter, got: %s", result[:min(300, len(result))])
		}

		// Should not contain all 2000 lines (truncated to ~80KB head+tail)
		lineCount := strings.Count(result, "Line ")
		if lineCount > 750 {
			t.Errorf("Expected truncation to ~80KB head+tail (~580 lines), got %d lines", lineCount)
		}
		if lineCount < 350 {
			t.Errorf("Expected more lines from 80KB head+tail read, got only %d lines", lineCount)
		}
	})

	// Test 2: Line range read should NOT truncate for this size file
	t.Run("LineRangeReadNoTruncate", func(t *testing.T) {
		result, err := ReadFileWithRange(ctx, largeFile, 1000, 1010)
		if err != nil {
			t.Fatalf("ReadFileWithRange failed: %v", err)
		}

		// Should NOT contain truncation warning (file is only 300KB)
		if strings.Contains(result, "[WARN]") {
			t.Errorf("Should not truncate for 300KB file with line range, got: %s", result)
		}

		// Should contain requested lines
		if !strings.Contains(result, "Line 1000:") {
			t.Errorf("Expected Line 1000 in result, got: %s", result)
		}
		if !strings.Contains(result, "Line 1010:") {
			t.Errorf("Expected Line 1010 in result, got: %s", result)
		}
	})

	// Test 3: Line range beyond what fits in 100KB should work
	t.Run("LineRangeBeyond100KB", func(t *testing.T) {
		// Lines 800-810 would definitely be beyond 100KB truncation point
		result, err := ReadFileWithRange(ctx, largeFile, 800, 810)
		if err != nil {
			t.Fatalf("ReadFileWithRange failed: %v", err)
		}

		// Should succeed (no truncation with 10MB limit)
		if strings.Contains(result, "[WARN]") {
			t.Errorf("Should not truncate line ranges for 150KB file, got: %s", result)
		}

		if !strings.Contains(result, "Line 800:") {
			t.Errorf("Expected Line 800, got: %s", result)
		}
	})
}

// TestEditAfterTruncatedRead verifies the fix works:
// 1. Model reads truncated file (sees warning with view_range hint)
// 2. Model uses line range to read the function
// 3. Model can now successfully edit the function
func TestEditAfterTruncatedRead(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file > 80KB (target function at line 1501 will be in omitted range with 80KB head+tail)
	testFile := filepath.Join(tmpDir, "test.go")
	var content strings.Builder

	// First 1500 lines: filler content
	for i := 1; i <= 1500; i++ {
		content.WriteString(fmt.Sprintf("// Line %d: %s\n", i, strings.Repeat("x", 140)))
	}

	// Lines 1501-1503: target function
	content.WriteString("func targetFunction() {\n")
	content.WriteString("    // Important code here\n")
	content.WriteString("}\n")

	// More filler to ensure file > 80KB
	for i := 1504; i <= 2000; i++ {
		content.WriteString(fmt.Sprintf("// Line %d: %s\n", i, strings.Repeat("x", 140)))
	}

	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Step 1: Model does full read, gets truncation warning
	fullRead, err := ReadFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Should have truncation warning
	if !strings.Contains(fullRead, "[WARN]") {
		t.Error("Expected truncation warning in full read")
	}

	// Should NOT contain function (line 1501 is in the omitted range with 80KB head+tail)
	if strings.Contains(fullRead, "targetFunction") {
		t.Fatalf("Full read should NOT contain targetFunction — line 1501 should be in the omitted middle section")
	}

	// Step 2: Model follows the view_range hint and uses line range
	lineRangeRead, err := ReadFileWithRange(ctx, testFile, 1501, 1503)
	if err != nil {
		t.Fatalf("ReadFileWithRange failed: %v", err)
	}

	// Should contain the function
	if !strings.Contains(lineRangeRead, "targetFunction") {
		t.Errorf("Line range read should contain targetFunction, got: %s", lineRangeRead)
	}

	// Should NOT have truncation warning (10MB limit is sufficient)
	if strings.Contains(lineRangeRead, "[WARN]") {
		t.Error("Line range read should not truncate for this file size")
	}

	// Step 3: Model can now successfully edit the function
	oldString := "func targetFunction() {\n    // Important code here\n}\n"
	newString := "func targetFunction() {\n    // Updated code here\n}\n"

	result, err := EditFile(ctx, testFile, oldString, newString)
	if err != nil {
		t.Errorf("Edit should succeed with line range read, got error: %v", err)
	}
	if result == "" {
		t.Error("Expected success message from edit")
	}
}
