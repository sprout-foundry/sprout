package tools

// The project's files, as its look is read from them (mockup.Files): the
// files git tracks, or the folder walked (without dependency and build
// folders) when it is not a repository. Reading is bounded so a large
// repository does not cost a walk of everything.

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// mockupLookFileMax is the largest file read for a look.
	mockupLookFileMax = 128 << 10
	// mockupLookMaxPaths bounds a project folder walked without git.
	mockupLookMaxPaths = 20000
)

// projectFolderFiles is a project folder's files, for reading its look.
type projectFolderFiles struct {
	root  string
	paths []string
}

func newProjectFolderFiles(root string) *projectFolderFiles {
	return &projectFolderFiles{root: root, paths: projectFolderPaths(root)}
}

func (w *projectFolderFiles) Paths() []string { return w.paths }

func (w *projectFolderFiles) Read(p string) (string, bool) {
	full := filepath.Join(w.root, filepath.FromSlash(p))
	if rel, err := filepath.Rel(w.root, full); err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() || info.Size() > mockupLookFileMax {
		return "", false
	}
	data, err := os.ReadFile(full)
	if err != nil || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

// projectFolderPaths lists a project folder's files: git's tracked set when it is a
// repository, else a bounded walk that skips dependency and build folders.
func projectFolderPaths(root string) []string {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		var paths []string
		for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if p != "" {
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		return paths
	}
	var paths []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "build", ".next", "coverage", ".sprout":
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= mockupLookMaxPaths {
			return filepath.SkipAll
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}
