package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// isRootedPath reports whether p names a location independent of the
// working directory. On Windows "/etc/passwd" and "\x" are not IsAbs (they
// lack a drive) yet still resolve against the current drive's root, so
// treating them as relative would place them inside the workspace.
func isRootedPath(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}

// absPathLexical cleans a path and makes it absolute (relative to the
// process cwd, or the current drive for rooted Windows paths) without
// touching the filesystem. Returns "" for empty input.
func absPathLexical(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// pathWithin reports whether path equals root or sits beneath it,
// component-aware and case-insensitive on Windows. Both must be cleaned.
func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		path, root = strings.ToLower(path), strings.ToLower(root)
	}
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(root, sep)+sep)
}

func isOSTempPath(p string) bool {
	tmp := os.TempDir()
	return tmp != "" && pathWithin(absPathLexical(p), absPathLexical(tmp))
}
