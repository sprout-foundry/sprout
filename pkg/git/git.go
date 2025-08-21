package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

// GetGitRootDir returns the absolute path to the root directory of the current Git repository.
func GetGitRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out []byte
	var err error
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not find git root: %v", string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// GetGitRemoteURL returns the remote URL of the current Git repository.
func GetGitRemoteURL() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	var out []byte
	var err error
	if out, err = cmd.CombinedOutput(); err != nil {
		// Try to get any remote if origin doesn't exist
		cmd = exec.Command("git", "remote")
		remotesOut, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("could not find git remotes: %v", string(remotesOut))
		}

		remotes := strings.Split(strings.TrimSpace(string(remotesOut)), "\n")
		if len(remotes) == 0 || remotes[0] == "" {
			return "", nil // No remotes configured
		}

		// Get URL for the first remote
		cmd = exec.Command("git", "remote", "get-url", remotes[0])
		out, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("could not get git remote URL: %v", string(out))
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// GetFileGitPath returns the path of the given filename relative to the Git repository root.
func GetFileGitPath(filename string) (string, error) {
	gitRoot, err := GetGitRootDir()
	if err != nil {
		return filename, fmt.Errorf("failed to get git root directory: %w", err)
	}
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return filename, fmt.Errorf("failed to get absolute path for %s: %w", filename, err)
	}
	relPath, err := filepath.Rel(gitRoot, absPath)
	if err != nil {
		return filename, fmt.Errorf("failed to get relative path for %s: %w", filename, err)
	}
	return relPath, nil
}

// AddAndCommitFile stages the specified file and commits it with the given message.
func AddAndCommitFile(newFilename, message string) error {
	if err := exec.Command("git", "add", newFilename).Run(); err != nil {
		return fmt.Errorf("error adding changes to git: %v", err)
	}
	if err := exec.Command("git", "commit", "-m", message).Run(); err != nil {
		return fmt.Errorf("error committing changes to git: %v", err)
	}
	logger := utils.GetLogger(true) // Use true for skipPrompt since this is internal
	logger.Logf("Changes committed to git for %s", newFilename)
	return nil
}

// AddAllAndCommit commits all staged changes with the provided message (non-interactive).
func AddAllAndCommit(message string, timeoutSeconds int) error {
	cmd := exec.Command("git", "commit", "-m", message)
	if timeoutSeconds > 0 {
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("error committing changes to git: %v", err)
			}
		case <-time.After(time.Duration(timeoutSeconds) * time.Second):
			_ = cmd.Process.Kill()
			return fmt.Errorf("git commit timed out after %ds", timeoutSeconds)
		}
	} else {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error committing changes to git: %v", err)
		}
	}
	return nil
}

// GetGitStatus returns the current branch, number of uncommitted changes, and number of staged changes.
func GetGitStatus() (currentBranch string, uncommittedChanges int, stagedChanges int, err error) {
	// Get current branch
	cmdBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := cmdBranch.CombinedOutput()
	if err != nil {
		// If not in a git repo, or no commits yet, this might fail.
		// Return empty string and 0 counts, but still indicate an error if it's not just "not a git repo".
		if strings.Contains(strings.ToLower(string(branchOut)), "not a git repository") {
			return "", 0, 0, nil // Not an error if it's just not a git repo
		}
		return "", 0, 0, fmt.Errorf("failed to get git branch: %v", string(branchOut))
	}
	currentBranch = strings.TrimSpace(string(branchOut))

	// Get status --porcelain to count changes
	cmdStatus := exec.Command("git", "status", "--porcelain", "-u", "--no-ahead-behind")
	statusOut, err := cmdStatus.CombinedOutput()
	if err != nil {
		return currentBranch, 0, 0, fmt.Errorf("failed to get git status: %v", string(statusOut))
	}

	lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// X is the status of the index (staged), Y is the status of the working tree (uncommitted)
		// XY PATH
		// M  file.txt (staged modified, uncommitted modified)
		// M  file.txt (staged modified, uncommitted unchanged)
		//  M file.txt (staged unchanged, uncommitted modified)
		// A  file.txt (staged added)
		// D  file.txt (staged deleted)
		//  A file.txt (untracked added) - this is not staged or uncommitted in the sense of tracked files
		// ?? file.txt (untracked)

		// Staged changes (X column)
		if len(line) >= 1 && line[0] != ' ' && line[0] != '?' { // ' ' means not staged, '?' means untracked
			stagedChanges++
		}
		// Uncommitted changes (Y column)
		if len(line) >= 2 && line[1] != ' ' && line[1] != '?' { // ' ' means not uncommitted, '?' means untracked
			uncommittedChanges++
		}
	}

	return currentBranch, uncommittedChanges, stagedChanges, nil
}

// GetUncommittedChanges returns detailed information about uncommitted changes in the repository.
func GetUncommittedChanges() (string, error) {
	// Get the diff of uncommitted changes
	cmd := exec.Command("git", "diff", "--no-color", "--no-ext-diff")
	diffOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get git diff: %v", string(diffOut))
	}

	diff := strings.TrimSpace(string(diffOut))
	if diff == "" {
		return "", nil // No uncommitted changes
	}

	// Truncate if too long to keep under token limit
	const maxDiffLength = 5000 // Limit diff length to help stay under 2000 tokens
	if len(diff) > maxDiffLength {
		diff = diff[:maxDiffLength] + "\n... (diff truncated for brevity)"
	}

	return diff, nil
}

// GetStagedChanges returns detailed information about staged changes in the repository.
func GetStagedChanges() (string, error) {
	// Get the diff of staged changes
	cmd := exec.Command("git", "diff", "--cached", "--no-color", "--no-ext-diff")
	diffOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get staged git diff: %v", string(diffOut))
	}

	diff := strings.TrimSpace(string(diffOut))
	if diff == "" {
		return "", nil // No staged changes
	}

	// Truncate if too long to keep under token limit
	const maxDiffLength = 5000 // Limit diff length to help stay under 2000 tokens
	if len(diff) > maxDiffLength {
		diff = diff[:maxDiffLength] + "\n... (diff truncated for brevity)"
	}

	return diff, nil
}

// GetRecentTouchedFiles returns a de-duplicated list of files touched in the last N commits
func GetRecentTouchedFiles(numCommits int) ([]string, error) {
	if numCommits <= 0 {
		numCommits = 5
	}
	cmd := exec.Command("git", "log", "-n", fmt.Sprintf("%d", numCommits), "--name-only", "--pretty=format:")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get recent files: %v", string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	seen := map[string]bool{}
	var files []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if !seen[ln] {
			seen[ln] = true
			files = append(files, ln)
		}
	}
	return files, nil
}

// GetRecentFileLog returns a short summary of recent commits for a file
func GetRecentFileLog(filePath string, limit int) (string, error) {
	if limit <= 0 {
		limit = 3
	}
	cmd := exec.Command("git", "log", "-n", fmt.Sprintf("%d", limit), "--pretty=format:%h %ad %an %s", "--date=short", "--", filePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get file log: %v", string(out))
	}
	log := strings.TrimSpace(string(out))
	if log == "" {
		return "(no recent commits)", nil
	}
	// Limit lines to avoid prompt bloat
	lines := strings.Split(log, "\n")
	if len(lines) > limit {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n"), nil
}
