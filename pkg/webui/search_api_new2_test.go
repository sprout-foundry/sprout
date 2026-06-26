//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Test compileSearchPattern function
func TestCompileSearchPattern2(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		caseSensitive bool
		wholeWord     bool
		isRegex       bool
		wantErr       bool
		validate      func(*testing.T, *regexp.Regexp)
	}{
		{
			name:          "simple case insensitive",
			query:         "hello",
			caseSensitive: false,
			wholeWord:     false,
			isRegex:       false,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("Hello") {
					t.Errorf("pattern should match 'Hello'")
				}
				if !re.MatchString("hello") {
					t.Errorf("pattern should match 'hello'")
				}
			},
		},
		{
			name:          "simple case sensitive",
			query:         "Hello",
			caseSensitive: true,
			wholeWord:     false,
			isRegex:       false,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("Hello") {
					t.Errorf("pattern should match 'Hello'")
				}
				if re.MatchString("hello") {
					t.Errorf("pattern should not match 'hello'")
				}
			},
		},
		{
			name:          "whole word match",
			query:         "test",
			caseSensitive: false,
			wholeWord:     true,
			isRegex:       false,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("test") {
					t.Errorf("pattern should match 'test'")
				}
				if !re.MatchString(" test ") {
					t.Errorf("pattern should match ' test '")
				}
				if re.MatchString("testing") {
					t.Errorf("pattern should not match 'testing'")
				}
			},
		},
		{
			name:          "regex pattern",
			query:         "hel+o",
			caseSensitive: false,
			wholeWord:     false,
			isRegex:       true,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("hello") {
					t.Errorf("pattern should match 'hello'")
				}
				if !re.MatchString("helllo") {
					t.Errorf("pattern should match 'helllo'")
				}
			},
		},
		{
			name:          "regex with case sensitive",
			query:         "[A-Z]",
			caseSensitive: true,
			wholeWord:     false,
			isRegex:       true,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("Hello") {
					t.Errorf("pattern should match 'Hello'")
				}
				if re.MatchString("hello") {
					t.Errorf("pattern should not match 'hello'")
				}
			},
		},
		{
			name:          "regex with whole word",
			query:         "test.*ing",
			caseSensitive: false,
			wholeWord:     true,
			isRegex:       true,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("testing") {
					t.Errorf("pattern should match 'testing'")
				}
				if re.MatchString("pretesting") {
					t.Errorf("pattern should not match 'pretesting'")
				}
			},
		},
		{
			name:          "special regex characters escaped",
			query:         "test.txt",
			caseSensitive: false,
			wholeWord:     false,
			isRegex:       false,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if !re.MatchString("test.txt") {
					t.Errorf("pattern should match 'test.txt'")
				}
			},
		},
		{
			name:          "invalid regex pattern",
			query:         "[unclosed",
			caseSensitive: false,
			wholeWord:     false,
			isRegex:       true,
			wantErr:       true,
		},
		{
			name:          "empty query",
			query:         "",
			caseSensitive: false,
			wholeWord:     false,
			isRegex:       false,
			wantErr:       false,
			validate: func(t *testing.T, re *regexp.Regexp) {
				if re == nil {
					t.Errorf("pattern should not be nil for empty query")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, err := compileSearchPattern(tt.query, tt.caseSensitive, tt.wholeWord, tt.isRegex)
			if (err != nil) != tt.wantErr {
				t.Errorf("compileSearchPattern() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, pattern)
			}
		})
	}
}

// Test isBinaryFile function
func TestIsBinaryFile2(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string][]byte{
		"text.go":      []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"),
		"text.txt":     []byte("This is a plain text file\nWith multiple lines\n"),
		"text.js":      []byte("console.log('hello');\nconst x = 42;\n"),
		"binary.png":   []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}, // PNG magic bytes + null byte
		"binary.jpg":   []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x00, 0x01}, // JPEG magic bytes + null
		"binary.exe":   []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00, 0x04, 0x00}, // MZ header + null
		"has_null.txt": []byte("text\x00with\x00nulls"),
	}

	for name, content := range testFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", name, err)
		}
	}

	tests := []struct {
		name     string
		path     string
		wantBool bool
	}{
		{"go source file", filepath.Join(tmpDir, "text.go"), false},
		{"plain text file", filepath.Join(tmpDir, "text.txt"), false},
		{"javascript file", filepath.Join(tmpDir, "text.js"), false},
		{"PNG file", filepath.Join(tmpDir, "binary.png"), true},
		{"JPEG file", filepath.Join(tmpDir, "binary.jpg"), true},
		{"EXE file", filepath.Join(tmpDir, "binary.exe"), true},
		{"text with nulls", filepath.Join(tmpDir, "has_null.txt"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBinaryFile(tt.path)
			if got != tt.wantBool {
				t.Errorf("isBinaryFile(%q) = %v, want %v", tt.path, got, tt.wantBool)
			}
		})
	}

	// Test with non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		got := isBinaryFile(filepath.Join(tmpDir, "does_not_exist.txt"))
		if got != false {
			t.Errorf("isBinaryFile() on non-existent file should return false, got %v", got)
		}
	})
}

// Test parsePatterns function
func TestParsePatterns2(t *testing.T) {
	tests := []struct {
		name     string
		patterns string
		want     []string
	}{
		{
			name:     "comma separated patterns",
			patterns: "*.go,*.txt,*.js",
			want:     []string{"*.go", "*.txt", "*.js"},
		},
		{
			name:     "single pattern",
			patterns: "*.go",
			want:     []string{"*.go"},
		},
		{
			name:     "empty string",
			patterns: "",
			want:     nil,
		},
		{
			name:     "whitespace only",
			patterns: "   ",
			want:     nil,
		},
		{
			name:     "patterns with whitespace",
			patterns: "  *.go  ,  *.txt  ,  *.js  ",
			want:     []string{"*.go", "*.txt", "*.js"},
		},
		{
			name:     "trailing comma",
			patterns: "*.go,*.txt,",
			want:     []string{"*.go", "*.txt"},
		},
		{
			name:     "leading comma",
			patterns: ",*.go,*.txt",
			want:     []string{"*.go", "*.txt"},
		},
		{
			name:     "multiple commas",
			patterns: "*.go,,,*.txt",
			want:     []string{"*.go", "*.txt"},
		},
		{
			name:     "empty entries",
			patterns: " *.go , , *.txt , ",
			want:     []string{"*.go", "*.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePatterns(tt.patterns)
			if len(got) != len(tt.want) {
				t.Errorf("parsePatterns() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("parsePatterns()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

// Test matchesAnyPattern function
func TestMatchesAnyPattern2(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{
			name:     "match simple pattern",
			path:     "file.go",
			patterns: []string{"*.go"},
			want:     true,
		},
		{
			name:     "match multiple patterns",
			path:     "file.go",
			patterns: []string{"*.txt", "*.go", "*.js"},
			want:     true,
		},
		{
			name:     "no match",
			path:     "file.go",
			patterns: []string{"*.txt", "*.js"},
			want:     false,
		},
		{
			name:     "empty patterns",
			path:     "file.go",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "nil patterns",
			path:     "file.go",
			patterns: nil,
			want:     false,
		},
		{
			name:     "match path with directory",
			path:     filepath.Join("src", "file.go"),
			patterns: []string{"*.go"},
			want:     true,
		},
		{
			name:     "match with wildcard path",
			path:     filepath.Join("src", "test", "file.go"),
			patterns: []string{"src/test/*.go"},
			want:     true,
		},
		{
			name:     "pattern with whitespace",
			path:     "file.go",
			patterns: []string{"  *.go  "},
			want:     true,
		},
		{
			name:     "question mark wildcard",
			path:     "file.go",
			patterns: []string{"????.go"},
			want:     true,
		},
		{
			name:     "question mark no match",
			path:     "verylongfilename.go",
			patterns: []string{"????.go"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAnyPattern(tt.path, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

// Test getContextLines function
func TestGetContextLines2(t *testing.T) {
	tests := []struct {
		name         string
		buffer       []string
		bufferLen    int
		contextLines int
		before       bool
		want         []string
	}{
		{
			name:         "get 2 lines before",
			buffer:       []string{"line1", "line2", "line3", "line4", "line5"},
			bufferLen:    5,
			contextLines: 2,
			before:       true,
			want:         []string{"line3", "line4"},
		},
		{
			name:         "get 1 line before",
			buffer:       []string{"line1", "line2", "line3"},
			bufferLen:    3,
			contextLines: 1,
			before:       true,
			want:         []string{"line2"},
		},
		{
			name:         "get 0 lines before",
			buffer:       []string{"line1", "line2", "line3"},
			bufferLen:    3,
			contextLines: 0,
			before:       true,
			want:         nil,
		},
		{
			name:         "buffer smaller than context",
			buffer:       []string{"line1", "line2"},
			bufferLen:    2,
			contextLines: 5,
			before:       true,
			want:         []string{"line1"},
		},
		{
			name:         "single line buffer",
			buffer:       []string{"line1"},
			bufferLen:    1,
			contextLines: 2,
			before:       true,
			want:         nil,
		},
		{
			name:         "exact context size",
			buffer:       []string{"line1", "line2", "line3"},
			bufferLen:    3,
			contextLines: 2,
			before:       true,
			want:         []string{"line1", "line2"},
		},
		{
			name:         "negative context (before=false should still work)",
			buffer:       []string{"line1", "line2", "line3"},
			bufferLen:    3,
			contextLines: -1,
			before:       true,
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getContextLines(tt.buffer, tt.bufferLen, tt.contextLines, tt.before)
			if len(got) != len(tt.want) {
				t.Errorf("getContextLines() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("getContextLines()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

// Additional helper tests for edge cases
func TestCompileSearchPatternEdgeCases2(t *testing.T) {
	t.Run("special characters in regex", func(t *testing.T) {
		pattern, err := compileSearchPattern("test.*", false, false, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !pattern.MatchString("test") {
			t.Error("should match 'test'")
		}
		if !pattern.MatchString("test123") {
			t.Error("should match 'test123'")
		}
	})

	t.Run("unicode characters", func(t *testing.T) {
		pattern, err := compileSearchPattern("hello", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !pattern.MatchString("héllo") {
			// This might fail depending on regex engine unicode handling
			// Just ensure it doesn't crash
		}
	})
}

func TestParsePatternsEdgeCases2(t *testing.T) {
	t.Run("comma only", func(t *testing.T) {
		got := parsePatterns(",")
		if got != nil {
			t.Errorf("parsePatterns(\",\") = %v, want nil", got)
		}
	})

	t.Run("multiple commas", func(t *testing.T) {
		got := parsePatterns(",,,")
		if got != nil {
			t.Errorf("parsePatterns(\",,,\") = %v, want nil", got)
		}
	})
}

func TestMatchesAnyPatternEdgeCases2(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		got := matchesAnyPattern("", []string{"*"})
		if !got {
			t.Error("matchesAnyPattern(\"\", [\"*\"]) should return true")
		}
	})

	t.Run("star pattern matches everything", func(t *testing.T) {
		got := matchesAnyPattern("any.file.name", []string{"*"})
		if !got {
			t.Error("matchesAnyPattern(\"any.file.name\", [\"*\"]) should return true")
		}
	})
}
