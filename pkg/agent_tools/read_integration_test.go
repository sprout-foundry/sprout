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
// 1. Full file reads truncate to 100KB
// 2. Line range reads use 10MB limit
// 3. Truncation warnings are shown appropriately
func TestReadFileTruncationBehavior(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file larger than 100KB but smaller than 10MB
	// 1000 lines, each ~150 bytes = ~150KB
	largeFile := filepath.Join(tmpDir, "large.txt")
	var content strings.Builder
	for i := 1; i <= 1000; i++ {
		content.WriteString(fmt.Sprintf("Line %d: %s\n", i, strings.Repeat("x", 130)))
	}

	err := os.WriteFile(largeFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test 1: Full file read should truncate and warn
	t.Run("FullFileReadTruncates", func(t *testing.T) {
		result, err := ReadFile(ctx, largeFile)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		// Should contain truncation warning
		if !strings.Contains(result, "⚠️") {
			t.Errorf("Expected truncation warning for full file read, got: %s", result[:min(200, len(result))])
		}

		// Should mention using line ranges
		if !strings.Contains(result, "use line range") {
			t.Errorf("Expected hint to use line ranges, got: %s", result[:min(300, len(result))])
		}

		// Should not contain all 1000 lines (truncated)
		lineCount := strings.Count(result, "Line ")
		if lineCount > 800 {
			t.Errorf("Expected truncation to ~700 lines (100KB), got %d lines", lineCount)
		}
		if lineCount < 600 {
			t.Errorf("Expected more lines from 100KB read, got only %d lines", lineCount)
		}
	})

	// Test 2: Line range read should NOT truncate for this size file
	t.Run("LineRangeReadNoTruncate", func(t *testing.T) {
		result, err := ReadFileWithRange(ctx, largeFile, 500, 510)
		if err != nil {
			t.Fatalf("ReadFileWithRange failed: %v", err)
		}

		// Should NOT contain truncation warning (file is only 150KB)
		if strings.Contains(result, "⚠️") {
			t.Errorf("Should not truncate for 150KB file with line range, got: %s", result)
		}

		// Should contain requested lines
		if !strings.Contains(result, "Line 500:") {
			t.Errorf("Expected Line 500 in result, got: %s", result)
		}
		if !strings.Contains(result, "Line 510:") {
			t.Errorf("Expected Line 510 in result, got: %s", result)
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
		if strings.Contains(result, "⚠️") {
			t.Errorf("Should not truncate line ranges for 150KB file, got: %s", result)
		}

		if !strings.Contains(result, "Line 800:") {
			t.Errorf("Expected Line 800, got: %s", result)
		}
	})
}

// TestEditAfterTruncatedRead verifies the fix works:
// 1. Model reads truncated file (sees warning)
// 2. Model uses line range to read the function
// 3. Model can now successfully edit the function
func TestEditAfterTruncatedRead(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file > 100KB
	testFile := filepath.Join(tmpDir, "test.go")
	var content strings.Builder

	// First 700 lines: filler content
	for i := 1; i <= 700; i++ {
		content.WriteString(fmt.Sprintf("// Line %d: %s\n", i, strings.Repeat("x", 140)))
	}

	// Lines 701-703: target function
	content.WriteString("func targetFunction() {\n")
	content.WriteString("    // Important code here\n")
	content.WriteString("}\n")

	// More filler to ensure file > 100KB
	for i := 704; i <= 1000; i++ {
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
	if !strings.Contains(fullRead, "⚠️") {
		t.Error("Expected truncation warning in full read")
	}

	// Should NOT contain function (it's beyond 100KB)
	if strings.Contains(fullRead, "targetFunction") {
		t.Logf("WARNING: Full read contains targetFunction (file might be smaller than expected)")
		// This is OK - it just means our test file isn't large enough
	}

	// Step 2: Model follows the hint and uses line range
	lineRangeRead, err := ReadFileWithRange(ctx, testFile, 701, 703)
	if err != nil {
		t.Fatalf("ReadFileWithRange failed: %v", err)
	}

	// Should contain the function
	if !strings.Contains(lineRangeRead, "targetFunction") {
		t.Errorf("Line range read should contain targetFunction, got: %s", lineRangeRead)
	}

	// Should NOT have truncation warning (10MB limit is sufficient)
	if strings.Contains(lineRangeRead, "⚠️") {
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
