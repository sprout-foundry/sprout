package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Plan flag defaults ---

func TestPlanFlagDefaults(t *testing.T) {
	// Verify that the flag defaults are as expected.
	// These are package-level vars initialized by init().
	if planModel != "" {
		t.Errorf("planModel default should be empty, got %q", planModel)
	}
	if planProvider != "" {
		t.Errorf("planProvider default should be empty, got %q", planProvider)
	}
	if planOutputFile != "" {
		t.Errorf("planOutputFile default should be empty, got %q", planOutputFile)
	}
	if planContinue != false {
		t.Errorf("planContinue default should be false, got %v", planContinue)
	}
	if planCreateTodos != true {
		t.Errorf("planCreateTodos default should be true, got %v", planCreateTodos)
	}
}

// --- Log flag defaults ---

func TestLogFlagDefault(t *testing.T) {
	if rawLog != false {
		t.Errorf("rawLog default should be false, got %v", rawLog)
	}
}

// --- detectProjectType ---

func TestDetectProjectType_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Go project" {
		t.Errorf("expected %q, got %q", "Go project", result)
	}
}

func TestDetectProjectType_NodeJS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Node.js project" {
		t.Errorf("expected %q, got %q", "Node.js project", result)
	}
}

func TestDetectProjectType_PythonRequirements(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Python project" {
		t.Errorf("expected %q, got %q", "Python project", result)
	}
}

func TestDetectProjectType_PythonSetupPy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.py"), []byte("# setup\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Python project" {
		t.Errorf("expected %q, got %q", "Python project", result)
	}
}

func TestDetectProjectType_PythonPyprojectToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Python project" {
		t.Errorf("expected %q, got %q", "Python project", result)
	}
}

func TestDetectProjectType_Rust(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Rust project" {
		t.Errorf("expected %q, got %q", "Rust project", result)
	}
}

func TestDetectProjectType_Ruby(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte("gem 'rails'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Ruby project" {
		t.Errorf("expected %q, got %q", "Ruby project", result)
	}
}

func TestDetectProjectType_Empty(t *testing.T) {
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "" {
		t.Errorf("expected empty string for unknown project type, got %q", result)
	}
}

func TestDetectProjectType_GoTakesPriorityOverNode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0644)

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := detectProjectType()
	if result != "Go project" {
		t.Errorf("Go should take priority over Node.js, got %q", result)
	}
}

// --- extractKeyCommentsFromDiff ---

func TestExtractKeyCommentsFromDiff_WithComments(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
 
+// IMPORTANT: This function must validate input
+// WARNING: Do not remove this check
+func doWork() {
+	# TODO: refactor later
+}
`

	result := extractKeyCommentsFromDiff(diff)
	if result == "" {
		t.Fatal("expected non-empty result for diff with important comments")
	}
	if !strings.Contains(result, "IMPORTANT") {
		t.Errorf("expected IMPORTANT comment, got: %s", result)
	}
	if !strings.Contains(result, "WARNING") {
		t.Errorf("expected WARNING comment, got: %s", result)
	}
}

func TestExtractKeyCommentsFromDiff_NoComments(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
 package main
 
+func main() {}
`
	result := extractKeyCommentsFromDiff(diff)
	if result != "" {
		t.Errorf("expected empty result for diff with no important comments, got: %q", result)
	}
}

func TestExtractKeyCommentsFromDiff_FollowsFileContext(t *testing.T) {
	diff := `diff --git a/auth.go b/auth.go
--- a/auth.go
+++ b/auth.go
@@ -1,2 +1,3 @@
 package auth
 
+// NOTE: authentication required
 
diff --git a/util.go b/util.go
--- a/util.go
+++ b/util.go
@@ -1,2 +1,3 @@
 package util
 
+// FIX: correct off-by-one error
`

	result := extractKeyCommentsFromDiff(diff)
	if !strings.Contains(result, "auth.go") {
		t.Errorf("expected auth.go file context, got: %s", result)
	}
	if !strings.Contains(result, "util.go") {
		t.Errorf("expected util.go file context, got: %s", result)
	}
}

func TestExtractKeyCommentsFromDiff_LimitsToTen(t *testing.T) {
	var lines []string
	lines = append(lines, "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go")
	for i := 0; i < 15; i++ {
		lines = append(lines, fmt.Sprintf("+// CRITICAL: important comment number %d", i))
	}
	diff := strings.Join(lines, "\n")

	result := extractKeyCommentsFromDiff(diff)
	// Should be limited to 10 comments
	commentLines := strings.Split(result, "\n")
	if len(commentLines) > 10 {
		t.Errorf("expected at most 10 comments, got %d", len(commentLines))
	}
}

// --- isImportantComment ---

func TestIsImportantComment(t *testing.T) {
	tests := []struct {
		comment string
		want    bool
	}{
		{"// CRITICAL: this must not fail", true},
		{"// IMPORTANT: always validate", true},
		{"// NOTE: see documentation", true},
		{"// WARNING: dangerous", true},
		{"// TODO: implement later", true},
		{"// FIXME: broken", true},
		{"// HACK: workaround for bug", true},
		{"// XXX: needs review", true},
		{"// BUG: known issue", true},
		{"// SECURITY: sensitive", true},
		{"// FIX: correct behavior", true},
		{"// WORKAROUND: temp fix", true},
		{"// BECAUSE: without this X fails", true},
		{"// REASON: explains why", true},
		{"// WHY: explains purpose", true},
		{"// INTENT: future work", true},
		{"// PURPOSE: documents intent", true},
		{"// This is a regular comment", false},
		{"// just a note", false},
		{"regular code", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.comment, func(t *testing.T) {
			got := isImportantComment(tt.comment)
			if got != tt.want {
				t.Errorf("isImportantComment(%q) = %v, want %v", tt.comment, got, tt.want)
			}
		})
	}
}

func TestIsImportantComment_LongMultiLineComment(t *testing.T) {
	// Long comment (>50 chars) starting with // should be important
	longComment := "// This is a very detailed explanation that spans more than fifty characters to explain something"
	if !isImportantComment(longComment) {
		t.Errorf("expected long comment to be important")
	}

	// Short comment starting with // should not be important
	shortComment := "// short one"
	if isImportantComment(shortComment) {
		t.Errorf("expected short comment to NOT be important")
	}
}

// --- categorizeChanges ---

func TestCategorizeChanges_SecurityChanges(t *testing.T) {
	diff := `diff --git a/auth.go b/auth.go
--- a/auth.go
+++ b/auth.go
@@ -1,2 +1,4 @@
 package auth
 
+// SECURITY: add validation
+func validateInput() error { return nil }
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Security") {
		t.Errorf("expected security category, got: %s", result)
	}
}

func TestCategorizeChanges_ErrorHandling(t *testing.T) {
	diff := `diff --git a/handler.go b/handler.go
--- a/handler.go
+++ b/handler.go
@@ -1,2 +1,4 @@
 package handler
 
+	if err != nil {
+		return nil, err
+	}
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Error handling") {
		t.Errorf("expected error handling category, got: %s", result)
	}
}

func TestCategorizeChanges_Dependencies(t *testing.T) {
	diff := `diff --git a/go.mod b/go.mod
--- a/go.mod
+++ b/go.mod
@@ -1,2 +1,3 @@
 module test
 
+	require github.com/some/dep v1.0.0
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Dependency") {
		t.Errorf("expected dependency category, got: %s", result)
	}
}

func TestCategorizeChanges_Tests(t *testing.T) {
	diff := `diff --git a/handler_test.go b/handler_test.go
--- a/handler_test.go
+++ b/handler_test.go
@@ -1,2 +1,3 @@
 package handler
 
+func TestHandler(t *testing.T) {}
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Test") {
		t.Errorf("expected test category, got: %s", result)
	}
}

func TestCategorizeChanges_Logging(t *testing.T) {
	diff := `diff --git a/handler.go b/handler.go
--- a/handler.go
+++ b/handler.go
@@ -1,2 +1,3 @@
 package handler
 
+	debugLog("something happened")
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Debug") {
		t.Errorf("expected debug/logging category, got: %s", result)
	}
}

func TestCategorizeChanges_CodeRemoval(t *testing.T) {
	diff := `diff --git a/old.go b/old.go
--- a/old.go
+++ b/old.go
@@ -1,5 +1,2 @@
 package old
 
-func oldCode() {
-	// deprecated
-}
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "removal") || !strings.Contains(result, "refactoring") {
		t.Errorf("expected code removal/refactoring category, got: %s", result)
	}
}

func TestCategorizeChanges_Empty(t *testing.T) {
	result := categorizeChanges("")
	if result != "" {
		t.Errorf("expected empty result for empty diff, got: %q", result)
	}
}

func TestCategorizeChanges_MixedCategories(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,8 @@
 package main
 
-oldFunction()
+	if err != nil {
+		return nil, err
+	}
+	// TODO: fix this
+	debugLog("tracing")
+func TestNew(t *testing.T) {}
`
	result := categorizeChanges(diff)
	if result == "" {
		t.Fatal("expected categories for mixed diff")
	}
	// Should contain multiple categories
	found := 0
	if strings.Contains(result, "Error handling") { found++ }
	if strings.Contains(result, "Test") { found++ }
	if strings.Contains(result, "Debug") { found++ }
	if strings.Contains(result, "removal") { found++ }
	if found < 2 {
		t.Errorf("expected at least 2 categories, got %d from: %s", found, result)
	}
}

// --- shouldSkipFileForContext ---

func TestShouldSkipFileForContext(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", false},
		{"pkg/utils/helper.go", false},
		{"go.sum", true},
		{"package-lock.json", true},
		{"yarn.lock", true},
		{"file.lock", true},
		{"bundle.min.js", true},
		{"app.min.css", true},
		{"source.map", true},
		{"file.map", true},
		{"node_modules/pkg/index.js", true},
		{"gen.pb.go", true},
		{"gen.pb.cc", true},
		{"gen.pb.h", true},
		{"zz_generated.go", true},
		{"api_generated.go", true},
		{"generated.proto", false},
		{"coverage.out", true},
		{"coverage.html", true},
		{"app.test", true},
		{"output.out", true},
		{"icon.svg", true},
		{"logo.png", true},
		{"photo.jpg", true},
		{"favicon.ico", true},
		{"vendor/pkg/lib.go", true},
		{".git/config", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := shouldSkipFileForContext(tt.path)
			if got != tt.want {
				t.Errorf("shouldSkipFileForContext(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// --- isValidRepoFilePath ---

func TestIsValidRepoFilePath(t *testing.T) {
	// Test files within CWD (the project root)
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "simple file in cwd",
			path: "main.go",
			want: true,
		},
		{
			name: "subdirectory file",
			path: "pkg/git/commit.go",
			want: true,
		},
		{
			name: "parent directory traversal",
			path: "../etc/passwd",
			want: false,
		},
		{
			name: "absolute path to file in repo",
			path: "", // will be set to abs path dynamically
			want: true,
		},
		{
			name: "deep parent traversal",
			path: "../../../../etc/shadow",
			want: false,
		},
		{
			name: "mixed traversal then valid",
			path: "../src/../src/file.go",
			want: false,
		},
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absMainGo := filepath.Join(cwd, "main.go")
	tests[3].path = absMainGo

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRepoFilePath(tt.path)
			if got != tt.want {
				t.Errorf("isValidRepoFilePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsValidRepoFilePath_TempDirCleanup(t *testing.T) {
	// A file path that resolves outside cwd
	got := isValidRepoFilePath(filepath.Join(os.TempDir(), "somefile.txt"))
	// The result depends on whether TempDir is inside CWD; normally it's not
	if strings.HasPrefix(os.TempDir(), cwdStr()) {
		t.Skip("TempDir is inside cwd, skipping")
	}
	if got {
		t.Errorf("expected false for path outside cwd: %q", filepath.Join(os.TempDir(), "somefile.txt"))
	}
}

// --- extractStagedChangesSummary ---

func TestExtractStagedChangesSummary(t *testing.T) {
	// This function requires git to be available and to have staged changes.
	// Since we can't guarantee staged changes, we just verify it doesn't panic.
	result := extractStagedChangesSummary()
	// Result may be empty or non-empty; just verify it's a string
	_ = result
}

func cwdStr() string {
	d, _ := os.Getwd()
	return d
}

// =============================================================================
// New coverage-improvement tests
// =============================================================================

// --- displayVerboseLog ---

// captureStdout captures fmt.Print/Printf output from the given function.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestDisplayVerboseLog_NoLeditDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	out := captureStdout(t, displayVerboseLog)
	if !strings.Contains(out, "does not exist") {
		t.Errorf("expected 'does not exist' message, got: %s", out)
	}
}

func TestDisplayVerboseLog_NoWorkspaceLog(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ledit"), 0755)
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	out := captureStdout(t, displayVerboseLog)
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got: %s", out)
	}
}

func TestDisplayVerboseLog_EmptyLog(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ledit"), 0755)
	os.WriteFile(filepath.Join(dir, ".ledit", "workspace.log"), []byte(""), 0644)
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	out := captureStdout(t, displayVerboseLog)
	if !strings.Contains(out, "is empty") {
		t.Errorf("expected 'is empty' message, got: %s", out)
	}
}

func TestDisplayVerboseLog_WithContent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ledit"), 0755)
	content := "line one\nline two\nline three\n"
	os.WriteFile(filepath.Join(dir, ".ledit", "workspace.log"), []byte(content), 0644)
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	out := captureStdout(t, displayVerboseLog)
	if !strings.Contains(out, "line one") {
		t.Errorf("expected content to be displayed, got: %s", out)
	}
	if !strings.Contains(out, "line three") {
		t.Errorf("expected last line to be displayed, got: %s", out)
	}
	if !strings.Contains(out, "workspace.log") {
		t.Errorf("expected log file path in output, got: %s", out)
	}
}

// --- extractFileContextForChanges ---

func TestExtractFileContext_EmptyDiff(t *testing.T) {
	result := extractFileContextForChanges("")
	if result != "" {
		t.Errorf("expected empty string for empty diff, got: %q", result)
	}
}

func TestExtractFileContext_ExistingFiles(t *testing.T) {
	// Test with known existing files within the cmd package
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// We're already in cmd/ directory when tests run

	diff := `diff --git a/root.go b/root.go
--- a/root.go
+++ b/root.go
@@ -1,6 +1,7 @@
 package cmd
 
 import (
+	"fmt"
 	"os"
 	"sync"
 	tools "github.com/alantheprice/ledit/pkg/agent_tools"
+)
`

	result := extractFileContextForChanges(diff)
	if result == "" {
		t.Fatal("expected non-empty result for diff with existing root.go")
	}
	if !strings.Contains(result, "root.go") {
		t.Errorf("expected root.go in context, got: %s", result)
	}
	if !strings.Contains(result, "package cmd") {
		t.Errorf("expected file content in context, got: %s", result)
	}
}

func TestExtractFileContext_DeletedFiles(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	diff := `diff --git a/nonexistent_deleted_file.go b/nonexistent_deleted_file.go
deleted file mode 100644
--- a/nonexistent_deleted_file.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package deleted
-
-func deletedFunc() {}
`

	result := extractFileContextForChanges(diff)
	// Deleted file doesn't exist on disk, should be skipped
	if result != "" {
		t.Errorf("expected empty result for diff with only deleted files, got: %q", result)
	}
}

func TestExtractFileContext_SkipFiles(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	diff := `diff --git a/vendor/lib.go b/vendor/lib.go
--- a/vendor/lib.go
+++ b/vendor/lib.go
@@ -1,2 +1,3 @@
 package vendor
+func vendorFunc() {}
diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1,2 +1,3 @@
-sum1
+sum1
+sum2
`

	result := extractFileContextForChanges(diff)
	// Both vendor/ and .sum files should be skipped
	if result != "" {
		t.Errorf("expected empty result when all files are skipped, got: %q", result)
	}
}

func TestExtractFileContext_PathTraversal(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	diff := `diff --git a/../../../etc/passwd b/../../../etc/passwd
--- a/../../../etc/passwd
+++ b/../../../etc/passwd
@@ -1,2 +1,3 @@
 root:x:0:0
+attacker
`

	result := extractFileContextForChanges(diff)
	// Paths with .. should be rejected by isValidRepoFilePath
	if result != "" {
		t.Errorf("expected empty result for path traversal attempt, got: %q", result)
	}
}

// --- isValidRepoFilePath edge cases ---

func TestIsValidRepoFilePath_RelativePathInCwd(t *testing.T) {
	// Test with explicit ./ prefix (relative path that resolves inside cwd)
	relPath := "./main.go"
	orgDir, _ := os.Getwd()
	defer os.Chdir(orgDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	got := isValidRepoFilePath(relPath)
	if !got {
		t.Errorf("isValidRepoFilePath(%q) = false, want true", relPath)
	}
}

func TestIsValidRepoFilePath_SpecialChars(t *testing.T) {
	// A path with special characters (unicode) but still a valid relative path.
	// Even if the file doesn't exist, the path validation should not reject it
	// based on characters alone — only on traversal and cwd membership.
	orgDir, _ := os.Getwd()
	defer os.Chdir(orgDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	// This is technically a valid path name (no .. traversal)
	specialPath := "cmd/søme_fíle.go"
	got := isValidRepoFilePath(specialPath)
	if !got {
		t.Errorf("isValidRepoFilePath(%q) = false, want true (special chars are ok)", specialPath)
	}
}

func TestIsValidRepoFilePath_AbsPathStartsWithCwd(t *testing.T) {
	// An absolute path that starts exactly with cwd should be valid
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	absPath := filepath.Join(projectRoot, "main.go")
	got := isValidRepoFilePath(absPath)
	if !got {
		t.Errorf("isValidRepoFilePath(%q) = false, want true", absPath)
	}
}

func TestIsValidRepoFilePath_AbsPathOutsideCwd(t *testing.T) {
	// An absolute path that does NOT start with cwd should be invalid
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	projectRoot, _ := filepath.Abs("..")
	os.Chdir(projectRoot)

	// Use /tmp which should not be inside the project root
	tmpDir := filepath.Clean(os.TempDir())
	if strings.HasPrefix(tmpDir, projectRoot) {
		t.Skip("TempDir is inside the project root, cannot test path outside cwd")
	}

	absPath := filepath.Join(tmpDir, "somefile.txt")
	got := isValidRepoFilePath(absPath)
	if got {
		t.Errorf("isValidRepoFilePath(%q) = true, want false (outside cwd)", absPath)
	}
}

// --- extractStagedChangesSummary ---

func TestExtractStagedChangesSummary_NoGitRepo(t *testing.T) {
	// In a temp directory with no git repo, git diff should fail → returns ""
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	result := extractStagedChangesSummary()
	if result != "" {
		t.Errorf("expected empty string when no git repo, got: %q", result)
	}
}

// --- categorizeChanges Documentation category ---

func TestCategorizeChanges_Documentation(t *testing.T) {
	// Test .md suffix in an added line
	diff := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,2 +1,3 @@
 # Readme
-Old docs
+New docs
+See CHANGELOG.md
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Documentation") {
		t.Errorf("expected Documentation category for .md reference, got: %s", result)
	}
}

func TestCategorizeChanges_DocumentKeyword(t *testing.T) {
	// Test DOCUMENT keyword in added line
	diff := `diff --git a/handler.go b/handler.go
--- a/handler.go
+++ b/handler.go
@@ -1,2 +1,4 @@
 package handler
 
+// DOCUMENT: this function documents the API contract
+func handler() {}
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Documentation") {
		t.Errorf("expected Documentation category for DOCUMENT keyword, got: %s", result)
	}
}

func TestCategorizeChanges_CommentKeyword(t *testing.T) {
	// Test COMMENT keyword triggers Documentation
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,2 +1,3 @@
 package main
+// COMMENT: explains behavior
`
	result := categorizeChanges(diff)
	if !strings.Contains(result, "Documentation") {
		t.Errorf("expected Documentation category for COMMENT keyword, got: %s", result)
	}
}
