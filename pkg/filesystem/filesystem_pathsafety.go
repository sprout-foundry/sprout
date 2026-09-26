package filesystem

// filesystem_pathsafety.go — the path-safety resolution layer: the
// SafeResolve* / write-safe resolvers, the symlink-timeout + bypass path,
// the FS gate decision logging, the under-context / under-tmp checks, and the
// resolved-temp-dir helpers, split out of filesystem.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SafeResolvePath validates and resolves a file path, checking for path traversal
// while allowing symlinks that stay within the working directory.
//
// Returns the resolved absolute path if it's safe to access, or an error otherwise.
func SafeResolvePath(filePath string) (string, error) {
	return SafeResolvePathWithBypass(context.Background(), filePath)
}

// SafeResolveAbs resolves filePath to an absolute path against the
// workspace root carried on ctx (falling back to the process CWD),
// WITHOUT symlink evaluation. Use for classification of paths that may
// not exist yet — EvalSymlinks fails on absent targets, but containment
// checks against absolute workspace/allowlist roots still need an
// absolute candidate. Returns ("", err) on failure; callers fall back
// to the raw input.
func SafeResolveAbs(ctx context.Context, filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}
	workspaceRoot := WorkspaceRootFromContext(ctx)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		workspaceRoot = cwd
	}
	base, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for workspace root: %w", err)
	}
	abs := filepath.Clean(filePath)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(base, abs)
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}
	return abs, nil
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
	// Marshal the entry to JSON and write via LogJSON to avoid type-identity
	// issues. The concrete implementation (*tools.AuditLogger) expects
	// tools.AuditEntry in LogEntry, but filesystem defines its own
	// filesystem.AuditEntry — they have identical JSON structure but different
	// Go types, causing the type assertion to fail.
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = logger.LogJSON(data)
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

	// The caller often hands us a *symlink-resolved* path (SafeResolvePath*
	// runs EvalSymlinks). os.TempDir() may not be in resolved form: on macOS
	// it is /var/folders/.../T, but everything under /var resolves to
	// /private/var/..., so a resolved temp path fails the prefix check above
	// and the fetch_url temp-file read-back hit "outside working directory"
	// errors. Compare against the resolved form as well.
	if resolvedTemp, ok := resolvedTempDir(); ok &&
		isUnderPrefix(cleanPath, resolvedTemp) {
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

// resolvedTempDirOnce caches the symlink-resolved os.TempDir() (e.g.
// /private/var/folders/.../T on macOS). Evaluated lazily and once because
// os.TempDir() is process-static and EvalSymlinks touches the filesystem.
var (
	resolvedTempDirOnce sync.Once
	resolvedTempDirVal  string
	resolvedTempDirOK   bool
)

// resolvedTempDir returns the symlink-resolved form of os.TempDir().
// The second return value is false when resolution fails (e.g. temp dir
// unavailable) or when the resolved form is empty.
func resolvedTempDir() (string, bool) {
	resolvedTempDirOnce.Do(func() {
		resolved, err := filepath.EvalSymlinks(os.TempDir())
		if err == nil {
			resolvedTempDirVal = filepath.Clean(resolved)
			resolvedTempDirOK = resolvedTempDirVal != ""
		}
	})
	return resolvedTempDirVal, resolvedTempDirOK
}

// IsUnderTmpPath is the exported wrapper around isInTmpPath.
// It reports whether path is within the OS temp directory.
// SP-127 M1: used by the Gate 1 path-tier classifier to allow /tmp unconditionally.
func IsUnderTmpPath(path string) bool {
	return isInTmpPath(path)
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
