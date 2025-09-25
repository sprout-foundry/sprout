package utils

import (
	"strings"
	"testing"
)

func TestDiffOptimizer_NewDiffOptimizer(t *testing.T) {
	optimizer := NewDiffOptimizer()

	if optimizer == nil {
		t.Fatal("Expected NewDiffOptimizer to return non-nil")
	}

	if optimizer.MaxDiffLines != 500 {
		t.Errorf("Expected MaxDiffLines to be 500, got %d", optimizer.MaxDiffLines)
	}

	if len(optimizer.LockFilePatterns) == 0 {
		t.Error("Expected LockFilePatterns to be non-empty")
	}
}

func TestDiffOptimizer_IsLockFile(t *testing.T) {
	optimizer := NewDiffOptimizer()

	tests := []struct {
		filename string
		expected bool
	}{
		{"package-lock.json", true},
		{"yarn.lock", true},
		{"Cargo.lock", true},
		{"go.sum", true},
		{"regular-file.go", false},
		{"src/package.json", false}, // Not a lock file, just contains package
		{"package.json", false},     // Not a lock file
	}

	for _, test := range tests {
		result := optimizer.isLockFile(test.filename)
		if result != test.expected {
			t.Errorf("isLockFile(%q) = %t, expected %t", test.filename, result, test.expected)
		}
	}
}

func TestDiffOptimizer_IsGeneratedFile(t *testing.T) {
	optimizer := NewDiffOptimizer()

	tests := []struct {
		filename string
		expected bool
	}{
		{"app.min.js", true},
		{"styles.min.css", true},
		{"bundle.js", true},
		{"dist/app.js", true},
		{"build/main.css", true},
		{"regular-file.go", false},
		{"src/components/Button.js", false},
	}

	for _, test := range tests {
		result := optimizer.isGeneratedFile(test.filename)
		if result != test.expected {
			t.Errorf("isGeneratedFile(%q) = %t, expected %t", test.filename, result, test.expected)
		}
	}
}

func TestDiffOptimizer_OptimizeDiff_EmptyDiff(t *testing.T) {
	optimizer := NewDiffOptimizer()
	result := optimizer.OptimizeDiff("")

	if result.OptimizedContent != "" {
		t.Error("Expected empty diff to return empty optimized content")
	}

	if len(result.FileSummaries) != 0 {
		t.Error("Expected empty diff to return empty file summaries")
	}
}

func TestDiffOptimizer_OptimizeDiff_LockFile(t *testing.T) {
	optimizer := NewDiffOptimizer()

	// Sample diff for a lock file
	diff := `diff --git a/package-lock.json b/package-lock.json
index abc123..def456 100644
--- a/package-lock.json
+++ b/package-lock.json
@@ -1,100 +1,200 @@
 {
   "name": "test-project",
   "lockfileVersion": 2,
+  "requires": true,
+  "packages": {
+    "": {
+      "name": "test-project"
+    }
+  }
 }`

	result := optimizer.OptimizeDiff(diff)

	// Should have optimized the lock file
	if len(result.FileSummaries) == 0 {
		t.Error("Expected file summaries for lock file optimization")
	}

	if _, exists := result.FileSummaries["package-lock.json"]; !exists {
		t.Error("Expected summary for package-lock.json")
	}

	// Should contain summary in optimized content
	if !strings.Contains(result.OptimizedContent, "Large file optimized") {
		t.Error("Expected optimized content to contain summary information")
	}

	// Should save bytes
	if result.BytesSaved <= 0 {
		t.Error("Expected to save bytes by optimizing lock file")
	}
}

func TestDiffOptimizer_OptimizeDiff_RegularFile(t *testing.T) {
	optimizer := NewDiffOptimizer()

	// Sample diff for a regular small file
	diff := `diff --git a/src/main.go b/src/main.go
index abc123..def456 100644
--- a/src/main.go
+++ b/src/main.go
@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
+
 func main() {
+	fmt.Println("Hello, World!")
 }`

	result := optimizer.OptimizeDiff(diff)

	// Should NOT optimize regular small files
	if len(result.FileSummaries) != 0 {
		t.Error("Expected no file summaries for regular small file")
	}

	// Should return original content
	if result.OptimizedContent != diff {
		t.Error("Expected optimized content to be same as original for regular file")
	}

	// Should not save bytes
	if result.BytesSaved != 0 {
		t.Error("Expected no bytes saved for regular file")
	}
}

func TestDiffOptimizer_CreateFileSummary(t *testing.T) {
	optimizer := NewDiffOptimizer()

	changes := &FileChangeSummary{
		AddedLines:   5,
		DeletedLines: 2,
		TotalLines:   100,
	}

	summary := optimizer.createFileSummary("package-lock.json", changes)

	if !strings.Contains(summary, "lock file") {
		t.Error("Expected summary to mention 'lock file'")
	}

	if !strings.Contains(summary, "5 additions") {
		t.Error("Expected summary to mention additions")
	}

	if !strings.Contains(summary, "2 deletions") {
		t.Error("Expected summary to mention deletions")
	}
}

func TestDiffOptimizer_ExtractFilename(t *testing.T) {
	optimizer := NewDiffOptimizer()

	tests := []struct {
		line     string
		expected string
	}{
		{"diff --git a/src/main.go b/src/main.go", "src/main.go"},
		{"diff --git a/package-lock.json b/package-lock.json", "package-lock.json"},
		{"diff --git a/path/to/file.js b/path/to/file.js", "path/to/file.js"},
		{"invalid line", ""},
	}

	for _, test := range tests {
		result := optimizer.extractFilename(test.line)
		if result != test.expected {
			t.Errorf("extractFilename(%q) = %q, expected %q", test.line, result, test.expected)
		}
	}
}
