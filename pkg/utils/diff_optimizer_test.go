package utils

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(result.OptimizedContent, "Optimized:") {
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

	if !strings.Contains(summary, "modified") {
		t.Error("Expected summary to mention modified status")
	}

	if strings.Contains(summary, "additions") || strings.Contains(summary, "deletions") {
		t.Error("Expected metadata-only summary for lock files")
	}
}

func TestDiffOptimizer_CreateFileSummary_MetadataFromGit(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "Test User")

	lockPath := filepath.Join(repo, "package-lock.json")
	initialContent := "{\"name\":\"demo\"}\n"
	if err := os.WriteFile(lockPath, []byte(initialContent), 0o644); err != nil {
		t.Fatalf("write initial lock file: %v", err)
	}
	runGitCommand(t, repo, "add", "package-lock.json")
	runGitCommand(t, repo, "commit", "-m", "initial")

	updatedContent := "{\n  \"name\": \"demo\",\n  \"lockfileVersion\": 3\n}\n"
	if err := os.WriteFile(lockPath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("write updated lock file: %v", err)
	}
	runGitCommand(t, repo, "add", "package-lock.json")

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	summary := NewDiffOptimizer().createFileSummary("package-lock.json", &FileChangeSummary{
		AddedLines:   2,
		DeletedLines: 1,
		TotalLines:   8,
	})

	if !strings.Contains(summary, "lock file") {
		t.Fatalf("expected lock file marker in summary, got %q", summary)
	}
	if !strings.Contains(summary, "modified") {
		t.Fatalf("expected modified marker in summary, got %q", summary)
	}
	if !strings.Contains(summary, "45 bytes") {
		t.Fatalf("expected staged file size in summary, got %q", summary)
	}
	if strings.Contains(summary, "additions") || strings.Contains(summary, "deletions") {
		t.Fatalf("expected metadata-only summary, got %q", summary)
	}
}

func TestDiffOptimizer_CreateFileSummary_DeletedFileUsesHeadSize(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "Test User")

	genPath := filepath.Join(repo, "generated.min.js")
	content := "const value = 1;\n"
	if err := os.WriteFile(genPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write generated file: %v", err)
	}
	runGitCommand(t, repo, "add", "generated.min.js")
	runGitCommand(t, repo, "commit", "-m", "initial")

	if err := os.Remove(genPath); err != nil {
		t.Fatalf("remove generated file: %v", err)
	}
	runGitCommand(t, repo, "add", "-A")

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	summary := NewDiffOptimizer().createFileSummary("generated.min.js", &FileChangeSummary{
		DeletedLines: 1,
		TotalLines:   4,
	})

	if !strings.Contains(summary, "generated file") {
		t.Fatalf("expected generated file marker in summary, got %q", summary)
	}
	if !strings.Contains(summary, "deleted") {
		t.Fatalf("expected deleted marker in summary, got %q", summary)
	}
	if !strings.Contains(summary, "17 bytes") {
		t.Fatalf("expected HEAD file size in summary, got %q", summary)
	}
}

func TestDiffOptimizer_OptimizeDiff_BinaryFileAddsWarning(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "Test User")

	binaryPath := filepath.Join(repo, "artifact.wasm")
	if err := os.WriteFile(binaryPath, []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x02, 0x03, 0x04}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}
	runGitCommand(t, repo, "add", "artifact.wasm")

	diffOutput := exec.Command("git", "diff", "--cached", "--binary", "--", "artifact.wasm")
	diffOutput.Dir = repo
	diffBytes, err := diffOutput.CombinedOutput()
	if err != nil {
		t.Fatalf("git diff --cached --binary failed: %v\n%s", err, string(diffBytes))
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	result := NewDiffOptimizer().OptimizeDiff(string(diffBytes))

	if len(result.Warnings) != 1 {
		t.Fatalf("expected a single binary warning, got %v", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "Binary file staged: artifact.wasm") {
		t.Fatalf("expected binary warning to mention file, got %q", result.Warnings[0])
	}
	if !strings.Contains(result.Warnings[0], "added") {
		t.Fatalf("expected binary warning to mention change kind, got %q", result.Warnings[0])
	}
	if !strings.Contains(result.FileSummaries["artifact.wasm"], "binary file") {
		t.Fatalf("expected binary file summary, got %q", result.FileSummaries["artifact.wasm"])
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
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
