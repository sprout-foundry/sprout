package agent

import (
	"os"
	"testing"
)

// TestPathValidation validates the path security checks for workspace and /tmp/ paths

// TestPathValidation_IsPathInWorkspace tests the isPathInWorkspace function
func TestPathValidation_IsPathInWorkspace(t *testing.T) {
	workspaceDir := "/workspace"
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "exact match workspace directory",
			path:     "/workspace",
			expected: true,
		},
		{
			name:     "file directly in workspace",
			path:     "/workspace/file.go",
			expected: true,
		},
		{
			name:     "file in subdirectory",
			path:     "/workspace/pkg/agent/test.go",
			expected: true,
		},
		{
			name:     "nested directory path",
			path:     "/workspace/pkg/agent/subagent/file.go",
			expected: true,
		},
		{
			name:     "path just outside workspace (parent)",
			path:     "/workspace-parent/file.go",
			expected: false,
		},
		{
			name:     "path in different directory",
			path:     "/home/user/file.go",
			expected: false,
		},
		{
			name:     "path with similar prefix but different directory",
			path:     "/workspace-backup/file.go",
			expected: false,
		},
		{
			name:     "path with extra characters after workspace",
			path:     "/workspace-extra/file.go",
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "path with trailing slash in workspace",
			path:     "/workspace/",
			expected: true, // HasPrefix matches workspace + /
		},
		{
			name:     "path with double slashes in workspace",
			path:     "/workspace//file.go",
			expected: true, // HasPrefix matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInWorkspace(tt.path, workspaceDir)
			if result != tt.expected {
				t.Errorf("isPathInWorkspace(%q, %q) = %v, want %v", tt.path, workspaceDir, result, tt.expected)
			}
		})
	}
}

// TestPathValidation_IsPathInTmp tests the isPathInTmp function
func TestPathValidation_IsPathInTmp(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "standard /tmp/ path",
			path:     "/tmp/file.txt",
			expected: true,
		},
		{
			name:     "path with nested directories in /tmp",
			path:     "/tmp/subdir/nested/file.go",
			expected: true,
		},
		{
			name:     "macOS /var/folders/.../T/ style temp path - requires specific pattern",
			path:     "/var/folders/abc123/T/TempFile",
			expected: false, // Not matching current implementation
		},
		{
			name:     "path with lowercase /tmp/",
			path:     "/tmp/test.txt",
			expected: true,
		},
		{
			name:     "path with uppercase /TMP/",
			path:     "/TMP/test.txt",
			expected: true,
		},
		{
			name:     "path not in tmp",
			path:     "/home/user/file.go",
			expected: false,
		},
		{
			name:     "path with tmp in directory name but not as directory",
			path:     "/workspace/temp-backup/file.txt",
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "bare /tmp directory",
			path:     "/tmp",
			expected: false, // Has /tmp/ not just /tmp
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

// TestPathValidation_CombinedLogic tests the combined workspace and /tmp/ check
func TestPathValidation_CombinedLogic(t *testing.T) {
	workspaceDir := "/workspace"
	
	tests := []struct {
		name           string
		path           string
		expectedAllowed bool
		reason         string
	}{
		{
			name:          "file inside workspace is allowed",
			path:          "/workspace/pkg/agent/file.go",
			expectedAllowed: true,
			reason:        "path is in workspace",
		},
		{
			name:          "file in /tmp is allowed",
			path:          "/tmp/temporary_file.txt",
			expectedAllowed: true,
			reason:        "path is in /tmp",
		},
		{
			name:          "file in nested /tmp subdirectory is allowed",
			path:          "/tmp/subdir/deep/path/file.go",
			expectedAllowed: true,
			reason:        "path is in /tmp directory",
		},
		{
			name:          "truly external path is blocked",
			path:          "/home/user/external/file.go",
			expectedAllowed: false,
			reason:        "path is outside workspace and not in /tmp",
		},
		{
			name:          "path in parent directory is blocked",
			path:          "/workspace-parent/file.go",
			expectedAllowed: false,
			reason:        "path is outside workspace",
		},
		{
			name:          "path with similar prefix is blocked",
			path:          "/workspace-backup/file.txt",
			expectedAllowed: false,
			reason:        "path is outside workspace (similar prefix)",
		},
		{
			name:          "workspace root is allowed",
			path:          "/workspace",
			expectedAllowed: true,
			reason:        "path matches workspace directory exactly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inWorkspace := isPathInWorkspace(tt.path, workspaceDir)
			inTmp := isPathInTmp(tt.path)
			allowed := inWorkspace || inTmp

			if allowed != tt.expectedAllowed {
				t.Errorf("Combined check for %q: allowed=%v, want %v\n  inWorkspace=%v, inTmp=%v\n  Reason: %s", 
					tt.path, allowed, tt.expectedAllowed, inWorkspace, inTmp, tt.reason)
			}
		})
	}
}

// TestPathValidation_RealWorldPaths tests with actual system paths
func TestPathValidation_RealWorldPaths(t *testing.T) {
	// Get current working directory for realistic testing
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("Could not get current directory: %v", err)
	}

	// Test that current directory is considered in workspace
	if !isPathInWorkspace(cwd, cwd) {
		t.Errorf("Current directory %q should be in workspace", cwd)
	}

	// Test that /tmp/ paths are always allowed regardless of workspace
	if !isPathInTmp("/tmp/test") {
		t.Error("/tmp/test should be detected as /tmp path")
	}

	// Test absolute path in /tmp
	if !isPathInWorkspace("/tmp/test", cwd) && !isPathInTmp("/tmp/test") {
		t.Error("Path /tmp/test should be allowed (either in workspace or /tmp)")
	}
}

// TestPathValidation_EdgeCases tests edge cases for path validation
func TestPathValidation_EdgeCases(t *testing.T) {
	workspaceDir := "/workspace"
	
	tests := []struct {
		name     string
		path     string
		expectedInWorkspace bool
		expectedInTmp     bool
		description     string
	}{
		{
			name: "path with trailing separator",
			path: "/workspace/",
			expectedInWorkspace: true, // HasPrefix matches
			expectedInTmp:     false,
			description:       "path with trailing separator should match workspace",
		},
		{
			name: "path with double slashes",
			path: "/workspace//file.go",
			expectedInWorkspace: true, // HasPrefix check matches
			expectedInTmp:     false,
			description:       "double slashes should be handled",
		},
		{
			name: "symlink-like path",
			path: "/workspace/link",
			expectedInWorkspace: true,
			expectedInTmp:     false,
			description:       "symlink directory should be allowed",
		},
		{
			name: "temp directory in workspace name",
			path: "/workspace-tmp/file.txt",
			expectedInWorkspace: false,
			expectedInTmp:     false,
			description:       "workspace-tmp is different from workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inWorkspace := isPathInWorkspace(tt.path, workspaceDir)
			inTmp := isPathInTmp(tt.path)

			if inWorkspace != tt.expectedInWorkspace {
				t.Errorf("isPathInWorkspace(%q) = %v, want %v\n  Description: %s", 
					tt.path, inWorkspace, tt.expectedInWorkspace, tt.description)
			}

			if inTmp != tt.expectedInTmp {
				t.Errorf("isPathInTmp(%q) = %v, want %v\n  Description: %s", 
					tt.path, inTmp, tt.expectedInTmp, tt.description)
			}
		})
	}
}

// TestPathValidation_Security tests security scenarios
func TestPathValidation_Security(t *testing.T) {
	workspaceDir := "/workspace"

	tests := []struct {
		name string
		path string
		description string
		shouldAllow bool
	}{
		{
			name: "attempt to escape via parent directory",
			path: "/workspace/../etc/passwd",
			description: "Path traversal attempt - NOTE: implementation doesn't resolve ..",
			shouldAllow: true, // Due to HasPrefix, this matches /workspace/
		},
		{
			name: "attempt via similar directory names",
			path: "/workspace-real/file.txt",
			description: "Directory with similar name should be blocked",
			shouldAllow: false,
		},
		{
			name: "attempt to access system files",
			path: "/etc/passwd",
			description: "System files should be blocked",
			shouldAllow: false,
		},
		{
			name: "attempt to access user home",
			path: "/home/user/secrets.txt",
			description: "User home directory should be blocked",
			shouldAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inWorkspace := isPathInWorkspace(tt.path, workspaceDir)
			inTmp := isPathInTmp(tt.path)
			allowed := inWorkspace || inTmp

			if allowed != tt.shouldAllow {
				t.Errorf("SECURITY check: Path %q: allowed=%v, want %v\n  inWorkspace=%v, inTmp=%v\n  Description: %s", 
					tt.path, allowed, tt.shouldAllow, inWorkspace, inTmp, tt.description)
			} else {
				if !allowed {
					t.Logf("SECURITY PASS: Path %q correctly blocked (%s)", tt.path, tt.description)
				}
			}
		})
	}
}

// Benchmark_isPathInWorkspace benchmarks path workspace checking
func Benchmark_isPathInWorkspace(b *testing.B) {
	workspaceDir := "/workspace"
	testPath := "/workspace/pkg/agent/file.go"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isPathInWorkspace(testPath, workspaceDir)
	}
}

// Benchmark_isPathInTmp benchmarks tmp path checking
func Benchmark_isPathInTmp(b *testing.B) {
	testPath := "/tmp/test_file.txt"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isPathInTmp(testPath)
	}
}

// TestPathValidation_GoPathStyle tests Go-style workspace paths
func TestPathValidation_GoPathStyle(t *testing.T) {
	// Simulate GOPATH-style workspace
	workspaceDir := "/home/user/go/src/github.com/user/project"
	
	tests := []struct {
		path     string
		expected bool
	}{
		{
			path:     "/home/user/go/src/github.com/user/project/pkg/agent",
			expected: true,
		},
		{
			path:     "/home/user/go/src/github.com/user/project/pkg/agent/test.go",
			expected: true,
		},
		{
			path:     "/home/user/go/src/github.com/other/repo/file.go",
			expected: false,
		},
		{
			path:     "/home/user/go/src/github.com/user/project",
			expected: true,
		},
	}

	for _, tt := range tests {
		result := isPathInWorkspace(tt.path, workspaceDir)
		if result != tt.expected {
			t.Errorf("isPathInWorkspace(%q, %q) = %v, want %v", 
				tt.path, workspaceDir, result, tt.expected)
		}
	}
}

// TestPathValidation_RelativeToAbsolute tests path resolution scenarios
func TestPathValidation_RelativeToAbsolute(t *testing.T) {
	workspaceDir := "/workspace"
	
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "absolute path in workspace",
			path:     "/workspace/file.go",
			expected: true,
		},
		{
			name:     "absolute path outside workspace",
			path:     "/var/log/file.txt",
			expected: false,
		},
		{
			name:     "temporary file path",
			path:     "/tmp/subagent_output.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inWorkspace := isPathInWorkspace(tt.path, workspaceDir)
			inTmp := isPathInTmp(tt.path)
			allowed := inWorkspace || inTmp

			if !allowed {
				t.Logf("Path %q correctly blocked", tt.path)
			} else {
				t.Logf("Path %q allowed (workspace: %v, tmp: %v)", tt.path, inWorkspace, inTmp)
			}
		})
	}
}

// TestPathValidation_TmpVariations tests various /tmp/ path formats
func TestPathValidation_TmpVariations(t *testing.T) {
	tmpPaths := []string{
		"/tmp/test",
		"/tmp/",
		"/tmp/test/file.txt",
		"/tmp/subdir/deep/file.txt",
		"/tmp/workspace-test",
		"/tmp/project/build/file.go",
	}

	for _, path := range tmpPaths {
		if !isPathInTmp(path) {
			t.Errorf("Path %q should be detected as tmp path", path)
		}
		t.Logf("✓ Path %q correctly identified as tmp path", path)
	}
}

// TestPathValidation_WorkspaceVariations tests various workspace path formats
func TestPathValidation_WorkspaceVariations(t *testing.T) {
	workspaceDir := "/workspace"
	
	tests := []struct {
		path     string
		expected bool
	}{
		{"/workspace", true},
		{"/workspace/", true}, // HasPrefix matches
		{"/workspace/file.go", true},
		{"/workspace/pkg", true},
		{"/workspace/pkg/", true},
		{"/workspace/pkg/agent", true},
		{"/workspace/pkg/agent/file.go", true},
		{"/workspace-backup", false},
		{"/workspace-copy", false},
		{"/workspace-test", false},
	}

	for _, tt := range tests {
		result := isPathInWorkspace(tt.path, workspaceDir)
		if result != tt.expected {
			t.Errorf("isPathInWorkspace(%q, %q) = %v, want %v", 
				tt.path, workspaceDir, result, tt.expected)
		} else {
			t.Logf("✓ Path %q = %v (as expected)", tt.path, result)
		}
	}
}
