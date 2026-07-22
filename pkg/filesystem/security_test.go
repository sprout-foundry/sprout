package filesystem

import (
	"context"
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
	tempDir := filepath.Join(homeDir, ".sprout-test-path-security")
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
	tempDir := filepath.Join(homeDir, ".sprout-test-symlink-security")
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
	tempDir := filepath.Join(homeDir, ".sprout-test-write-security")
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

func TestFilesExist(t *testing.T) {
	// Create temporary files
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")
	file3 := filepath.Join(tempDir, "file3.txt")

	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	tests := []struct {
		name      string
		filenames []string
		want      bool
		wantErr   bool
	}{
		{
			name:      "all files exist",
			filenames: []string{file1, file2},
			want:      true,
			wantErr:   false,
		},
		{
			name:      "one file missing",
			filenames: []string{file1, file3},
			want:      false,
			wantErr:   false,
		},
		{
			name:      "no files provided",
			filenames: []string{},
			want:      true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilesExist(tt.filenames...)
			if (err != nil) != tt.wantErr {
				t.Errorf("FilesExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FilesExist() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tempDir := t.TempDir()
	newDir := filepath.Join(tempDir, "new", "nested", "dir")

	err := EnsureDir(newDir)
	if err != nil {
		t.Fatalf("EnsureDir() failed: %v", err)
	}

	// Verify directory was created
	if !FileExists(newDir) {
		t.Errorf("Directory was not created: %s", newDir)
	}

	// Calling again should succeed (idempotent)
	err = EnsureDir(newDir)
	if err != nil {
		t.Errorf("EnsureDir() idempotent call failed: %v", err)
	}
}

func TestWriteFileWithDir(t *testing.T) {
	tempDir := t.TempDir()
	nestedPath := filepath.Join(tempDir, "subdir", "deep", "file.txt")

	data := []byte("test content")
	err := WriteFileWithDir(nestedPath, data, 0644)
	if err != nil {
		t.Fatalf("WriteFileWithDir() failed: %v", err)
	}

	// Verify file was created with correct content
	content, err := os.ReadFile(nestedPath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("File content mismatch: got %q, want %q", string(content), "test content")
	}
}

func TestReadFileBytes(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.bin")

	// Write binary data
	testData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	got, err := ReadFileBytes(testFile)
	if err != nil {
		t.Fatalf("ReadFileBytes() failed: %v", err)
	}

	if len(got) != len(testData) {
		t.Errorf("ReadFileBytes() length mismatch: got %d, want %d", len(got), len(testData))
	}

	for i := range testData {
		if got[i] != testData[i] {
			t.Errorf("ReadFileBytes() byte mismatch at index %d: got 0x%02x, want 0x%02x", i, got[i], testData[i])
		}
	}
}

func TestSafeResolvePathTmpExemption(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	tempDir := filepath.Join(homeDir, ".sprout-test-tmp-exemption")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	// Create a file in /tmp to test
	tmpFile := "/tmp/testfile_existing.txt"
	defer os.Remove(tmpFile)
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Skipf("skipping: cannot write to /tmp on this platform: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "/tmp existing file",
			path:    tmpFile,
			wantErr: false,
		},
		{
			name:    "/tmp directory itself",
			path:    "/tmp",
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

func TestSafeResolvePathForWriteTmpExemption(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	tempDir := filepath.Join(homeDir, ".sprout-test-write-tmp-exemption")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "/tmp new file",
			path:    "/tmp/newfile.txt",
			wantErr: false,
		},
		{
			name:    "/tmp nested new file",
			path:    "/tmp/subdir/newfile.txt",
			wantErr: false,
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

func TestSaveFile(t *testing.T) {
	tempDir := t.TempDir()

	// Test creating a new file
	newFile := filepath.Join(tempDir, "newfile.txt")
	err := SaveFile(newFile, "Hello, World!")
	if err != nil {
		t.Fatalf("SaveFile() create failed: %v", err)
	}

	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if string(content) != "Hello, World!" {
		t.Errorf("SaveFile() content mismatch: got %q, want %q", string(content), "Hello, World!")
	}

	// Test updating an existing file
	err = SaveFile(newFile, "Updated content")
	if err != nil {
		t.Fatalf("SaveFile() update failed: %v", err)
	}

	content, err = os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}
	if string(content) != "Updated content" {
		t.Errorf("SaveFile() update content mismatch: got %q, want %q", string(content), "Updated content")
	}

	// Test removing a file (empty content)
	existingFile := filepath.Join(tempDir, "to_remove.txt")
	if err := os.WriteFile(existingFile, []byte("remove me"), 0644); err != nil {
		t.Fatalf("Failed to create file for removal: %v", err)
	}

	err = SaveFile(existingFile, "")
	if err != nil {
		t.Fatalf("SaveFile() remove failed: %v", err)
	}

	if FileExists(existingFile) {
		t.Errorf("SaveFile() should have removed file but it still exists")
	}

	// Test removing non-existent file (should not error)
	err = SaveFile(filepath.Join(tempDir, "nonexistent.txt"), "")
	if err != nil {
		t.Errorf("SaveFile() on non-existent file should not error, got: %v", err)
	}
}

// TestIsHomeDir verifies the home-directory guard used by the embedding and
// symbol indexers. Both paths are resolved through symlinks so macOS paths
// like /var/folders/.../T/foo compare equal to /Users/foo.
func TestIsHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}

	t.Run("home directory itself", func(t *testing.T) {
		if !IsHomeDir(home) {
			t.Errorf("IsHomeDir(%q) = false, want true", home)
		}
	})

	t.Run("non-home temp directory", func(t *testing.T) {
		dir := t.TempDir()
		if IsHomeDir(dir) {
			t.Errorf("IsHomeDir(%q) = true, want false", dir)
		}
	})

	t.Run("symlink to non-home directory", func(t *testing.T) {
		target := t.TempDir()
		link := filepath.Join(os.TempDir(), "sprout-ishomedir-symlink-test")
		os.Remove(link) // clean up any leftover from a previous run
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink not supported on this filesystem: %v", err)
		}
		defer os.Remove(link)
		if IsHomeDir(link) {
			t.Errorf("IsHomeDir(symlink-to-tempdir) = true, want false")
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if IsHomeDir("") {
			t.Errorf("IsHomeDir(\"\") = true, want false")
		}
	})
}

// =============================================================================
// Tests for SP-127 Phase 2.4 (Resolver Scope) and Phase 2.5 (Symlink Re-validation)
// =============================================================================

func TestSafeResolvePath_ResolverScope(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	// Create workspace root directory (not /tmp)
	workspaceRoot := filepath.Join(homeDir, ".sprout-test-resolver-workspace")
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatalf("Failed to create workspace root: %v", err)
	}
	defer os.RemoveAll(workspaceRoot)

	// Create effective cwd directory (different from workspace root)
	effectiveCwd := filepath.Join(homeDir, ".sprout-test-resolver-cwd")
	if err := os.MkdirAll(effectiveCwd, 0755); err != nil {
		t.Fatalf("Failed to create effective cwd: %v", err)
	}
	defer os.RemoveAll(effectiveCwd)

	// Create session-allowed folder (different from both)
	sessionFolder := filepath.Join(homeDir, ".sprout-test-resolver-session")
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		t.Fatalf("Failed to create session folder: %v", err)
	}
	defer os.RemoveAll(sessionFolder)

	// Create a file under workspace root
	workspaceFile := filepath.Join(workspaceRoot, "file.txt")
	if err := os.WriteFile(workspaceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create workspace file: %v", err)
	}

	// Create a file under effective cwd
	cwdFile := filepath.Join(effectiveCwd, "file.txt")
	if err := os.WriteFile(cwdFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create cwd file: %v", err)
	}

	// Create a file under session folder
	sessionFile := filepath.Join(sessionFolder, "file.txt")
	if err := os.WriteFile(sessionFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}

	// Create a file outside all allowed directories
	outsideFile := filepath.Join(homeDir, ".sprout-test-outside", "file.txt")
	outsideDir := filepath.Join(homeDir, ".sprout-test-outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)
	if err := os.WriteFile(outsideFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	tests := []struct {
		name          string
		path          string
		ctx           context.Context
		wantErr       bool
		wantErrSubstr string // substring that should appear in error
	}{
		{
			name:    "path under workspaceRoot is allowed",
			path:    workspaceFile,
			ctx:     WithWorkspaceRoot(context.Background(), workspaceRoot),
			wantErr: false,
		},
		{
			name:    "path under effectiveCwd but NOT workspaceRoot is allowed",
			path:    cwdFile,
			ctx:     WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, nil),
			wantErr: false,
		},
		{
			name:    "path under session-allowed folder is allowed",
			path:    sessionFile,
			ctx:     WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, []string{sessionFolder}),
			wantErr: false,
		},
		{
			name:          "path outside all allowlists is rejected",
			path:          outsideFile,
			ctx:           WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, []string{sessionFolder}),
			wantErr:       true,
			wantErrSubstr: "outside working directory",
		},
		{
			name:    "empty agent context still allows workspaceRoot paths",
			path:    workspaceFile,
			ctx:     WithWorkspaceRoot(context.Background(), workspaceRoot),
			wantErr: false,
		},
		{
			name:    "nil context uses process cwd (should succeed since we're in workspace)",
			path:    workspaceFile,
			ctx:     context.Background(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Change to workspace root so relative paths work
			if err := os.Chdir(workspaceRoot); err != nil {
				t.Fatalf("Failed to chdir to workspace: %v", err)
			}

			got, err := SafeResolvePathWithBypass(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathWithBypass() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantErrSubstr != "" {
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("SafeResolvePathWithBypass() error = %v, want error containing %q", err, tt.wantErrSubstr)
				}
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePathWithBypass() returned empty path, expected valid path")
			}
		})
	}
}

func TestSafeResolvePath_SymlinkInWorkspace(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	// Create workspace root
	workspaceRoot := filepath.Join(homeDir, ".sprout-test-symlink-workspace")
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatalf("Failed to create workspace root: %v", err)
	}
	defer os.RemoveAll(workspaceRoot)

	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a target file inside workspace
	targetFile := filepath.Join(workspaceRoot, "target.txt")
	if err := os.WriteFile(targetFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create a symlink in workspace pointing to target inside workspace
	symlinkInWorkspace := filepath.Join(workspaceRoot, "link.txt")
	if err := os.Symlink(targetFile, symlinkInWorkspace); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(symlinkInWorkspace)

	// Create a symlink in workspace pointing to outside
	outsideDir := filepath.Join(homeDir, ".sprout-test-symlink-outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	symlinkOutside := filepath.Join(workspaceRoot, "badlink.txt")
	if err := os.Symlink(outsideFile, symlinkOutside); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(symlinkOutside)

	ctx := WithWorkspaceRoot(context.Background(), workspaceRoot)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "symlink to file inside workspace is allowed",
			path:    symlinkInWorkspace,
			wantErr: false,
		},
		{
			name:    "symlink to file outside workspace is rejected",
			path:    symlinkOutside,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeResolvePathWithBypass(ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathWithBypass() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSafeResolvePathForWrite_ResolverScope(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	// Create directories
	workspaceRoot := filepath.Join(homeDir, ".sprout-test-write-scope-ws")
	effectiveCwd := filepath.Join(homeDir, ".sprout-test-write-scope-cwd")
	sessionFolder := filepath.Join(homeDir, ".sprout-test-write-scope-sess")
	outsideDir := filepath.Join(homeDir, ".sprout-test-write-scope-outside")

	for _, dir := range []string{workspaceRoot, effectiveCwd, sessionFolder, outsideDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
	}
	defer func() {
		for _, dir := range []string{workspaceRoot, effectiveCwd, sessionFolder, outsideDir} {
			os.RemoveAll(dir)
		}
	}()

	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	tests := []struct {
		name          string
		path          string
		ctx           context.Context
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "write under workspaceRoot",
			path:    filepath.Join(workspaceRoot, "new.txt"),
			ctx:     WithWorkspaceRoot(context.Background(), workspaceRoot),
			wantErr: false,
		},
		{
			name:    "write under effectiveCwd",
			path:    filepath.Join(effectiveCwd, "new.txt"),
			ctx:     WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, nil),
			wantErr: false,
		},
		{
			name:    "write under session folder",
			path:    filepath.Join(sessionFolder, "new.txt"),
			ctx:     WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, []string{sessionFolder}),
			wantErr: false,
		},
		{
			name:          "write outside all allowlists",
			path:          filepath.Join(outsideDir, "new.txt"),
			ctx:           WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), effectiveCwd, []string{sessionFolder}),
			wantErr:       true,
			wantErrSubstr: "outside working directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeResolvePathForWriteWithBypass(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantErrSubstr != "" {
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, want error containing %q", err, tt.wantErrSubstr)
				}
			}
		})
	}
}

func TestSafeResolvePathForWrite_SymlinkReValidation(t *testing.T) {
	// This tests Phase 2.5: symlink re-validation for existing files at write time.
	// If a file is a symlink pointing outside the allowlist, writes should be rejected.
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	workspaceRoot := filepath.Join(homeDir, ".sprout-test-write-symlink-ws")
	sessionFolder := filepath.Join(homeDir, ".sprout-test-write-symlink-sess")

	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		t.Fatalf("Failed to create session folder: %v", err)
	}
	defer func() {
		os.RemoveAll(workspaceRoot)
		os.RemoveAll(sessionFolder)
	}()

	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a target file in /tmp (allowed for writes)
	tmpFile := "/tmp/sprout-test-write-symlink-tmp.txt"
	if err := os.WriteFile(tmpFile, []byte("tmp"), 0644); err != nil {
		t.Skipf("cannot write to /tmp: %v", err)
	}
	defer os.Remove(tmpFile)

	// Create a symlink in workspace pointing to /tmp
	linkToTmp := filepath.Join(workspaceRoot, "link-to-tmp.txt")
	if err := os.Symlink(tmpFile, linkToTmp); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(linkToTmp)

	// Create a target file in session folder
	sessionFile := filepath.Join(sessionFolder, "allowed.txt")
	if err := os.WriteFile(sessionFile, []byte("allowed"), 0644); err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}

	// Create a symlink in workspace pointing to session folder file
	linkToSession := filepath.Join(workspaceRoot, "link-to-session.txt")
	if err := os.Symlink(sessionFile, linkToSession); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(linkToSession)

	// Create a target file outside
	outsideFile := filepath.Join(homeDir, ".sprout-test-write-symlink-outside", "secret.txt")
	outsideDir := filepath.Join(homeDir, ".sprout-test-write-symlink-outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	// Create a symlink in workspace pointing to outside
	linkToOutside := filepath.Join(workspaceRoot, "link-to-outside.txt")
	if err := os.Symlink(outsideFile, linkToOutside); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(linkToOutside)

	ctx := WithAgentContext(WithWorkspaceRoot(context.Background(), workspaceRoot), workspaceRoot, []string{sessionFolder})

	tests := []struct {
		name          string
		path          string
		ctx           context.Context
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "symlink in workspace pointing to /tmp is allowed (tmp special case)",
			path:    linkToTmp,
			ctx:     ctx,
			wantErr: false,
		},
		{
			name:    "symlink in workspace pointing to session folder is allowed",
			path:    linkToSession,
			ctx:     ctx,
			wantErr: false,
		},
		{
			name:          "symlink in workspace pointing to outside is rejected",
			path:          linkToOutside,
			ctx:           ctx,
			wantErr:       true,
			wantErrSubstr: "symlink target is outside allowed paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeResolvePathForWriteWithBypass(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.wantErrSubstr != "" {
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, want error containing %q", err, tt.wantErrSubstr)
				}
			}
		})
	}
}

func TestSafeResolvePathForWrite_SymlinkToEffectiveCwdFolder(t *testing.T) {
	// Test that a symlink under workspace pointing to a file in effectiveCwd-allowed folder is allowed
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	workspaceRoot := filepath.Join(homeDir, ".sprout-test-write-symlink2-ws")
	effectiveCwd := filepath.Join(homeDir, ".sprout-test-write-symlink2-cwd")

	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}
	if err := os.MkdirAll(effectiveCwd, 0755); err != nil {
		t.Fatalf("Failed to create effective cwd: %v", err)
	}
	defer func() {
		os.RemoveAll(workspaceRoot)
		os.RemoveAll(effectiveCwd)
	}()

	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a target file in effectiveCwd
	targetFile := filepath.Join(effectiveCwd, "target.txt")
	if err := os.WriteFile(targetFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create a symlink in workspace pointing to target in effectiveCwd
	link := filepath.Join(workspaceRoot, "link.txt")
	if err := os.Symlink(targetFile, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(link)

	ctx := WithAgentContext(
		WithWorkspaceRoot(context.Background(), workspaceRoot),
		effectiveCwd,
		nil,
	)

	// This should succeed because the symlink target is under effectiveCwd
	_, err = SafeResolvePathForWriteWithBypass(ctx, link)
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
}

func TestSafeResolvePath_MultipleSymlinkHops(t *testing.T) {
	// Test that multiple symlink hops are properly validated
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}

	workspaceRoot := filepath.Join(homeDir, ".sprout-test-multihop-ws")
	outsideDir := filepath.Join(homeDir, ".sprout-test-multihop-outside")

	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}
	defer func() {
		os.RemoveAll(workspaceRoot)
		os.RemoveAll(outsideDir)
	}()

	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create a chain: link1 -> link2 -> outsideFile
	link1 := filepath.Join(workspaceRoot, "link1.txt")
	link2 := filepath.Join(workspaceRoot, "link2.txt")
	outsideFile := filepath.Join(outsideDir, "secret.txt")

	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}

	// Create link2 first (pointing to outside)
	if err := os.Symlink(outsideFile, link2); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(link2)

	// Create link1 pointing to link2
	if err := os.Symlink(link2, link1); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	defer os.Remove(link1)

	ctx := WithWorkspaceRoot(context.Background(), workspaceRoot)

	// link1 resolves to outside, should be rejected
	_, err = SafeResolvePathWithBypass(ctx, link1)
	if err == nil {
		t.Errorf("Expected rejection for multi-hop symlink to outside, got success")
	}
}

func TestAgentContext_Getters(t *testing.T) {
	// Test that the context getters work correctly
	t.Run("WithEffectiveCwd and AgentEffectiveCwdFromContext", func(t *testing.T) {
		ctx := context.Background()
		if got := AgentEffectiveCwdFromContext(ctx); got != "" {
			t.Errorf("Empty context should return empty string, got %q", got)
		}

		ctx = WithEffectiveCwd(context.Background(), "/some/path")
		if got := AgentEffectiveCwdFromContext(ctx); got != "/some/path" {
			t.Errorf("Expected /some/path, got %q", got)
		}

		// Empty string should not set a value
		ctx = WithEffectiveCwd(context.Background(), "")
		if got := AgentEffectiveCwdFromContext(ctx); got != "" {
			t.Errorf("Empty string should not set value, got %q", got)
		}
	})

	t.Run("WithSessionAllowedFolders and SessionAllowedFoldersFromContext", func(t *testing.T) {
		ctx := context.Background()
		if got := SessionAllowedFoldersFromContext(ctx); got != nil {
			t.Errorf("Empty context should return nil, got %v", got)
		}

		folders := []string{"/folder1", "/folder2"}
		ctx = WithSessionAllowedFolders(context.Background(), folders)
		got := SessionAllowedFoldersFromContext(ctx)
		if len(got) != 2 {
			t.Errorf("Expected 2 folders, got %d", len(got))
		}
		if got[0] != "/folder1" || got[1] != "/folder2" {
			t.Errorf("Unexpected folders: %v", got)
		}

		// Verify it's a copy, not the original
		folders[0] = "/modified"
		got2 := SessionAllowedFoldersFromContext(ctx)
		if got2[0] == "/modified" {
			t.Errorf("SessionAllowedFoldersFromContext should return a copy")
		}

		// Empty slice should not set a value
		ctx = WithSessionAllowedFolders(context.Background(), []string{})
		if got := SessionAllowedFoldersFromContext(ctx); got != nil {
			t.Errorf("Empty slice should not set value, got %v", got)
		}
	})

	t.Run("WithAgentContext convenience helper", func(t *testing.T) {
		folders := []string{"/allowed"}
		ctx := WithAgentContext(context.Background(), "/cwd", folders)

		if got := AgentEffectiveCwdFromContext(ctx); got != "/cwd" {
			t.Errorf("Expected /cwd, got %q", got)
		}

		got := SessionAllowedFoldersFromContext(ctx)
		if len(got) != 1 || got[0] != "/allowed" {
			t.Errorf("Unexpected folders: %v", got)
		}

		// Nil context should work
		ctx = WithAgentContext(nil, "/cwd", folders)
		if got := AgentEffectiveCwdFromContext(ctx); got != "/cwd" {
			t.Errorf("Expected /cwd from nil context, got %q", got)
		}
	})

	t.Run("nil context handling", func(t *testing.T) {
		// All functions should handle nil context gracefully
		if got := WorkspaceRootFromContext(nil); got != "" {
			t.Errorf("WorkspaceRootFromContext(nil) should return empty string")
		}
		if got := AgentEffectiveCwdFromContext(nil); got != "" {
			t.Errorf("AgentEffectiveCwdFromContext(nil) should return empty string")
		}
		if got := SessionAllowedFoldersFromContext(nil); got != nil {
			t.Errorf("SessionAllowedFoldersFromContext(nil) should return nil")
		}
		if SecurityBypassEnabled(nil) {
			t.Errorf("SecurityBypassEnabled(nil) should return false")
		}
	})
}
