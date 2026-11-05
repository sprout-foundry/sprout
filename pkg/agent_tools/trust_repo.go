package tools

// trust_repo.go — repository facts the trust question is decided on: which
// repository a path belongs to, and whether that repository is the
// workspace's own under another checkout (a linked worktree).

import (
	"os"
	"path/filepath"
	"strings"
)

// repoRootOf returns the nearest ancestor of path (inclusive of path when it
// is a directory) that holds a .git entry — a directory for a clone, a file
// for a linked worktree or submodule — or "" when there is none.
func repoRootOf(path string) string {
	dir := filepath.Clean(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitCommonDir returns the git directory a checkout shares with every other
// checkout of the same repository: <root>/.git for a clone, and for a linked
// worktree (whose .git is a file naming its private gitdir) the directory its
// commondir points at. "" when it cannot be read.
func gitCommonDir(root string) string {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return canonicalDir(dotGit)
	}
	data, err := os.ReadFile(dotGit) //nolint:gosec // G304: a .git file inside a path being read
	if err != nil {
		return ""
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	common := gitDir
	if c, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil { //nolint:gosec // G304: git metadata
		common = strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
	}
	return canonicalDir(common)
}

func canonicalDir(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}

// sameRepository reports whether two checkouts are the same repository — the
// workspace and one of its own worktrees (a /task worktree under .sprout/,
// or a worktree checked out elsewhere). Code there is ours by construction.
func sameRepository(a, b string) bool {
	ca, cb := gitCommonDir(a), gitCommonDir(b)
	return ca != "" && ca == cb
}
