package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// FileExists checks if a file exists at the given path
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// FilesExist checks if all the given files exist
func FilesExist(filenames ...string) (bool, error) {
	for _, filename := range filenames {
		if !FileExists(filename) {
			return false, nil
		}
	}
	return true, nil
}

// SaveFile saves or removes a file with the given content.
// If content is empty, the file is removed.
func SaveFile(filename, content string) error {
	if content == "" {
		if _, err := os.Stat(filename); err == nil {
			// File exists, remove it
			fmt.Printf("🗑️  Removing file: %s\n", filename)
			return os.Remove(filename)
		} else if os.IsNotExist(err) {
			// File does not exist, nothing to do
			return nil
		} else {
			// Other error checking file stat
			return fmt.Errorf("error checking file %s: %w", filename, err)
		}
	}

	// Notify user about file being written
	fmt.Printf("💾 Writing file: %s (%d bytes)\n", filename, len(content))

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	// Normalize EOLs to existing file style if present and preserve BOM/UTF-16 when detected
	in := []byte(content)
	normalized := in
	if b, err := os.ReadFile(filename); err == nil {
		// EOL style
		if bytes.Contains(b, []byte("\r\n")) {
			normalized = bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
		}
		// BOM/UTF-16 detection (simple heuristics)
		if len(b) >= 2 {
			// UTF-16 LE BOM FF FE
			if b[0] == 0xFF && b[1] == 0xFE {
				// encode as UTF-16 LE with BOM
				u16 := utf16.Encode([]rune(string(content)))
				// write BOM
				buf := []byte{0xFF, 0xFE}
				for _, w := range u16 {
					buf = append(buf, byte(w), byte(w>>8))
				}
				normalized = buf
			}
			// UTF-16 BE BOM FE FF
			if b[0] == 0xFE && b[1] == 0xFF {
				u16 := utf16.Encode([]rune(string(content)))
				buf := []byte{0xFE, 0xFF}
				for _, w := range u16 {
					buf = append(buf, byte(w>>8), byte(w))
				}
				normalized = buf
			}
			// UTF-8 BOM EF BB BF
			if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
				// Ensure output starts with UTF-8 BOM
				if !bytes.HasPrefix(normalized, []byte{0xEF, 0xBB, 0xBF}) {
					normalized = append([]byte{0xEF, 0xBB, 0xBF}, normalized...)
				}
			}
		}
	}
	// Ensure UTF-8 validity for normal writes
	if len(normalized) > 0 && !utf8.Valid(normalized) {
		// As a safety, write raw; caller chose encoding (likely UTF-16 block above)
	}
	err := os.WriteFile(filename, normalized, 0644)
	if err != nil {
		fmt.Printf("   ❌ Failed to write file: %v\n", err)
	} else {
		fmt.Print("   ✅ File written successfully\n")
	}
	return err
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	fmt.Printf("📖 Reading file: %s\n", filename)

	// Use buffered reader for potential large files; still load whole file for simplicity
	f, err := os.Open(filename)
	if err != nil {
		fmt.Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	defer f.Close()
	buf := new(bytes.Buffer)
	if _, err := bufio.NewReader(f).WriteTo(buf); err != nil {
		fmt.Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	content := buf.Bytes()
	if err != nil {
		fmt.Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}

	fmt.Printf("   ✅ File read successfully (%d bytes)\n", len(content))
	return string(content), nil
}

// EnsureDir creates directory if it doesn't exist
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// WriteFileWithDir creates the directory and writes the file
func WriteFileWithDir(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return os.WriteFile(path, data, perm)
}

// ReadFileBytes reads file as bytes
func ReadFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// CreateTempFile creates a temporary file
func CreateTempFile(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}

// SafeResolvePath validates and resolves a file path, checking for path traversal
// while allowing symlinks that stay within the working directory.
//
// Returns the resolved absolute path if it's safe to access, or an error otherwise.
func SafeResolvePath(filePath string) (string, error) {
	return SafeResolvePathWithBypass(context.Background(), filePath)
}

// SafeResolvePathWithBypass validates a file path for reading, checking that it's
// within the working directory and handling symlinks properly. Optional bypass
// can be enabled via context when user has explicitly approved the operation.
func SafeResolvePathWithBypass(ctx context.Context, filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}

	// Clean the path
	cleanPath := filepath.Clean(filePath)

	// Get absolute paths
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for cwd: %w", err)
	}

	// Resolve symlinks to their targets
	resolvedAbs, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path (including symlink evaluation): %w", err)
	}

	// Also resolve CWD in case it's a symlink
	resolvedCwd, err := filepath.EvalSymlinks(cwdAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve cwd symlink: %w", err)
	}

	// Check if the resolved path is within the resolved working directory
	relPath, err := filepath.Rel(resolvedCwd, resolvedAbs)
	if err != nil {
		return "", fmt.Errorf("failed to determine relative path: %w", err)
	}

	// If the relative path starts with "..", it's outside the working directory
	if strings.HasPrefix(relPath, "..") {
		if SecurityBypassEnabled(ctx) {
			// Security bypass enabled - allow access outside working directory
			return resolvedAbs, nil
		}
		return "", fmt.Errorf("security violation: attempt to access file outside working directory: %s (resolves to: %s)", cleanPath, resolvedAbs)
	}

	return resolvedAbs, nil
}

// SafeResolvePathForWrite validates a file path for writing, checking that the
// parent directory is safe to access. This allows writing to new files that don't
// exist yet while still preventing path traversal attacks.
//
// Returns the absolute path if it's safe to write, or an error otherwise.
func SafeResolvePathForWrite(filePath string) (string, error) {
	return SafeResolvePathForWriteWithBypass(context.Background(), filePath)
}

// SafeResolvePathForWriteWithBypass validates a file path for writing with optional bypass.
// This allows writing to new files that don't exist yet while still preventing path
// traversal attacks. When security bypass is enabled via context, writes outside the
// working directory are allowed.
//
// Returns the absolute path if it's safe to write, or an error otherwise.
func SafeResolvePathForWriteWithBypass(ctx context.Context, filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}

	// Clean the path
	cleanPath := filepath.Clean(filePath)

	// Get absolute paths
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for cwd: %w", err)
	}

	// Get the parent directory and resolve it (file may not exist yet)
	parentDir := filepath.Dir(absPath)

	// Find the nearest existing parent directory
	maxDepth := 50 // Prevent infinite loops
	depth := 0
	for depth < maxDepth {
		if _, statErr := os.Stat(parentDir); statErr == nil {
			// Found an existing directory
			break
		}

		// Parent doesn't exist, try going up one level
		newParent := filepath.Dir(parentDir)
		if newParent == parentDir {
			// We've reached the root without finding a valid directory
			return "", fmt.Errorf("no safe parent directory found for path: %s", cleanPath)
		}
		parentDir = newParent
		depth++
	}

	if depth >= maxDepth {
		return "", fmt.Errorf("parent directory search exceeded maximum depth for path: %s", cleanPath)
	}

	// Resolve symlinks in the parent directory path
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve parent directory symlink: %w", err)
	}

	// Also resolve CWD in case it's a symlink
	resolvedCwd, err := filepath.EvalSymlinks(cwdAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve cwd symlink: %w", err)
	}

	// Check if the resolved parent directory is within the resolved working directory
	relPath, err := filepath.Rel(resolvedCwd, resolvedParent)
	if err != nil {
		return "", fmt.Errorf("failed to determine relative path: %w", err)
	}

	// If the relative path starts with "..", it's outside the working directory
	if strings.HasPrefix(relPath, "..") {
		if SecurityBypassEnabled(ctx) {
			// Security bypass enabled - allow writing outside working directory
			return absPath, nil
		}
		return "", fmt.Errorf("security violation: attempt to write file outside working directory: %s (parent resolves to: %s)", cleanPath, resolvedParent)
	}

	return absPath, nil
}
