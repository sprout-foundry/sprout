package webui

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

//go:embed static
var embeddedFiles embed.FS

// repoRoot is the absolute path to the repository root, resolved once.
// It is determined by walking up from this file's location (pkg/webui/).
var (
	repoRoot     string
	repoRootOnce sync.Once
)

func getRepoRoot() string {
	repoRootOnce.Do(func() {
		// Resolve the directory containing this source file.
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			repoRoot = "."
			return
		}
		// This file lives at pkg/webui/static_loader.go, so the repo root
		// is two levels up.
		repoRoot = filepath.Join(filepath.Dir(filename), "..", "..")
		repoRoot = filepath.Clean(repoRoot)
	})
	return repoRoot
}

// readStaticFile reads a static asset by name (relative to the static root,
// e.g. "index.html", "js/main.abc123.js").
//
// It first tries the embedded filesystem (populated by "go generate" or a
// manual deploy-ui build).  If the embedded file does not exist — which is
// the common case when the repo is freshly cloned — it falls back to the
// React build output at webui/build/{name} on the local filesystem.
//
// This allows developers to run the application without committing built
// artifacts to version control while still supporting "go install" with
// pre-embedded assets.
func readStaticFile(name string) ([]byte, error) {
	// Defense-in-depth: reject path traversal attempts.  embed.FS is
	// inherently sandboxed, but os.ReadFile is not — so we gate the
	// filesystem fallback with the same constraints the HTTP handlers
	// already enforce.
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return nil, fmt.Errorf("static asset: invalid path")
	}

	// Try embedded files first (baked into the binary).
	data, err := embeddedFiles.ReadFile("static/" + name)
	if err == nil {
		return data, nil
	}

	root := getRepoRoot()

	// Fallback: read from the React build output on the filesystem.
	// CRA produces two tiers of assets:
	//   - Root-level files (index.html, manifest.json, sw.js, icons)
	//     are placed directly in webui/build/.
	//   - Hashed bundles (js/*.js, css/*.css) are nested under
	//     webui/build/static/.
	// The HTTP handlers strip the "/static/" URL prefix before calling us
	// (e.g. "/static/js/main.abc.js" → "js/main.abc.js").  This means
	// the name may or may not contain a subdirectory, so we try both
	// locations to handle either form.
	fsPath := filepath.Join(root, "webui", "build", name)
	data, err = os.ReadFile(fsPath)
	if err == nil {
		return data, nil
	}
	// Hashed bundles live under webui/build/static/{name}.
	data, err = os.ReadFile(filepath.Join(root, "webui", "build", "static", name))
	if err != nil {
		return nil, fmt.Errorf("read static file %q: %w", name, err)
	}
	return data, nil
}

//go:generate node ../../scripts/build-webui-embed.mjs
