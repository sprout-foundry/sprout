package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExtractor_Extract_Supported(t *testing.T) {
	ext := NewFileExtractor(8000)

	tests := []struct {
		name      string
		path      string
		content   string
		wantLang  string
		wantCount int
	}{
		{
			name:      "markdown file",
			path:      "README.md",
			content:   "# Project Title\n\nThis is a test.",
			wantLang:  "markdown",
			wantCount: 1,
		},
		{
			name:      "yaml file",
			path:      "config.yaml",
			content:   "key: value\nnested:\n  item: test",
			wantLang:  "yaml",
			wantCount: 1,
		},
		{
			name:      "json file",
			path:      "package.json",
			content:   `{"name": "test", "version": "1.0"}`,
			wantLang:  "json",
			wantCount: 1,
		},
		{
			name:      "shell script",
			path:      "script.sh",
			content:   "#!/bin/bash\necho 'hello'",
			wantLang:  "shell",
			wantCount: 1,
		},
		{
			name:      "dockerfile",
			path:      "Dockerfile",
			content:   "FROM alpine\nRUN echo 'build'",
			wantLang:  "dockerfile",
			wantCount: 1,
		},
		{
			name:      "Makefile",
			path:      "Makefile",
			content:   "build:\n\tgo build .",
			wantLang:  "text", // Falls back to text
			wantCount: 1,
		},
		{
			name:      "gitignore",
			path:      ".gitignore",
			content:   "*.tmp\nnode_modules/",
			wantLang:  "config",
			wantCount: 1,
		},
		{
			name:      "AGENTS.md",
			path:      "AGENTS.md",
			content:   "# Agent Documentation\n\nAgent details here.",
			wantLang:  "markdown",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units, err := ext.Extract(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			if len(units) != tt.wantCount {
				t.Fatalf("Extract() got %d units, want %d", len(units), tt.wantCount)
			}

			if len(units) > 0 {
				unit := units[0]
				if unit.File != tt.path {
					t.Errorf("Extract().File = %v, want %v", unit.File, tt.path)
				}
				if unit.Name != filepath.Base(tt.path) {
					t.Errorf("Extract().Name = %v, want %v", unit.Name, filepath.Base(tt.path))
				}
				if unit.Language != tt.wantLang {
					t.Errorf("Extract().Language = %v, want %v", unit.Language, tt.wantLang)
				}
				if unit.Signature != filepath.Base(tt.path) {
					t.Errorf("Extract().Signature = %v, want %v", unit.Signature, filepath.Base(tt.path))
				}
				if unit.Hash == "" {
					t.Errorf("Extract().Hash should not be empty")
				}
			}
		})
	}
}

func TestFileExtractor_Extract_Unsupported(t *testing.T) {
	ext := NewFileExtractor(8000)

	tests := []struct {
		name    string
		path    string
		content string
	}{
		{
			name:    "go file (not supported for file-level)",
			path:    "main.go",
			content: "package main",
		},
		{
			name:    "python file",
			path:    "test.py",
			content: "def test(): pass",
		},
		{
			name:    "typescript file",
			path:    "app.ts",
			content: "export const x = 1;",
		},
		{
			name:    "unknown extension",
			path:    "file.xyz",
			content: "some content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units, err := ext.Extract(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			if len(units) != 0 {
				t.Errorf("Extract() got %d units, want 0 (unsupported file)", len(units))
			}
		})
	}
}

func TestFileExtractor_Truncate(t *testing.T) {
	ext := NewFileExtractor(100) // Small limit

	// Create content larger than limit
	content := ""
	for i := 0; i < 1000; i++ {
		content += "line " + string(rune('a'+(i%26))) + "\n"
	}

	units, err := ext.Extract("test.txt", []byte(content))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("Extract() got %d units, want 1", len(units))
	}

	// Body should be truncated
	unit := units[0]
	if len(unit.Body) > 200 { // 100 bytes + some margin
		t.Errorf("Extract().Body length = %d, want <= 200", len(unit.Body))
	}
}

func TestFileExtractor_CleanMarkdown(t *testing.T) {
	ext := NewFileExtractor(8000)

	content := `<h1>Title</h1>

This is **bold** and <em>italic</em>.


Extra blank lines above and below.`

	units, err := ext.Extract("README.md", []byte(content))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("Extract() got %d units, want 1", len(units))
	}

	body := units[0].Body
	// HTML tags should be removed
	if contains(body, "<h1>") || contains(body, "<em>") {
		t.Errorf("Extract() body still contains HTML tags: %q", body)
	}
}

func TestFileExtractor_NewFileExtractor_Default(t *testing.T) {
	ext := NewFileExtractor(0)
	if ext.maxFileBytes != 8000 {
		t.Errorf("NewFileExtractor(0) maxFileBytes = %d, want 8000", ext.maxFileBytes)
	}

	ext = NewFileExtractor(-1)
	if ext.maxFileBytes != 8000 {
		t.Errorf("NewFileExtractor(-1) maxFileBytes = %d, want 8000", ext.maxFileBytes)
	}
}

func TestHashContent(t *testing.T) {
	content1 := []byte("test content")
	content2 := []byte("test content")
	content3 := []byte("different content")

	hash1 := HashContent(content1)
	hash2 := HashContent(content2)
	hash3 := HashContent(content3)

	if hash1 != hash2 {
		t.Errorf("HashContent() same content produces different hashes: %v != %v", hash1, hash2)
	}

	if hash1 == hash3 {
		t.Errorf("HashContent() different content produces same hash: %v", hash1)
	}

	if hash1 == "" {
		t.Errorf("HashContent() returned empty string")
	}
}

func TestIsSupportedIndexableFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "README.md", want: true},
		{path: "config.yaml", want: true},
		{path: "Dockerfile", want: true},
		{path: "Makefile", want: true},
		{path: ".gitignore", want: true},
		{path: "AGENTS.md", want: true},
		{path: "script.sh", want: true},
		{path: "main.go", want: false},
		{path: "test.py", want: false},
		{path: "app.ts", want: false},
		{path: "file.xyz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := IsSupportedIndexableFile(tt.path)
			if got != tt.want {
				t.Errorf("IsSupportedIndexableFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestFileExtractor_RealFile(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a real markdown file
	content := `# Test README

This is a test README file.

## Features

- Feature one
- Feature two
`
	mdPath := filepath.Join(tmpDir, "README.md")
	err := os.WriteFile(mdPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Read and extract
	contentBytes, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	ext := NewFileExtractor(8000)
	units, err := ext.Extract(mdPath, contentBytes)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("Extract() got %d units, want 1", len(units))
	}

	unit := units[0]
	if unit.ID != mdPath {
		t.Errorf("Extract().ID = %v, want %v", unit.ID, mdPath)
	}
	if unit.File != mdPath {
		t.Errorf("Extract().File = %v, want %v", unit.File, mdPath)
	}
	if unit.Name != "README.md" {
		t.Errorf("Extract().Name = %v, want README.md", unit.Name)
	}
	if unit.Language != "markdown" {
		t.Errorf("Extract().Language = %v, want markdown", unit.Language)
	}
	if unit.Hash == "" {
		t.Errorf("Extract().Hash should not be empty")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
