package cmd

import (
	"fmt"
	"os"
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
