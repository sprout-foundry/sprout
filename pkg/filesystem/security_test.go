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
	tempDir := filepath.Join(homeDir, ".ledit-test-tmp-exemption")
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
		t.Fatalf("Failed to create temp file in /tmp: %v", err)
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
	tempDir := filepath.Join(homeDir, ".ledit-test-write-tmp-exemption")
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
