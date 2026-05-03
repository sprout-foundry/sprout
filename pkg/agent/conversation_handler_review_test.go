package agent

import (
	"testing"
)

func TestHasCodeLikeTrackedFiles_GoExtension(t *testing.T) {
	if !hasCodeLikeTrackedFiles([]string{"main.go"}) {
		t.Error("should detect .go files")
	}
}

func TestHasCodeLikeTrackedFiles_PythonExtension(t *testing.T) {
	if !hasCodeLikeTrackedFiles([]string{"app.py"}) {
		t.Error("should detect .py files")
	}
}

func TestHasCodeLikeTrackedFiles_JavaScript(t *testing.T) {
	tests := []struct {
		files []string
		want  bool
	}{
		{[]string{"app.js"}, true},
		{[]string{"app.ts"}, true},
		{[]string{"app.tsx"}, true},
		{[]string{"app.jsx"}, true},
		{[]string{"README.md"}, false},
		{[]string{"image.png"}, false},
		{[]string{"data.csv"}, false},
	}
	for _, tc := range tests {
		got := hasCodeLikeTrackedFiles(tc.files)
		if got != tc.want {
			t.Errorf("hasCodeLikeTrackedFiles(%v) = %v, want %v", tc.files, got, tc.want)
		}
	}
}

func TestHasCodeLikeTrackedFiles_AllCodeExtensions(t *testing.T) {
	codeFiles := []string{
		"file.go", "file.py", "file.rs", "file.java", "file.c", "file.cc", "file.cpp",
		"file.h", "file.cs", "file.rb", "file.php", "file.swift", "file.kt",
		"file.sh", "file.bash", "file.zsh", "file.sql", "file.html", "file.css",
		"file.yaml", "file.toml", "file.json", "file.proto", "file.tf",
	}
	for _, f := range codeFiles {
		if !hasCodeLikeTrackedFiles([]string{f}) {
			t.Errorf("should detect %q as code file", f)
		}
	}
}

func TestHasCodeLikeTrackedFiles_CodeBasenames(t *testing.T) {
	basenames := []string{
		"dockerfile", "Dockerfile", "makefile", "Makefile",
		"justfile", "Justfile", "cmakelists.txt", "CMakeLists.txt",
		"build.gradle", "build.gradle.kts",
	}
	for _, name := range basenames {
		if !hasCodeLikeTrackedFiles([]string{name}) {
			t.Errorf("should detect %q as code basename", name)
		}
	}
}

func TestHasCodeLikeTrackedFiles_EmptyList(t *testing.T) {
	if hasCodeLikeTrackedFiles([]string{}) {
		t.Error("empty list should return false")
	}
	if hasCodeLikeTrackedFiles(nil) {
		t.Error("nil list should return false")
	}
}

func TestHasCodeLikeTrackedFiles_NonCodeFiles(t *testing.T) {
	nonCode := []string{"README.md", "LICENSE", "image.png", "data.csv", "notes.txt"}
	if hasCodeLikeTrackedFiles(nonCode) {
		t.Error("non-code files should return false")
	}
}

func TestHasCodeLikeTrackedFiles_MixedFiles(t *testing.T) {
	mixed := []string{"README.md", "main.go"}
	if !hasCodeLikeTrackedFiles(mixed) {
		t.Error("should return true when at least one code file exists")
	}
}

func TestHasCodeLikeTrackedFiles_EmptyStrings(t *testing.T) {
	if hasCodeLikeTrackedFiles([]string{"", "  ", "README.md"}) {
		t.Error("should only count non-empty strings with code extensions")
	}
}

func TestHasCodeLikeTrackedFiles_PathWithDirectory(t *testing.T) {
	if !hasCodeLikeTrackedFiles([]string{"src/main.go"}) {
		t.Error("should detect code file with directory prefix")
	}
}

// --- isSelfReviewGatePersonaEnabled ---

func TestIsSelfReviewGatePersona(t *testing.T) {
	tests := []struct {
		persona string
		want    bool
	}{
		{"orchestrator", true},
		{"Orchestrator", true},
		{"ORCHESTRATOR", true},
		{"repo_orchestrator", true},
		{"coder", true},
		{"Coder", true},
		{"debugger", false},
		{"tester", false},
		{"code_reviewer", false},
		{"", false},
		{"  ", false},
		{"general", false},
		{"researcher", false},
	}
	for _, tc := range tests {
		got := isSelfReviewGatePersonaEnabled(tc.persona)
		if got != tc.want {
			t.Errorf("isSelfReviewGatePersonaEnabled(%q) = %v, want %v", tc.persona, got, tc.want)
		}
	}
}
