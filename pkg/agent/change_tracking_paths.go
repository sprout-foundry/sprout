package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveAbsPath resolves filePath to a cleaned absolute path, using
// the agent's workspace root as the base for relative paths when
// available, else the process CWD. This normalization is applied at
// track time (H3) so stored FilePaths are independent of the process's
// CWD — a later `cd` in a shell command can't make recovery or dedup
// resolve a relative path to the wrong location.
//
// Absolute paths are returned cleaned but otherwise unchanged. If the
// resolution fails (e.g., os.Getwd error), the raw input is returned
// as a fallback so tracking doesn't silently drop the change.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) resolveAbsPath(filePath string) string {
	if filepath.IsAbs(filePath) {
		return filepath.Clean(filePath)
	}
	root := ""
	if ct.agent != nil {
		root = ct.agent.workspaceRoot
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return filePath
		}
	}
	abs, err := filepath.Abs(filepath.Join(root, filePath))
	if err != nil {
		return filePath
	}
	return abs
}

// isOutsideWorkspace returns true if filePath is outside the agent's workspace root.
// If the workspace root is empty or the agent is nil, it returns false (treats all files as in-workspace).
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) isOutsideWorkspace(filePath string) bool {
	if ct.agent == nil {
		return false
	}
	workspaceRoot := ct.agent.workspaceRoot
	if workspaceRoot == "" {
		return false
	}

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return false // If we can't resolve the path, don't redact
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false // If we can't resolve workspace, don't redact
	}

	// Resolve symlinks on both sides for consistent comparison.
	// On macOS, /var → /private/var and os.Chdir may resolve the symlink
	// in the process's CWD, causing absFile and absWorkspace to diverge.
	absFile = resolveSymlinksPath(absFile)
	resolvedWorkspace, werr := filepath.EvalSymlinks(absWorkspace)
	if werr == nil {
		absWorkspace = resolvedWorkspace
	}

	rel, err := filepath.Rel(absWorkspace, absFile)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's outside the workspace
	return strings.HasPrefix(rel, "..")
}

// resolveSymlinksPath resolves symlinks in a path, handling non-existent
// files/directories by walking up to the nearest existing ancestor and
// appending the remaining components.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func resolveSymlinksPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	// Walk up the directory tree until we find an existing ancestor.
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	for {
		resolvedDir, derr := filepath.EvalSymlinks(dir)
		if derr == nil {
			return filepath.Join(resolvedDir, base)
		}
		base = filepath.Join(filepath.Base(dir), base)
		dir = filepath.Dir(dir)
		if dir == "/" || dir == "." {
			// Reached the root without resolving; return original.
			return path
		}
	}
}
