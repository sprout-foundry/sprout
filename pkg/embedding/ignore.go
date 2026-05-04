package embedding

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs holds directory component names that should never be indexed.
var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"__pycache__":  true,
	".tox":         true,
	".venv":        true,
	"venv":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".next":        true,
	".nuxt":        true,
	"coverage":     true,
	".cache":       true,
	".gradle":      true,
	".mvn":         true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
}

// ShouldIgnorePath reports whether the given path should be excluded from
// indexing. It applies two layers of filtering:
//
// Layer 1 — Hard-coded directory and filename patterns.
// Layer 2 — Binary file detection (null byte in first 8 KB).
//
// The repoRoot parameter is reserved for future gitignore-based filtering.
func ShouldIgnorePath(path string, repoRoot string) bool {
	if layer1Ignore(path) {
		return true
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && IsBinaryFile(path) {
		return true
	}
	return false
}

// layer1Ignore returns true when a path matches any hard-coded exclusion rule.
func layer1Ignore(path string) bool {
	// Check each path component against skip directory names.
	p := filepath.Clean(path)
	parts := strings.Split(p, string(filepath.Separator))
	for _, part := range parts {
		if skipDirs[part] {
			return true
		}
	}

	base := filepath.Base(path)

	// Exact filename matches.
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum":
		return true
	}

	// Glob-like suffix patterns.
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
		return true
	}
	// Files ending in .map (source maps).
	if strings.HasSuffix(base, ".map") {
		return true
	}
	// Files ending in .lock (generic lock files beyond the exact matches above).
	if strings.HasSuffix(base, ".lock") {
		return true
	}

	return false
}

// supportedCodeExtensions lists file extensions that should be collected
// during directory walks for indexing.
var supportedCodeExtensions = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".py":  true,
}

// hasSupportedExtension returns true if the file path has a recognized source-code extension.
func hasSupportedExtension(path string) bool {
	return supportedCodeExtensions[filepath.Ext(path)]
}

// WalkCodeFiles walks the directory tree rooted at root and returns all file
// paths that should be indexed (i.e., those that pass ShouldIgnorePath). Only
// files with recognized extensions (.go, .ts, .tsx, .js, .jsx, .mjs) are
// included. Directories matching Layer 1 skip patterns are pruned (no recursion).
func WalkCodeFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries silently
		}

		// Prune directories that match Layer 1 skip list.
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Only collect recognized source files.
		if !hasSupportedExtension(path) {
			return nil
		}

		// Apply full ignore logic.
		if ShouldIgnorePath(path, root) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: walk %s: %w", root, err)
	}
	return files, nil
}

// IsBinaryFile reports whether the file at path appears to be binary.
// It reads up to the first 8 KB and checks for a null byte (0x00).
func IsBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && !os.IsNotExist(err) {
		return false
	}
	return bytes.Contains(buf[:n], []byte{0x00})
}
