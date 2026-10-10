package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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
	real = filepath.Clean(real)
	if err := refuseBroadDir(real); err != nil {
		return "", err
	}
	return real, nil
}

// refuseBroadDir rejects directories too broad to hand to a workspace: the
// filesystem root, the user's home or any of its ancestors (a writable home
// lets workspace code plant shell profiles and login agents that later run
// outside any sandbox), and the runner's own state directory, its ancestors
// or anything inside it (workspace metadata and credentials live there).
// A project directory under the home is the intended shape.
func refuseBroadDir(real string) error {
	if real == filepath.Dir(real) {
		return fmt.Errorf("workspace directory %q is the filesystem root; name a project directory", real)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if h, err := filepath.EvalSymlinks(home); err == nil && containsPath(real, filepath.Clean(h)) {
			return fmt.Errorf("workspace directory %q is your home directory or contains it; name a project directory inside it", real)
		}
	}
	for _, base := range sproutDataDirs() {
		if containsPath(real, base) || containsPath(base, real) {
			return fmt.Errorf("workspace directory %q overlaps sprout's own data directory %q", real, base)
		}
	}
	return nil
}

// sproutDataDirs are sprout's config and state roots, symlink-resolved where
// they exist: the runner's state, workspace metadata and stored credentials
// all live under them.
func sproutDataDirs() []string {
	var dirs []string
	if d, err := configuration.GetConfigDir(); err == nil {
		dirs = append(dirs, d)
	}
	if d, err := envutil.StateDir(); err == nil {
		dirs = append(dirs, d)
	}
	for i, d := range dirs {
		d = filepath.Clean(d)
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = filepath.Clean(r)
		}
		dirs[i] = d
	}
	return dirs
}

// containsPath reports whether child is parent or lies inside it. Both must be
// clean absolute paths.
func containsPath(parent, child string) bool {
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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
