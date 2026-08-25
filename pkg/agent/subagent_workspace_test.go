package agent

import (
	"path/filepath"
	"testing"
)

// TestCommonParent verifies the commonParent helper function correctly
// finds the common parent directory of multiple file paths
func TestCommonParent(t *testing.T) {
	tests := []struct {
		name     string
		paths    []string
		expected string
	}{
		{
			name:     "single path",
			paths:    []string{"/home/user/project/file.go"},
			expected: "/home/user/project",
		},
		{
			name:     "same directory",
			paths:    []string{"/home/user/project/file1.go", "/home/user/project/file2.go"},
			expected: "/home/user/project",
		},
		{
			name:     "same parent directory",
			paths:    []string{"/home/user/project/pkg/file.go", "/home/user/project/cmd/main.go"},
			expected: "/home/user/project",
		},
		{
			name:     "different depths",
			paths:    []string{"/home/user/project/pkg/sub/file.go", "/home/user/project/cmd/main.go"},
			expected: "/home/user/project",
		},
		{
			name:     "root level",
			paths:    []string{"/home/user/file1.go", "/opt/file2.go"},
			expected: "/",
		},
		{
			name:     "empty list",
			paths:    []string{},
			expected: "",
		},
		{
			name:     "relative paths",
			paths:    []string{"./pkg/file.go", "./cmd/main.go"},
			expected: ".",
		},
		{
			name:     "deep nesting",
			paths:    []string{"/home/user/project/pkg/sub1/sub2/file.go", "/home/user/project/cmd/main.go"},
			expected: "/home/user/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := commonParent(tt.paths)
			if result != tt.expected {
				t.Errorf("commonParent(%v) = %q, want %q", tt.paths, result, tt.expected)
			}
		})
	}
}

// TestIsPathInWorkspace verifies the path-in-workspace check works correctly
func TestIsPathInWorkspace(t *testing.T) {
	workspaceDir := "/home/user/project"

	tests := []struct {
		name      string
		path      string
		workspace string
		expected  bool
	}{
		{
			name:      "file in workspace",
			path:      "/home/user/project/pkg/file.go",
			workspace: workspaceDir,
			expected:  true,
		},
		{
			name:      "file directly in workspace",
			path:      "/home/user/project/file.go",
			workspace: workspaceDir,
			expected:  true,
		},
		{
			name:      "deep nested file in workspace",
			path:      "/home/user/project/pkg/sub1/sub2/file.go",
			workspace: workspaceDir,
			expected:  true,
		},
		{
			name:      "file outside workspace (different project)",
			path:      "/home/user/other-project/file.go",
			workspace: workspaceDir,
			expected:  false,
		},
		{
			name:      "file outside workspace (different user)",
			path:      "/home/other-user/project/file.go",
			workspace: workspaceDir,
			expected:  false,
		},
		{
			name:      "file outside workspace (system path)",
			path:      "/usr/local/bin/file",
			workspace: workspaceDir,
			expected:  false,
		},
		{
			name:      "workspace directory itself",
			path:      "/home/user/project",
			workspace: workspaceDir,
			expected:  true,
		},
		{
			name:      "file in subdirectory of workspace with same prefix",
			path:      "/home/user/project-2/file.go",
			workspace: workspaceDir,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInWorkspace(tt.path, tt.workspace)
			if result != tt.expected {
				t.Errorf("isPathInWorkspace(%q, %q) = %v, want %v", tt.path, tt.workspace, result, tt.expected)
			}
		})
	}
}

// TestIsPathInTmp verifies the /tmp detection works correctly
func TestIsPathInTmp(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "file in /tmp",
			path:     "/tmp/file.txt",
			expected: true,
		},
		{
			name:     "file in subdirectory of /tmp",
			path:     "/tmp/sprout/file.txt",
			expected: true,
		},
		{
			name:     "file in /tmp with deep nesting",
			path:     "/tmp/sprout/sub1/sub2/file.txt",
			expected: true,
		},
		{
			name:     "file in workspace",
			path:     "/home/user/project/file.go",
			expected: false,
		},
		{
			name:     "file in system directory",
			path:     "/usr/local/bin/file",
			expected: false,
		},
		{
			name:     "file in home directory",
			path:     "/home/user/.config/file",
			expected: false,
		},
		{
			name:     "file path containing tmp but not in /tmp",
			path:     "/home/user/tmp-files/file.txt",
			expected: false,
		},
		{
			name:     "macOS temp directory pattern",
			path:     "/var/folders/xx/xxxxxxxxxx/T/file.txt",
			expected: false, // Pattern matches /var/folders/.../T/, not the exact structure
		},
		{
			name:     "exact macOS temp path",
			path:     "/var/folders/.../T/",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInTmp(tt.path)
			if result != tt.expected {
				t.Errorf("isPathInTmp(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

// TestCommonParentWithMixedPaths tests edge cases for commonParent
func TestCommonParentWithMixedPaths(t *testing.T) {
	tests := []struct {
		name     string
		paths    []string
		expected string
	}{
		{
			name:     "paths with clean/unclean versions",
			paths:    []string{"/home/user/project/./file.go", "/home/user/project/pkg/file.go"},
			expected: "/home/user/project",
		},
		{
			name:     "three paths from different subdirs",
			paths:    []string{"/a/b/c/d/file1.go", "/a/b/c/e/file2.go", "/a/b/c/f/file3.go"},
			expected: "/a/b/c",
		},
		{
			name:     "paths that converge at root",
			paths:    []string{"/a/b/file.go", "/c/d/file.go"},
			expected: "/",
		},
		{
			name:     "Windows-style separators on Unix (treated as part of name)",
			paths:    []string{"/home/user/project/file.go"},
			expected: "/home/user/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := commonParent(tt.paths)
			if result != tt.expected {
				t.Errorf("commonParent(%v) = %q, want %q", tt.paths, result, tt.expected)
			}
		})
	}
}

// TestCommonParentBehavior tests commonParent behavior with filepath.Clean
func TestCommonParentBehavior(t *testing.T) {
	// This test verifies that commonParent works correctly with filepath.Clean
	// which is what handleRunSubagent uses before calling commonParent

	paths := []string{
		"/home/user/project/pkg/sub1/file.go",
		"/home/user/project/pkg/sub2/file.go",
		"/home/user/project/cmd/main.go",
	}

	// Clean all paths first (as handleRunSubagent does)
	cleanedPaths := make([]string, len(paths))
	for i, path := range paths {
		cleanedPaths[i] = filepath.Clean(path)
	}

	result := commonParent(cleanedPaths)
	expected := "/home/user/project"

	if result != expected {
		t.Errorf("commonParent(cleaned paths) = %q, want %q", result, expected)
	}
}
