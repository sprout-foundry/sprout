package embedding

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs holds directory component names that should never be indexed.
// This includes build artifacts, package managers, AND security-sensitive
// or user-data directories. When the embedding index walks from a home
// directory (daemon/service mode), these entries prevent it from touching
// private keys, credentials, media libraries, and other non-project data.
var skipDirs = map[string]bool{
	// Package managers
	"node_modules": true,
	"vendor":       true,
	// Version control
	".git":  true,
	".hg":   true,
	".svn":  true,
	".npm":  true,
	".yarn": true,
	".pnp":  true,
	// Python
	"__pycache__": true,
	".tox":        true,
	".venv":       true,
	"venv":        true,
	"env":         true,
	".env":        true,
	".direnv":     true,
	// JavaScript/Node
	".next":    true,
	".nuxt":    true,
	".turbo":   true,
	"coverage": true,
	".cache":   true,
	// Java/Kotlin
	".gradle": true,
	".mvn":    true,
	// Build artifacts
	"dist":             true,
	"build":            true,
	"out":              true,
	"target":           true,
	"storybook-static": true, // Storybook build output
	".storybook":       true, // Storybook config
	// IDE
	".idea":   true,
	".vscode": true,
	// Terraform
	".terraform": true,
	// Sprout-specific
	".ledit":   true, // Agent revision history / session data
	".agent-i": true, // Agent session data
	".sprout":  true, // Sprout runtime data (run/embeddings)
	// Security-sensitive: credentials, keys, auth tokens
	".ssh":    true, // SSH private keys, known hosts
	".aws":    true, // AWS credentials and config
	".kube":   true, // Kubernetes configs and tokens
	".gnupg":  true, // GPG keys
	".gpg":    true, // GPG keys (alternate)
	".pki":    true, // Public key infrastructure
	".vault":  true, // HashiCorp Vault
	".docker": true, // Docker credentials (config.json with auth tokens)
	// User data directories (macOS / Linux home)
	"Library":      true, // macOS app data, keychains, caches
	"Applications": true, // macOS apps
	"Desktop":      true, // User desktop files
	"Downloads":    true, // User downloads
	"Documents":    true, // User documents
	"Music":        true, // User media
	"Pictures":     true, // User media
	"Videos":       true, // User media
	"Public":       true, // macOS shared folder
	// Email
	".maildir": true,
	"Maildir":  true,
	// E-readers / books
	"calibre": true,
	// Trash
	".Trash": true,
	// Config and local data (may contain sensitive app configs)
	".config": true,
	".local":  true,
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
// during directory walks for code-only indexing.
var supportedCodeExtensions = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".py":  true,
}

// supportedIndexableExtensions lists all file extensions that should be collected
// during directory walks for full indexing (code + non-code files).
var supportedIndexableExtensions = map[string]bool{
	// Code extensions (from supportedCodeExtensions)
	".go":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".py":  true,

	// Documentation
	".md":  true,
	".rst": true,
	".txt": true,

	// Configuration
	".yaml": true,
	".yml":  true,
	".toml": true,
	".json": true,
	".xml":  true,

	// Web/data
	".html": true,
	".css":  true,
	".sql":  true,

	// Shell scripts
	".sh":   true,
	".bash": true,
	".zsh":  true,
	".fish": true,

	// Config/build
	".env":    true,
	".cfg":    true,
	".ini":    true,
	".conf":   true,
	".gradle": true,
	".cmake":  true,
}

// specialFilenames lists files without extensions that should be indexed.
var specialFilenames = map[string]bool{
	"Makefile":      true,
	"Dockerfile":    true,
	".dockerignore": true,
	".gitignore":    true,
	".env.example":  true,
	"AGENTS.md":     true,
}

// hasSupportedExtension returns true if the file path has a recognized source-code extension.
func hasSupportedExtension(path string) bool {
	return supportedCodeExtensions[filepath.Ext(path)]
}

// hasIndexableExtension returns true if the file path has a recognized extension
// for full indexing (code + non-code files).
func hasIndexableExtension(path string) bool {
	return supportedIndexableExtensions[filepath.Ext(path)]
}

// isSpecialFilename returns true if the file basename matches a special filename
// (files without extensions that should be indexed).
func isSpecialFilename(path string) bool {
	return specialFilenames[filepath.Base(path)]
}

// WalkCodeFiles walks the directory tree rooted at root and returns all file
// paths that should be indexed (i.e., those that pass ShouldIgnorePath). Only
// files with recognized extensions (.go, .ts, .tsx, .js, .tsx, .mjs, .py) are
// included. Directories matching Layer 1 skip patterns are pruned (no recursion).
//
// It accepts a context for cancellation and applies three protections:
//   - A 30-second absolute timeout (WalkTimeout).
//   - A maximum directory depth of 15 (MaxDepth).
//   - A cap of 10,000 collected files (MaxFileCount).
//
// Progress is logged every ProgressInterval files.
// If the context is cancelled or any limit is hit, the files collected so far
// are returned with no error (partial result).
func WalkCodeFiles(ctx context.Context, root string) ([]string, error) {
	return walkFiles(ctx, root, hasSupportedExtension, false)
}

// WalkAllIndexableFiles walks the directory tree rooted at root and returns all
// file paths that should be indexed — both code files (for symbol extraction)
// and non-code files (for file-level embedding). It includes all extensions
// from supportedCodeExtensions plus supported non-code extensions (.md, .yaml,
// .json, .sh, etc.) and special filenames (Makefile, Dockerfile, .gitignore, etc.).
//
// It accepts a context for cancellation and applies three protections:
//   - A 30-second absolute timeout (WalkTimeout).
//   - A maximum directory depth of 15 (MaxDepth).
//   - A cap of 10,000 collected files (MaxFileCount).
//
// Progress is logged every ProgressInterval files.
// If the context is cancelled or any limit is hit, the files collected so far
// are returned with no error (partial result).
func WalkAllIndexableFiles(ctx context.Context, root string) ([]string, error) {
	return walkFiles(ctx, root, func(path string) bool {
		return hasIndexableExtension(path) || isSpecialFilename(path)
	}, true)
}

// walkFiles is the shared implementation for walking files with a custom
// extension checker. The checkSpecial parameter indicates whether to check
// special filenames in addition to extensions.
func walkFiles(ctx context.Context, root string, extensionCheck func(path string) bool, checkSpecial bool) ([]string, error) {
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

		// Only collect recognized source files (using the provided extension checker).
		if !extensionCheck(path) {
			return nil
		}

		// Apply full ignore logic.
		if ShouldIgnorePath(path, root) {
			return nil
		}

		files = append(files, path)

		// Emit progress log every ProgressInterval files.
		if len(files)%ProgressInterval == 0 {
			debugLogf("embedding: walk progress: %d files collected", len(files))
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
