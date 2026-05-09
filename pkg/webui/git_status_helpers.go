package webui

import (
	"os/exec"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// getGitFileStatusMap runs git status --porcelain once and returns sets of modified and untracked files.
func getGitFileStatusMap(workspaceRoot string) (modified, untracked map[string]bool) {
	modified = make(map[string]bool)
	untracked = make(map[string]bool)

	// Check if we're in a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = workspaceRoot
	if err := cmd.Run(); err != nil {
		return
	}

	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = workspaceRoot
	output, err := cmd.Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		// Format: XY<space><filename>  (X=staged, Y=unstaged, ??=untracked)
		if len(line) < 4 {
			continue
		}
		staged := string(line[0])
		unstaged := string(line[1])
		filePath := strings.TrimSpace(line[3:])

		// Untracked files
		if staged == "?" && unstaged == "?" {
			untracked[filePath] = true
			continue
		}

		// Modified, added, renamed, deleted files (both staged and unstaged)
		if unstaged == "M" || unstaged == "D" || staged == "M" || staged == "A" || staged == "D" || staged == "R" || staged == "C" {
			modified[filePath] = true
		}
	}

	return
}

// getGitStatusForEntry determines the git status for a single file or directory entry.
func getGitStatusForEntry(relPath string, isDir bool, modified, untracked map[string]bool, ignoreRules *ignore.GitIgnore, workspaceRoot string) string {
	// Special case: .git directory is always gitignored
	if isDir && relPath == ".git" {
		return "ignored"
	}

	if ignoreRules != nil {
		if isDir {
			if ignoreRules.MatchesPath(relPath) || ignoreRules.MatchesPath(relPath+"/") {
				return "ignored"
			}
		} else {
			if ignoreRules.MatchesPath(relPath) {
				return "ignored"
			}
		}
	}

	if modified[relPath] {
		return "modified"
	}

	if untracked[relPath] {
		return "untracked"
	}

	// For directories, check if any child has modified or untracked status
	if isDir {
		prefix := relPath + "/"
		for p := range modified {
			if strings.HasPrefix(p, prefix) {
				return "modified"
			}
		}
		for p := range untracked {
			if strings.HasPrefix(p, prefix) {
				return "untracked"
			}
		}
	}

	return ""
}
