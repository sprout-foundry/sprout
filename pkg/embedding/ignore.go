package embedding

import (
	"context"
	"bytes"
	"fmt"
	"log"
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
// files with recognized extensions (.go, .ts, .tsx, .js, .tsx, .mjs, .py) are
// included. Directories matching Layer 1 skip patterns are pruned (no recursion).
//
// It accepts a context for cancellation and applies three protections:
//  - A 30-second absolute timeout (WalkTimeout).
//  - A maximum directory depth of 15 (MaxDepth).
//  - A cap of 10,000 collected files (MaxFileCount).
//
// Progress is logged every ProgressInterval files.
// If the context is cancelled or any limit is hit, the files collected so far
// are returned with no error (partial result).
func WalkCodeFiles(ctx context.Context, root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, WalkTimeout)
	defer cancel()

	var files []string
	root = filepath.Clean(root)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		// Check context on every callback to exit early on timeout/cancellation.
		if err := ctx.Err(); err != nil {
			if err == context.DeadlineExceeded {
				log.Printf("embedding: walk timed out after %d files (limit %d)", len(files), MaxFileCount)
			} else {
				log.Printf("embedding: walk cancelled after %d files: %v", len(files), err)
			}
			// Return the sentinel to stop the walk.
			return ctx.Err()
		}

		if err != nil {
			return nil // skip unreadable entries silently
		}

		// Enforce maximum directory depth.
		if d.IsDir() {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			depth := strings.Count(rel, string(filepath.Separator))
			if depth >= MaxDepth {
				return filepath.SkipDir
			}

			// Prune directories that match Layer 1 skip list.
			name := d.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Enforce file count cap.
		if len(files) >= MaxFileCount {
			log.Printf("embedding: walk file limit reached (%d files), stopping walk", MaxFileCount)
			return fmt.Errorf("embedding: walk file limit reached (%d)", MaxFileCount)
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

		// Emit progress log every ProgressInterval files.
		if len(files)%ProgressInterval == 0 {
			log.Printf("embedding: walk progress: %d files collected", len(files))
		}

		return nil
	})

	// Walk returns context.Canceled, context.DeadlineExceeded, or our file-limit
	// error when we stop early. In those cases we return the partial results.
	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			return files, nil
		}
		if strings.Contains(err.Error(), "walk file limit reached") {
			return files, nil
		}
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
