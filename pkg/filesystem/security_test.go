package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeResolvePath(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory (not /tmp to avoid the /tmp exception)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	tempDir := filepath.Join(homeDir, ".ledit-test-path-security")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	// Create test file structure
	testDir := filepath.Join(tempDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	testFile := filepath.Join(testDir, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name             string
		path             string
		wantErr          bool
		anyErrorContains []string // Any of these strings in error message is acceptable
	}{
		{
			name:             "valid file in current directory",
			path:             "test.txt",
			wantErr:          true, // File doesn't exist, but path validation passes
			anyErrorContains: nil,
		},
		{
			name:             "valid file in subdirectory",
			path:             filepath.Join("testdir", "testfile.txt"),
			wantErr:          false,
			anyErrorContains: nil,
		},
		{
			name:             "path traversal attempt - absolute",
			path:             "/etc/passwd",
			wantErr:          true,
			anyErrorContains: []string{"file access outside working directory", "failed to resolve", "no such file"},
		},
		{
			name:             "path traversal attempt - parent",
			path:             "../../../etc/passwd",
			wantErr:          true,
			anyErrorContains: []string{"file access outside working directory", "no such file"},
		},
		{
			name:             "path traversal attempt - dotdot",
			path:             "../test.txt",
			wantErr:          true,
			anyErrorContains: []string{"file access outside working directory", "no such file"},
		},
		{
			name:             "path traversal attempt - windows style",
			path:             "..\\test.txt",
			wantErr:          true,
			anyErrorContains: []string{"file access outside working directory", "no such file"},
		},
		{
			name:             "empty path",
			path:             "",
			wantErr:          true,
			anyErrorContains: nil,
		},
		{
			name:             "valid relative path with subdirs",
			path:             "testdir/testfile.txt",
			wantErr:          false,
			anyErrorContains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeResolvePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && len(tt.anyErrorContains) > 0 {
				// Check if error contains any of the acceptable substrings
				matched := false
				for _, substr := range tt.anyErrorContains {
					if strings.Contains(err.Error(), substr) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("SafeResolvePath() error = %v, should contain one of %v", err, tt.anyErrorContains)
				}
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePath() returned empty path, expected valid path")
			}
		})
	}
}

func TestSafeResolvePathSymlinks(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory (not /tmp to avoid the /tmp exception)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	tempDir := filepath.Join(homeDir, ".ledit-test-symlink-security")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	// Create test directory and file
	testDir := filepath.Join(tempDir, "targetdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	testFile := filepath.Join(testDir, "targetfile.txt")
	if err := os.WriteFile(testFile, []byte("target content"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create a symlink to a file within the directory
	symlinkFile := filepath.Join(tempDir, "link.txt")
	if err := os.Symlink(testFile, symlinkFile); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Create a symlink to a directory within the directory
	symlinkDir := filepath.Join(tempDir, "linkdir")
	if err := os.Symlink(testDir, symlinkDir); err != nil {
		t.Fatalf("Failed to create dir symlink: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid symlink to file",
			path:    "link.txt",
			wantErr: false,
		},
		{
			name:    "valid file via directory symlink",
			path:    filepath.Join("linkdir", "targetfile.txt"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeResolvePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePath() returned empty path, expected valid path")
			}
		})
	}
}

func TestSafeResolvePathForWrite(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory (not /tmp to avoid the /tmp exception)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	tempDir := filepath.Join(homeDir, ".ledit-test-write-security")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	// Create test directory
	testDir := filepath.Join(tempDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid write to current directory",
			path:    "newfile.txt",
			wantErr: false,
		},
		{
			name:    "valid write to subdirectory",
			path:    filepath.Join("testdir", "newfile.txt"),
			wantErr: false,
		},
		{
			name:    "valid write to nested subdirectory",
			path:    filepath.Join("testdir", "subdir", "newfile.txt"),
			wantErr: false,
		},
		{
			name:    "path traversal attempt - parent",
			path:    "../newfile.txt",
			wantErr: true,
		},
		{
			name:    "path traversal attempt - absolute",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeResolvePathForWrite(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathForWrite() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePathForWrite() returned empty path, expected valid path")
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tempFile, err := os.CreateTemp("", "test-fileexists-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	nonExistentPath := "/this/path/does/not/exist/file.txt"

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: tempFile.Name(),
			want: true,
		},
		{
			name: "non-existent file",
			path: nonExistentPath,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FileExists(tt.path)
			if got != tt.want {
				t.Errorf("FileExists() = %v, want %v", got, tt.want)
			}
		})
	}
}
