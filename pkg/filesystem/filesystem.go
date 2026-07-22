package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// AuditEntry represents a single filesystem gate audit log entry.
// This is a local definition to avoid import cycles - the fields must match
// pkg/agent_tools.AuditEntry for compatibility.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool"`
	Args      string    `json:"args,omitempty"`
	RiskLevel string    `json:"risk_level"`
	Category  string    `json:"category"`
	Action    string    `json:"action"`
	Reasoning string    `json:"reasoning,omitempty"`
	Source    string    `json:"source,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
}

// ErrOutsideWorkingDirectory is returned when a path is outside the working directory
// This should be caught by tool handlers to prompt the user for confirmation
var ErrOutsideWorkingDirectory = errors.New("file access outside working directory")

// ErrWriteOutsideWorkingDirectory is returned when a write path is outside the working directory
var ErrWriteOutsideWorkingDirectory = errors.New("file write outside working directory")

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
			fmt.Printf("\n[x] Removing file: %s\n", filename)
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
	fmt.Printf("\n[save] Writing file: %s (%d bytes)\n", filename, len(content))

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
		console.GlyphError.Fprintf(os.Stdout, "Failed to write file: %v", err)
		return fmt.Errorf("write file %s: %w", filename, err)
	}
	console.GlyphSuccess.Fprintln(os.Stdout, "File written successfully")
	return nil
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	fmt.Printf("[read] Reading file: %s\n", filename)

	// Use buffered reader for potential large files; still load whole file for simplicity
	f, err := os.Open(filename)
	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Failed to read file: %v", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	defer f.Close()
	buf := new(bytes.Buffer)
	if _, err := bufio.NewReader(f).WriteTo(buf); err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Failed to read file: %v", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	content := buf.Bytes()
	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Failed to read file: %v", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}

	console.GlyphSuccess.Fprintf(os.Stdout, "File read successfully (%d bytes)", len(content))
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

// symlinkTimeout is the maximum time allowed for symlink resolution.
// Network filesystems (NFS, cloud mounts) can hang indefinitely on EvalSymlinks
// if the server is unreachable. This prevents file operations from blocking forever.
const symlinkTimeout = 3 * time.Second

// evalSymlinksWithTimeout wraps filepath.EvalSymlinks with a timeout guard.
// Returns ctx.Err() if the timeout fires before resolution completes.
func evalSymlinksWithTimeout(ctx context.Context, path string) (string, error) {
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resolved, err := filepath.EvalSymlinks(path)
		done <- result{resolved, err}
	}()
	select {
	case res := <-done:
		return res.path, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(symlinkTimeout):
		return "", fmt.Errorf("symlink resolution timed out after %v for: %s", symlinkTimeout, path)
	}
}

// SafeResolvePathWithBypass validates a file path for reading, checking that it's
// within the working directory and handling symlinks properly. Optional bypass
// can be enabled via context when user has explicitly approved the operation.
func SafeResolvePathWithBypass(ctx context.Context, filePath string) (string, error) {
	start := time.Now()
	defer func() {
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			// Log slow path resolution — usually indicates a network filesystem issue
			log.Printf("WARN: SafeResolvePathWithBypass took %v for %s", elapsed, filePath)
		}
	}()

	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}

	// Clean the path
	cleanPath := filepath.Clean(filePath)

	workspaceRoot := WorkspaceRootFromContext(ctx)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		workspaceRoot = cwd
	}

	cwdAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for workspace root: %w", err)
	}

	// Resolve relative paths against the explicit workspace root instead of the
	// process-global cwd.
	absPath := cleanPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwdAbs, cleanPath)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Resolve symlinks with timeout guard to prevent hangs on unresponsive network mounts
	resolvedAbs, err := evalSymlinksWithTimeout(ctx, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path (including symlink evaluation): %w", err)
	}

	// Also resolve CWD in case it's a symlink
	resolvedCwd, err := evalSymlinksWithTimeout(ctx, cwdAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve cwd symlink: %w", err)
	}

	// Allow all /tmp/* operations without security checks (SP-127 Phase 2.6: no audit for /tmp - not a gate decision)
	if isInTmpPath(resolvedAbs) {
		return resolvedAbs, nil
	}

	// Check if the resolved path is within the resolved working directory
	relPath, err := filepath.Rel(resolvedCwd, resolvedAbs)
	if err != nil {
		return "", fmt.Errorf("failed to determine relative path: %w", err)
	}

	// If the relative path starts with "..", it's outside the working directory
	if strings.HasPrefix(relPath, "..") {
		// Check if path is under effective cwd or session-allowlisted folders
		if isUnderAgentContext(ctx, resolvedAbs) {
			// Allowed via effective cwd or session folders (SP-127 Phase 2.6: audit)
			logFsGateDecision(ctx, "filesystem_read", cleanPath, "allowed", "low", "path is under effective cwd or session allowlist")
			return resolvedAbs, nil
		}
		if SecurityBypassEnabled(ctx) {
			// Security bypass enabled - allow access outside working directory (SP-127 Phase 2.6: audit)
			logFsGateDecision(ctx, "filesystem_read", cleanPath, "allowed", "low", "security bypass is enabled")
			return resolvedAbs, nil
		}
		// Return custom error that can be caught for user confirmation (SP-127 Phase 2.6: audit denied)
		logFsGateDecision(ctx, "filesystem_read", cleanPath, "denied", "high", "path outside workspace root and not in session allowlist")
		return "", fmt.Errorf("%w: attempt to access file outside working directory: %s (resolves to: %s)", ErrOutsideWorkingDirectory, cleanPath, resolvedAbs)
	}

	// Allowed: path is within workspace (SP-127 Phase 2.6: audit)
	logFsGateDecision(ctx, "filesystem_read", cleanPath, "allowed", "low", "path is within workspace")
	return resolvedAbs, nil
}

// logFsGateDecision emits an audit entry for a filesystem gate decision.
// Nil-safe: skips silently when no logger is configured.
func logFsGateDecision(ctx context.Context, tool, path, action, riskLevel, reasoning string) {
	logger := AuditLoggerFromContext(ctx)
	if logger == nil {
		return
	}
	entry := AuditEntry{
		Timestamp: time.Now(),
		Tool:      tool,
		Args:      path,
		RiskLevel: riskLevel,
		Category:  "fs_gate",
		Action:    action,
		Reasoning: reasoning,
		Source:    "unified-gate",
	}
	// Log through the interface. The concrete implementation (e.g., *tools.AuditLogger)
	// will handle JSON marshaling. Passing filesystem.AuditEntry works because:
	// 1. tools.AuditLogger.LogEntry has a value receiver taking tools.AuditEntry
	// 2. The interface method signature is effectively LogEntry(entry any)
	// 3. json.Marshal serializes based on struct tags, not type identity
	_ = logger.LogEntry(entry)
}

// isUnderAgentContext checks if the resolved path is under the agent's effective cwd
// or any session-allowlisted folder. It resolves all candidate roots through symlinks
// to prevent symlink-escape attacks.
func isUnderAgentContext(ctx context.Context, resolvedPath string) bool {
	// Get effective cwd from context
	effectiveCwd := AgentEffectiveCwdFromContext(ctx)
	if effectiveCwd != "" {
		resolvedCwd, err := evalSymlinksWithTimeout(ctx, effectiveCwd)
		if err == nil {
			if isUnderPrefix(resolvedPath, resolvedCwd) {
				return true
			}
		}
	}

	// Get session-allowlisted folders from context
	sessionFolders := SessionAllowedFoldersFromContext(ctx)
	for _, folder := range sessionFolders {
		resolvedFolder, err := evalSymlinksWithTimeout(ctx, folder)
		if err == nil {
			if isUnderPrefix(resolvedPath, resolvedFolder) {
				return true
			}
		}
	}

	return false
}

// isUnderPrefix reports whether path is equal to prefix or is a proper subdirectory of it.
// Both paths must already be cleaned and, for symlink safety, resolved.
func isUnderPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}

// isInTmpPath checks if a path is within the OS temp directory (os.TempDir()).
// This handles platforms like Termux where the temp dir is not /tmp.
func isInTmpPath(path string) bool {
	cleanPath := filepath.Clean(path)
	tempDir := os.TempDir()
	tempClean := filepath.Clean(tempDir)

	// Check if the path is within the OS temp directory
	// This handles /tmp, /private/tmp (macOS), /data/data/com.termux/files/usr/tmp (Termux), etc.
	if strings.HasPrefix(cleanPath, tempClean+string(filepath.Separator)) || cleanPath == tempClean {
		return true
	}

	// Also check for /tmp and /private/tmp as fallbacks (for cross-platform compatibility
	// even if os.TempDir() returns something else on some platforms).
	if strings.HasPrefix(cleanPath, "/tmp/") || cleanPath == "/tmp" ||
		strings.HasPrefix(cleanPath, "/private/tmp/") || cleanPath == "/private/tmp" {
		return true
	}

	// Also check for Windows-style temp paths
	lowerPath := strings.ToLower(cleanPath)
	if strings.Contains(lowerPath, "\\temp\\") || strings.Contains(lowerPath, "\\tmp\\") {
		return true
	}

	return false
}

// IsHomeDir reports whether path is the current user's home directory.
// Both paths are resolved through symlinks so that, e.g., /var/folders/...
// and /Users/alanp compare correctly on macOS.
//
// Symlink resolution is bounded by symlinkTimeout so a hanging network mount
// (NFS, SMB) cannot stall index builds indefinitely.
func IsHomeDir(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), symlinkTimeout)
	defer cancel()
	resolved, err := evalSymlinksWithTimeout(ctx, filepath.Clean(path))
	if err != nil {
		resolved = filepath.Clean(path)
	}
	homeResolved, err := evalSymlinksWithTimeout(ctx, home)
	if err != nil {
		homeResolved = home
	}
	return resolved == homeResolved
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

	workspaceRoot := WorkspaceRootFromContext(ctx)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		workspaceRoot = cwd
	}

	cwdAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for workspace root: %w", err)
	}

	absPath := cleanPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwdAbs, cleanPath)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Allow all /tmp/* operations without security checks
	if isInTmpPath(absPath) {
		return absPath, nil
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

	// Resolve symlinks in the parent directory path with timeout guard
	resolvedParent, err := evalSymlinksWithTimeout(ctx, parentDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve parent directory symlink: %w", err)
	}

	// Also resolve CWD in case it's a symlink
	resolvedCwd, err := evalSymlinksWithTimeout(ctx, cwdAbs)
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
		// Check if path is under effective cwd or session-allowlisted folders
		if isUnderAgentContext(ctx, absPath) {
			// Path is allowed via effective cwd or session folders (SP-127 Phase 2.6: audit)
			logFsGateDecision(ctx, "filesystem_write", cleanPath, "allowed", "low", "write path is under effective cwd or session allowlist")
		} else if SecurityBypassEnabled(ctx) {
			// Security bypass enabled - allow writing outside working directory (SP-127 Phase 2.6: audit)
			logFsGateDecision(ctx, "filesystem_write", cleanPath, "allowed", "low", "security bypass is enabled for write")
			return absPath, nil
		} else {
			// Return custom error that can be caught for user confirmation (SP-127 Phase 2.6: audit denied)
			logFsGateDecision(ctx, "filesystem_write", cleanPath, "denied", "high", "write path outside workspace root and not in session allowlist")
			return "", fmt.Errorf("%w: attempt to write file outside working directory: %s (parent resolves to: %s)", ErrWriteOutsideWorkingDirectory, cleanPath, resolvedParent)
		}
	}

	// Phase 2.5: Symlink re-validation for existing files
	// If the target file exists, re-resolve it through symlinks and verify
	// the final target is under an allowed root. This catches the case where
	// a benign-looking file in workspace is actually a symlink to /etc/passwd.
	if _, statErr := os.Stat(absPath); statErr == nil {
		// File exists - re-resolve through symlinks to check the final target
		resolvedTarget, err := evalSymlinksWithTimeout(ctx, absPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve symlink target: %w", err)
		}

		// If the resolved target is different from absPath, it's a symlink
		if resolvedTarget != absPath {
			// Check /tmp special case FIRST - /tmp is always allowed for writes
			if isInTmpPath(resolvedTarget) {
				return resolvedTarget, nil
			}

			// Check if the resolved target is under an allowed root
			targetRelPath, err := filepath.Rel(resolvedCwd, resolvedTarget)
			if err != nil {
				return "", fmt.Errorf("failed to determine symlink target relative path: %w", err)
			}

			// Also check against effective cwd and session folders
			if strings.HasPrefix(targetRelPath, "..") && !isUnderAgentContext(ctx, resolvedTarget) {
				// Symlink target is outside allowed paths (SP-127 Phase 2.6: audit denied)
				logFsGateDecision(ctx, "filesystem_write", cleanPath, "denied", "high", "symlink target is outside allowed paths")
				return "", fmt.Errorf("%w: symlink target is outside allowed paths: %s (resolves to: %s)", ErrWriteOutsideWorkingDirectory, cleanPath, resolvedTarget)
			}

			// The resolved target is under an allowed root - this is a symlink redirect (SP-127 Phase 2.6: audit redirected)
			logFsGateDecision(ctx, "filesystem_write", cleanPath, "redirected", "medium", "symlink redirect: "+cleanPath+" resolves to "+resolvedTarget)
			return resolvedTarget, nil
		}
	}

	// Allowed: write path is within workspace (SP-127 Phase 2.6: audit)
	logFsGateDecision(ctx, "filesystem_write", cleanPath, "allowed", "low", "write path is within workspace")
	return absPath, nil
}
