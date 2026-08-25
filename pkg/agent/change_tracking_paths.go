package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveAbsPath resolves filePath to a cleaned absolute path, using
// the agent's workspace root as the base for relative paths.
func (ct *ChangeTracker) resolveAbsPath(filePath string) string {
	if filepath.IsAbs(filePath) {
		return filepath.Clean(filePath)
	}
	root := ""
	if ct.agent != nil {
		root = ct.agent.GetWorkspaceRoot()
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
func (ct *ChangeTracker) isOutsideWorkspace(filePath string) bool {
	if ct.agent == nil {
		return false
	}
	workspaceRoot := ct.agent.GetWorkspaceRoot()
	if workspaceRoot == "" {
		return false
	}

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}

	// Resolve symlinks on both sides for consistent comparison.
	absFile = resolveSymlinksPath(absFile)
	resolvedWorkspace, werr := filepath.EvalSymlinks(absWorkspace)
	if werr == nil {
		absWorkspace = resolvedWorkspace
	}

	rel, err := filepath.Rel(absWorkspace, absFile)
	if err != nil {
		return false
	}

	return strings.HasPrefix(rel, "..")
}

// resolveSymlinksPath resolves symlinks in a path, handling non-existent
// files by walking up to the nearest existing ancestor.
func resolveSymlinksPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
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
			return path
		}
	}
}
