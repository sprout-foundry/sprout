package changes

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// resolveAbsPath resolves filePath to a cleaned absolute path, using
// the agent's workspace root as the base for relative paths.
func (ct *ChangeTracker) resolveAbsPath(filePath string) string {
	if filepath.IsAbs(filePath) {
		return filepath.Clean(filePath)
	}
	root := ""
	if ct.view != nil {
		root = ct.view.GetWorkspaceRoot()
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return filePath
		}
	}
	joined := filepath.Join(root, filePath)
	// A drive-less rooted Windows path ("/etc/shadow") names the root of
	// the workspace's drive, as pkg/filesystem resolves it for the write —
	// except /tmp, which follows Git Bash to the user's temp dir.
	if tmp, ok := filesystem.PosixTmpPath(filePath); ok {
		joined = tmp
	} else if filePath != "" && os.IsPathSeparator(filePath[0]) {
		joined = filepath.VolumeName(root) + filepath.Clean(filePath)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return filePath
	}
	return abs
}

// IsOutsideWorkspace returns true if filePath is outside the agent's
// workspace root. Exported for pkg/agent's IsPathOutsideWorkspace
// facade (SP-141 phase 2).
func (ct *ChangeTracker) IsOutsideWorkspace(filePath string) bool {
	if ct.view == nil {
		return false
	}
	workspaceRoot := ct.view.GetWorkspaceRoot()
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

	// Rel fails when the two paths share no root (different Windows
	// volumes); such a file is outside by definition.
	rel, err := filepath.Rel(absWorkspace, absFile)
	if err != nil {
		return true
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
		parent := filepath.Dir(dir)
		if parent == dir || parent == "." {
			return path
		}
		dir = parent
	}
}
