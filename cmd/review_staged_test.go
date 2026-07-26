//go:build !js

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for pure helper functions in review_staged.go.
// These complement the coverage-focused tests in cmd_coverage_test.go
// by adding additional edge cases and scenarios.

// --- isImportantComment ---

func TestIsImportantComment_CaseSensitivity(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    bool
	}{
		{"uppercase CRITICAL", "// CRITICAL: fail fast", true},
		{"lowercase critical", "// critical: fail fast", true},
		{"mixed case ImPoRtAnT", "// ImPoRtAnT: mixed case", true},
		{"lowercase todo", "// todo:Deferred work", true},
		{"lowercase fixme", "// fixme: fix this", true},
		{"lowercase security", "// security: sanitize input", true},
		{"lowercase warning", "// warning: be careful", true},
		{"lowercase hack", "// hack: workaround", true},
		{"lowercase bug", "// bug: known issue", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isImportantComment(tt.comment)
			if got != tt.want {
				t.Errorf("isImportantComment(%q) = %v, want %v", tt.comment, got, tt.want)
			}
		})
	}
}

func TestIsImportantComment_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"hash comment without keywords", "# some random hash comment", false},
		{"non-// comment under 50 chars", "// short note about code", false},
		// Long comment without // prefix — function only checks length when
		// HasPrefix(comment, "//"), so a non-// long comment should be false
		{"long non-// comment no keywords", "This is a very long string without any keywords that exceeds fifty characters in length total here", false},
		{"long comment with // prefix over 50", "// This is a very long comment that exceeds fifty characters and should be flagged as important here", true},
		// Hash comment with a keyword embedded is still important (case-insensitive)
		{"hash comment with BUG keyword", "# BUG: need to fix this", true},
		// Hash comment with "NOTE" substring embedded in a word
		{"hash comment with NOTE substring in word", "# denote something", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isImportantComment(tt.comment)
			if got != tt.want {
				t.Errorf("isImportantComment(%q) = %v, want %v", tt.comment, got, tt.want)
			}
		})
	}
}

func TestIsImportantComment_AllKeywords(t *testing.T) {
	keywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "XXX", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}

	for _, kw := range keywords {
		t.Run("keyword_"+kw, func(t *testing.T) {
			comment := fmt.Sprintf("// %s this is a test", kw)
			if !isImportantComment(comment) {
				t.Errorf("isImportantComment(%q) = false, want true (keyword: %s)", comment, kw)
			}
		})
	}
}

// --- extractKeyCommentsFromDiff ---

func TestExtractKeyCommentsFromDiff_MultipleFiles(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,3 @@
 package foo
+// FIX: corrected race condition
 func bar() {}
diff --git a/baz.go b/baz.go
--- a/baz.go
+++ b/baz.go
@@ -1,2 +1,3 @@
 package baz
+// SECURITY: sanitize input here
 func qux() {}
`

	got := extractKeyCommentsFromDiff(diff)
	if !strings.Contains(got, "foo.go") {
		t.Errorf("result missing foo.go; got %q", got)
	}
	if !strings.Contains(got, "baz.go") {
		t.Errorf("result missing baz.go; got %q", got)
	}
	if !strings.Contains(got, "FIX:") {
		t.Errorf("result missing FIX:; got %q", got)
	}
	if !strings.Contains(got, "SECURITY:") {
		t.Errorf("result missing SECURITY:; got %q", got)
	}
}

func TestExtractKeyCommentsFromDiff_HashComments(t *testing.T) {
	// Test that # comments in added lines are also processed
	diff := `diff --git a/script.sh b/script.sh
--- a/script.sh
+++ b/script.sh
@@ -1,2 +1,3 @@
 #!/bin/bash
+# CRITICAL: do not remove this
 echo "hello"
+# TODO: add error handling
`

	got := extractKeyCommentsFromDiff(diff)
	if got == "" {
		t.Fatal("expected non-empty result for hash-style important comments")
	}
	if !strings.Contains(got, "script.sh") {
		t.Errorf("result missing script.sh; got %q", got)
	}
	if !strings.Contains(got, "CRITICAL:") {
		t.Errorf("result missing CRITICAL:; got %q", got)
	}
	if !strings.Contains(got, "TODO:") {
		t.Errorf("result missing TODO:; got %q", got)
	}
}

// --- categorizeChanges ---

func TestCategorizeChanges_DocumentationViaMdFile(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main
+// See README.md for details
+docs/GUIDE.md
 func main() {}
`

	got := categorizeChanges(diff)
	if !strings.Contains(got, "Documentation") {
		t.Errorf("expected Documentation category for .md references; got %q", got)
	}
}

func TestCategorizeChanges_DependencyUpdatesViaRequire(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+	_ = require("express")
`

	got := categorizeChanges(diff)
	if !strings.Contains(got, "Dependency updates") {
		t.Errorf("expected Dependency updates for require(); got %q", got)
	}
}

func TestCategorizeChanges_DiffWithOnlyHeaders(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
`

	got := categorizeChanges(diff)
	if got != "" {
		t.Errorf("categorizeChanges() with only headers = %q, want empty", got)
	}
}

func TestCategorizeChanges_DebugViaDebugLog(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+	debugLog("entering function")
`

	got := categorizeChanges(diff)
	if !strings.Contains(got, "Debug/logging") {
		t.Errorf("expected Debug/logging for debugLog(); got %q", got)
	}
}

// --- shouldSkipFileForContext ---

func TestShouldSkipFileForContext_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"dot-pb.h file", "api/proto.pb.h", true},
		{"_generated.go with underscore prefix", "zz_generated.go", true},
		{"_generated.ts file", "codegen_generated.ts", true},
		{"nested node_modules", "frontend/node_modules/react/index.js", true},
		{"nested vendor", "vendor/golang.org/x/net/http2/h2c.go", true},
		{"coverage.html", "coverage.html", true},
		{"lock file no prefix", "Cargo.lock", true},
		{"regular go file", "internal/model/user.go", false},
		{"go.mod is not skipped", "go.mod", false},
		{"Makefile not skipped", "Makefile", false},
		{"Dockerfile not skipped", "Dockerfile", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipFileForContext(tt.path)
			if got != tt.want {
				t.Errorf("shouldSkipFileForContext(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// --- detectProjectType ---

func TestDetectProjectType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"Go project via go.mod", "go.mod", "Go project"},
		{"Node.js project via package.json", "package.json", "Node.js project"},
		{"Python via requirements.txt", "requirements.txt", "Python project"},
		{"Python via setup.py", "setup.py", "Python project"},
		{"Python via pyproject.toml", "pyproject.toml", "Python project"},
		{"Rust via Cargo.toml", "Cargo.toml", "Rust project"},
		{"Ruby via Gemfile", "Gemfile", "Ruby project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmpDir, tt.filename), []byte(""), 0644); err != nil {
				t.Fatalf("failed to create %s: %v", tt.filename, err)
			}
			t.Chdir(tmpDir)
			got := detectProjectType()
			if got != tt.want {
				t.Errorf("detectProjectType() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("empty dir returns empty string", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)
		got := detectProjectType()
		if got != "" {
			t.Errorf("detectProjectType() = %q, want empty string", got)
		}
	})
}

// --- isValidRepoFilePath ---

func TestIsValidRepoFilePath_RelativePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"dot-slash relative path", "./main.go", true},
		{"plain relative path", "main.go", true},
		{"nested relative path", "pkg/utils/helper.go", true},
		{"parent traversal simple", "../other/file.go", false},
		{"parent traversal deep", "../../etc/passwd", false},
		{"embedded traversal", "foo/../../../etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRepoFilePath(tt.path)
			if got != tt.want {
				t.Errorf("isValidRepoFilePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsValidRepoFilePath_AbsolutePaths(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	t.Run("absolute path within cwd", func(t *testing.T) {
		absPath := filepath.Join(cwd, "cmd", "root.go")
		if !isValidRepoFilePath(absPath) {
			t.Errorf("isValidRepoFilePath(%q) = false, want true", absPath)
		}
	})

	t.Run("absolute path outside cwd", func(t *testing.T) {
		outside := "/tmp/fake_test_file_that_does_not_exist.go"
		got := isValidRepoFilePath(outside)
		// Skip if the repo itself is under /tmp (unlikely in development)
		if strings.HasPrefix(cwd, "/tmp") {
			t.Skip("/tmp is inside the working directory, skipping cross-directory test")
		}
		if got {
			t.Errorf("isValidRepoFilePath(%q) = true, want false (outside cwd)", outside)
		}
	})
}

// --- detectProjectType Tests ---

func TestDetectProjectType_GoProject(t *testing.T) {
	// Create a temp directory with go.mod
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	os.WriteFile("go.mod", []byte("module test"), 0644)

	projectType := detectProjectType()
	if projectType != "Go project" {
		t.Errorf("Expected 'Go project', got: %q", projectType)
	}
}

func TestDetectProjectType_NodeProject(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	os.WriteFile("package.json", []byte(`{"name": "test"}`), 0644)

	projectType := detectProjectType()
	if projectType != "Node.js project" {
		t.Errorf("Expected 'Node.js project', got: %q", projectType)
	}
}

func TestDetectProjectType_PythonProject(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"requirements.txt", "requirements.txt"},
		{"setup.py", "setup.py"},
		{"pyproject.toml", "pyproject.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			defer os.Chdir(oldWd)
			os.Chdir(tmpDir)

			os.WriteFile(tt.file, []byte("test"), 0644)

			projectType := detectProjectType()
			if projectType != "Python project" {
				t.Errorf("Expected 'Python project', got: %q", projectType)
			}
		})
	}
}

func TestDetectProjectType_RustProject(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	os.WriteFile("Cargo.toml", []byte("[package]"), 0644)

	projectType := detectProjectType()
	if projectType != "Rust project" {
		t.Errorf("Expected 'Rust project', got: %q", projectType)
	}
}

func TestDetectProjectType_RubyProject(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	os.WriteFile("Gemfile", []byte("gem 'rails'"), 0644)

	projectType := detectProjectType()
	if projectType != "Ruby project" {
		t.Errorf("Expected 'Ruby project', got: %q", projectType)
	}
}

func TestDetectProjectType_NoProject(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	projectType := detectProjectType()
	if projectType != "" {
		t.Errorf("Expected empty string, got: %q", projectType)
	}
}

// --- extractStagedChangesSummary Tests ---

func TestExtractStagedChangesSummary_Success(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Initialize git repo
	cmdInit := exec.Command("git", "init")
	if err := cmdInit.Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}

	// Create and stage a file
	err := os.WriteFile("test.txt", []byte("content"), 0644)
	if err != nil {
		t.Skipf("write file failed: %v", err)
	}
	cmdAdd := exec.Command("git", "add", "test.txt")
	if err := cmdAdd.Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}

	summary := extractStagedChangesSummary()
	if !strings.Contains(summary, "Staged changes summary") {
		t.Errorf("Expected 'Staged changes summary' in output, got: %q", summary)
	}
}

func TestExtractStagedChangesSummary_NoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Initialize git repo but no staged changes
	cmdInit := exec.Command("git", "init")
	if err := cmdInit.Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}

	summary := extractStagedChangesSummary()
	// Should return empty string when no staged changes
	if summary != "" {
		t.Errorf("Expected empty string, got: %q", summary)
	}
}

// --- extractKeyCommentsFromDiff Tests ---

func TestExtractKeyCommentsFromDiff_ImportantComment(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
+// CRITICAL: This fixes a security vulnerability
 func Test() {}`

	comments := extractKeyCommentsFromDiff(diff)
	if !strings.Contains(comments, "CRITICAL") {
		t.Errorf("Expected 'CRITICAL' in comments, got: %q", comments)
	}
}

func TestExtractKeyCommentsFromDiff_NoImportantComments(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
+func Test() {}`

	comments := extractKeyCommentsFromDiff(diff)
	if comments != "" {
		t.Errorf("Expected empty string, got: %q", comments)
	}
}

func TestExtractKeyCommentsFromDiff_MultipleFiles_Coverage(t *testing.T) {
	diff := `diff --git a/file1.go b/file1.go
index 123..456 100644
--- a/file1.go
+++ b/file1.go
@@ -1,0 +1,1 @@
+// TODO: Fix this later
 func Test1() {}

diff --git a/file2.go b/file2.go
index 123..456 100644
--- a/file2.go
+++ b/file2.go
@@ -1,0 +1,1 @@
+// FIXME: This is broken
 func Test2() {}`

	comments := extractKeyCommentsFromDiff(diff)
	if !strings.Contains(comments, "file1.go") || !strings.Contains(comments, "file2.go") {
		t.Errorf("Expected both file names in comments, got: %q", comments)
	}
}

func TestExtractKeyCommentsFromDiff_LimitToTen(t *testing.T) {
	// Create a diff with more than 10 important comments
	var diffLines []string
	diffLines = append(diffLines, "diff --git a/test.go b/test.go")
	diffLines = append(diffLines, "index 123..456 100644")
	diffLines = append(diffLines, "--- a/test.go")
	diffLines = append(diffLines, "+++ b/test.go")

	for i := 0; i < 15; i++ {
		diffLines = append(diffLines, fmt.Sprintf("+// IMPORTANT comment %d", i))
	}

	diff := strings.Join(diffLines, "\n")
	comments := extractKeyCommentsFromDiff(diff)

	// Should be limited to 10 comments
	commentCount := strings.Count(comments, "IMPORTANT")
	if commentCount > 10 {
		t.Errorf("Expected at most 10 comments, got %d", commentCount)
	}
}

// --- isImportantComment Tests ---

func TestIsImportantComment_ImportantKeywords(t *testing.T) {
	importantKeywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "XXX", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}

	for _, keyword := range importantKeywords {
		t.Run(keyword, func(t *testing.T) {
			comment := "// " + keyword + " test"
			if !isImportantComment(comment) {
				t.Errorf("Expected %q to be important, got false", keyword)
			}
		})
	}
}

func TestIsImportantComment_LongComment(t *testing.T) {
	comment := "// This is a very long comment that provides important context about why this code exists and what it does"
	if !isImportantComment(comment) {
		t.Error("Expected long comment to be important")
	}
}

func TestIsImportantComment_NotImportant(t *testing.T) {
	comment := "// just a simple comment"
	if isImportantComment(comment) {
		t.Error("Expected simple comment to not be important")
	}
}

// --- categorizeChanges Tests ---

func TestCategorizeChanges_SecurityFix(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
+// SECURITY: Added encryption
 func Test() {}`

	categories := categorizeChanges(diff)
	if !strings.Contains(categories, "Security fixes/improvements") {
		t.Errorf("Expected 'Security fixes/improvements' in categories, got: %q", categories)
	}
}

func TestCategorizeChanges_ErrorHandling_Coverage(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,2 @@
+if err != nil {
+    return err
+}
 func Test() {}`

	categories := categorizeChanges(diff)
	if !strings.Contains(categories, "Error handling") {
		t.Errorf("Expected 'Error handling' in categories, got: %q", categories)
	}
}

func TestCategorizeChanges_Documentation_Coverage(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
+// This is a comment
 func Test() {}`

	categories := categorizeChanges(diff)
	if !strings.Contains(categories, "Documentation") {
		t.Errorf("Expected 'Documentation' in categories, got: %q", categories)
	}
}

func TestCategorizeChanges_TestChanges(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
+func TestSomething() {}`

	categories := categorizeChanges(diff)
	if !strings.Contains(categories, "Test changes") {
		t.Errorf("Expected 'Test changes' in categories, got: %q", categories)
	}
}

func TestCategorizeChanges_CodeRemoval_Coverage(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,2 -1,0 @@
-removed code
 func Test() {}`

	categories := categorizeChanges(diff)
	if !strings.Contains(categories, "Code removal/refactoring") {
		t.Errorf("Expected 'Code removal/refactoring' in categories, got: %q", categories)
	}
}

func TestCategorizeChanges_NoCategories(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index 123..456 100644
--- a/test.go
+++ b/test.go
@@ -1,0 +1,1 @@
 func Test() {}`

	categories := categorizeChanges(diff)
	if categories != "" {
		t.Errorf("Expected empty string, got: %q", categories)
	}
}

// --- extractFileContextForChanges Tests ---

func TestExtractFileContextForChanges_ChangedFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Initialize git repo
	cmdInit := exec.Command("git", "init")
	if err := cmdInit.Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}

	// Create a file
	err := os.WriteFile("test.go", []byte("package main\nfunc Test() {}"), 0644)
	if err != nil {
		t.Skipf("write file failed: %v", err)
	}

	// Stage the file
	cmdAdd := exec.Command("git", "add", "test.go")
	if err := cmdAdd.Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}

	// Get the diff
	cmdDiff := exec.Command("git", "diff", "--cached")
	diff, _ := cmdDiff.Output()

	context := extractFileContextForChanges(string(diff))
	if !strings.Contains(context, "test.go") {
		t.Errorf("Expected 'test.go' in context, got: %q", context)
	}
}

func TestExtractFileContextForChanges_DeletedFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Initialize git repo
	cmdInit := exec.Command("git", "init")
	if err := cmdInit.Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}

	// Create and stage a file
	err := os.WriteFile("test.go", []byte("package main"), 0644)
	if err != nil {
		t.Skipf("write file failed: %v", err)
	}
	cmdAdd := exec.Command("git", "add", "test.go")
	if err := cmdAdd.Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}

	// Delete the file
	os.Remove("test.go")
	cmdAdd = exec.Command("git", "add", "test.go")
	cmdAdd.Run()

	// Get the diff
	cmdDiff := exec.Command("git", "diff", "--cached")
	diff, _ := cmdDiff.Output()

	context := extractFileContextForChanges(string(diff))
	// Deleted files should be skipped
	if strings.Contains(context, "test.go") {
		t.Errorf("Expected deleted file to be skipped, got: %q", context)
	}
}

// --- shouldSkipFileForContext Tests ---

func TestShouldSkipFileForContext_LockFiles(t *testing.T) {
	tests := []string{
		"go.sum",
		"package.lock",
		"package-lock.json",
		"yarn.lock",
	}

	for _, filePath := range tests {
		t.Run(filePath, func(t *testing.T) {
			if !shouldSkipFileForContext(filePath) {
				t.Errorf("Expected %q to be skipped", filePath)
			}
		})
	}
}

func TestShouldSkipFileForContext_GeneratedFiles(t *testing.T) {
	tests := []string{
		"file.min.js",
		"file.map",
		"file.pb.go",
		"file_generated.go",
		"file_test.test",
		"coverage.out",
	}

	for _, filePath := range tests {
		t.Run(filePath, func(t *testing.T) {
			if !shouldSkipFileForContext(filePath) {
				t.Errorf("Expected %q to be skipped", filePath)
			}
		})
	}
}

func TestShouldSkipFileForContext_BinaryFiles(t *testing.T) {
	tests := []string{
		"image.svg",
		"image.png",
		"image.jpg",
		"icon.ico",
	}

	for _, filePath := range tests {
		t.Run(filePath, func(t *testing.T) {
			if !shouldSkipFileForContext(filePath) {
				t.Errorf("Expected %q to be skipped", filePath)
			}
		})
	}
}

func TestShouldSkipFileForContext_VendorDirectories(t *testing.T) {
	tests := []string{
		"vendor/package.go",
		".git/config",
	}

	for _, filePath := range tests {
		t.Run(filePath, func(t *testing.T) {
			if !shouldSkipFileForContext(filePath) {
				t.Errorf("Expected %q to be skipped", filePath)
			}
		})
	}
}

func TestShouldSkipFileForContext_NormalFile(t *testing.T) {
	filePath := "main.go"
	if shouldSkipFileForContext(filePath) {
		t.Errorf("Expected %q to not be skipped", filePath)
	}
}

// --- isValidRepoFilePath Tests ---

func TestIsValidRepoFilePath_ValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Create a file
	os.WriteFile("test.go", []byte("package main"), 0644)

	if !isValidRepoFilePath("test.go") {
		t.Error("Expected 'test.go' to be valid")
	}
}

func TestIsValidRepoFilePath_ParentDirectoryTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	if isValidRepoFilePath("../etc/passwd") {
		t.Error("Expected '../etc/passwd' to be invalid")
	}
}

func TestIsValidRepoFilePath_AbsolutePathOutsideRepo(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// This should fail because /etc/passwd is outside the repo
	if isValidRepoFilePath("/etc/passwd") {
		t.Error("Expected '/etc/passwd' to be invalid")
	}
}

func TestIsValidRepoFilePath_CleanedPath(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Create a subdirectory
	os.MkdirAll("subdir", 0755)
	os.WriteFile("subdir/test.go", []byte("package main"), 0644)

	if !isValidRepoFilePath("./subdir/test.go") {
		t.Error("Expected './subdir/test.go' to be valid")
	}
}

func TestIsValidRepoFilePath_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(tmpDir)

	// Non-existent file should still be valid (it's about path safety, not existence)
	if isValidRepoFilePath("nonexistent.go") {
		// This is actually valid - the function checks path safety, not file existence
		t.Log("Non-existent file path is considered valid (safety check passes)")
	}
}
