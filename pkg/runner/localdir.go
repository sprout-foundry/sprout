package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalDir validates and normalizes a user-named directory the runner may
// serve workspaces in place: it must exist, be absolute, contain no ':' (the
// docker volume syntax would misparse it), and be a directory. The returned
// path is symlink-resolved — the same form start tasks are compared in — so
// an allowlist entry and a task naming the same directory through a symlink
// (macOS /tmp, a homedir symlink) match. A tilde leading the path expands to
// the user's home (flag ergonomics; the stored state and the protocol carry
// the expanded absolute path).
func LocalDir(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("workspace directory is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding ~: %w", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("workspace directory %q must be an absolute path", raw)
	}
	if strings.Contains(p, ":") {
		return "", fmt.Errorf("workspace directory %q must not contain %q", raw, ":")
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("workspace directory %q is not reachable: %w", p, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace directory %q is not a directory", p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolving workspace directory %q: %w", p, err)
	}
	return filepath.Clean(real), nil
}

// resolveLocalTaskDir returns the real path of a start task's WorkspaceDir
// with symlinks resolved, for comparing against the runner's allowlist: an
// allowlisted parent reached through a symlink (macOS /tmp, a homedir
// symlink) must match, and a symlink that escapes the allowlist must not
// sneak a task through.
func resolveLocalTaskDir(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("workspace_dir %q must be an absolute path", dir)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolving workspace_dir %q: %w", dir, err)
	}
	return filepath.Clean(real), nil
}

// allowedLocalDir reports whether taskDir names an allowlisted directory.
// Both sides are symlink-resolved absolute paths: LocalDir resolves the
// allowlist at configuration time and resolveLocalTaskDir resolves the task's
// path at start time, so the same directory matches through either spelling.
func allowedLocalDir(taskDir string, allowlist []string) bool {
	for _, a := range allowlist {
		if a == taskDir {
			return true
		}
	}
	return false
}
