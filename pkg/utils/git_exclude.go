package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeStatePatterns are the files sprout writes into a workspace's .sprout
// directory while it runs (its log and the web UI's host record). They are
// machine state, not project files: a commit that picks them up adds a log
// and a pid record to the person's repository.
var RuntimeStatePatterns = []string{
	".sprout/workspace.log",
	".sprout/workspace-*.log",
	".sprout/workspace.log.*",
	".sprout/webui_host.json",
}

// EnsureRuntimeStateExcluded adds RuntimeStatePatterns to the repository's
// local exclude file (.git/info/exclude) when dir is the top of a git
// repository, so `git add -A` and commits leave them out without changing
// any file the repository tracks. It is best effort: no repository, a
// worktree (.git is a file) or an unwritable exclude file is not an error.
func EnsureRuntimeStateExcluded(dir string) {
	gitDir := filepath.Join(dir, ".git")
	st, err := os.Stat(gitDir)
	if err != nil || !st.IsDir() {
		return
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	have := map[string]bool{}
	if f, err := os.Open(excludePath); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			have[strings.TrimSpace(sc.Text())] = true
		}
		_ = f.Close()
	}
	var missing []string
	for _, p := range RuntimeStatePatterns {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	var b strings.Builder
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("# sprout runtime state (not project files)\n")
	for _, p := range missing {
		b.WriteString(p + "\n")
	}
	_, _ = f.WriteString(b.String())
}
