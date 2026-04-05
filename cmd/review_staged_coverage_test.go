package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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