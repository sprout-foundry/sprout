package webui

import (
	"strings"
	"testing"
)

func TestReadStaticFile_InvalidPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{"path traversal dots", "../etc/passwd"},
		{"absolute path", "/etc/passwd"},
		{"backslash prefix", "\\machine\\share"},
		{"backslash only", "\\"},
		{"double dots in middle", "foo/../bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readStaticFile(tt.path)
			if err == nil {
				t.Errorf("readStaticFile(%q) = nil, want error", tt.path)
			} else {
				if !strings.Contains(err.Error(), "invalid path") {
					t.Errorf("readStaticFile(%q) error = %v, want 'invalid path'", tt.path, err)
				}
			}
		})
	}
}

func TestReadStaticFile_NonExistent(t *testing.T) {
	// "nonexistent_file_abc123.txt" is a valid name (no traversal, no absolute)
	// but should not exist in either the embed or the filesystem fallback
	_, err := readStaticFile("nonexistent_file_abc123.txt")
	if err == nil {
		t.Error("readStaticFile(nonexistent) = nil, want error")
	}
}
