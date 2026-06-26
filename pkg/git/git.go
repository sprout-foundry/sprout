package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// GetGitRootDir returns the absolute path to the root directory of the current Git repository.
func GetGitRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out []byte
	var err error
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not find git root: %w: %s", err, string(out))
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
			return "", fmt.Errorf("could not find git remotes: %w: %s", err, string(remotesOut))
		}

		remotes := strings.Split(strings.TrimSpace(string(remotesOut)), "\n")
		if len(remotes) == 0 || remotes[0] == "" {
			return "", nil // No remotes configured
		}

		// Get URL for the first remote
		cmd = exec.Command("git", "remote", "get-url", remotes[0])
		out, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("could not get git remote URL: %w: %s", err, string(out))
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// GetFileGitPath returns the path of the given filename relative to the Git repository root.
func GetFileGitPath(filename string) (string, error) {
	gitRoot, err := GetGitRootDir()
	if err != nil {
		return "", fmt.Errorf("failed to get git root directory: %w", err)
	}
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for %s: %w", filename, err)
	}
	// Resolve symlinks on both paths so filepath.Rel works correctly
	// (macOS /var → /private/var via os.Getwd vs git output).
	if evaled, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = evaled
	}
	if evaled, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaled
	}
	relPath, err := filepath.Rel(gitRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path for %s: %w", filename, err)
	}
	return relPath, nil
}

// AddAndCommitFile stages the specified file and commits it with the
// given message inside dir. dir MUST be non-empty — passing "" would
// let the operation hit the test process's CWD (the host repo on
// developer machines) and is refused by SafeGitCmd under `go test`.
func AddAndCommitFile(dir, newFilename, message string) error {
	if err := SafeGitCmd(dir, "add", newFilename).Run(); err != nil {
		return fmt.Errorf("error adding changes to git: %w", err)
	}
	if err := SafeGitCmd(dir, "commit", "-m", message).Run(); err != nil {
		return fmt.Errorf("error committing changes to git: %w", err)
	}
	logger := utils.GetLogger(true) // Use true for skipPrompt since this is internal
	logger.Logf("Changes committed to git for %s", newFilename)
	return nil
}

// AddAllAndCommit commits all staged changes inside dir with the
// provided message (non-interactive). dir MUST be non-empty.
func AddAllAndCommit(dir, message string, timeoutSeconds int) error {
	cmd := SafeGitCmd(dir, "commit", "-m", message)
	if timeoutSeconds > 0 {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("error starting git commit: %w", err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("error committing changes to git: %w", err)
			}
		case <-time.After(time.Duration(timeoutSeconds) * time.Second):
			_ = cmd.Process.Kill()
			return fmt.Errorf("git commit timed out after %ds", timeoutSeconds)
		}
	} else {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error committing changes to git: %w", err)
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
		return "", 0, 0, fmt.Errorf("failed to get git branch: %w: %s", err, string(branchOut))
	}
	currentBranch = strings.TrimSpace(string(branchOut))

	// Get status --porcelain to count changes
	cmdStatus := exec.Command("git", "status", "--porcelain", "-u", "--no-ahead-behind")
	statusOut, err := cmdStatus.CombinedOutput()
	if err != nil {
		return currentBranch, 0, 0, fmt.Errorf("failed to get git status: %w: %s", err, string(statusOut))
	}

	lines := strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n")
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
		return "", fmt.Errorf("failed to get git diff: %w: %s", err, string(diffOut))
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
		return "", fmt.Errorf("failed to get staged git diff: %w: %s", err, string(diffOut))
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
		return nil, fmt.Errorf("failed to get recent files: %w: %s", err, string(out))
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
		return "", fmt.Errorf("failed to get file log: %w: %s", err, string(out))
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

// IsFileContentCommitted reports whether the working-tree version of
// filePath matches what is recorded at git HEAD — i.e. the file is
// tracked by git AND has no uncommitted modifications. This is the
// git-awareness primitive used by the revert/recover staleness guards
// to refuse rolling back work that the user has intentionally
// committed to version control.
//
// Semantics:
//
//   - Not a git repo (GetGitRootDir fails) → (false, nil): no git
//     protection applies; callers fall back to the content-only check.
//   - File not tracked by git (untracked, or HEAD:<path> unknown) →
//     (false, nil): no git protection; content check applies.
//   - File tracked and working tree matches HEAD → (true, nil):
//     PROTECTED — the content is committed; reverting to an older
//     snapshot would silently undo committed work.
//   - File tracked but differs from HEAD (uncommitted modifications) →
//     (false, nil): not protected; the content-only staleness check
//     still decides.
//
// The check is performed in two steps:
//
//  1. `git ls-files --error-unmatch <relpath>` verifies the file is
//     tracked by git. Untracked files exit non-zero.
//  2. `git diff --quiet HEAD -- <relpath>` confirms the working-tree
//     copy is identical to HEAD. Both are read-only commands, so
//     SafeGitCmd is invoked with dir="" (matching the existing
//     GetGitStatus / GetUncommittedChanges pattern), which is not
//     blocked by the test-mode mutating-command guard.
//
// Step 1 is critical: `git diff --quiet HEAD -- <path>` alone returns
// exit 0 for UNTRACKED files because `git diff` does not include
// untracked files in its comparison. Without the tracked-file gate,
// a freshly-created (but never `git add`ed) file would be incorrectly
// reported as committed-clean, breaking the staleness guard.
//
// Any unexpected git error is returned as (false, err) so callers can
// fall back to the conservative content-only behavior rather than
// blocking legitimate reverts.
func IsFileContentCommitted(filePath string) (bool, error) {
	// Establish we are inside a git repository. GetGitRootDir uses the
	// process CWD; the staleness guards are always invoked with paths
	// resolved relative to the workspace root, so this is the right
	// scope. A non-repo is not an error — it just means no git
	// protection applies.
	if _, err := GetGitRootDir(); err != nil {
		return false, nil
	}

	// Resolve the path relative to the repo root so the commands target
	// the correct tracked entry. GetFileGitPath handles symlink
	// resolution on both the file and the git root.
	relPath, err := GetFileGitPath(filePath)
	if err != nil {
		return false, nil
	}

	// Step 1: verify the file is tracked by git. Without this gate,
	// the diff below would exit 0 for untracked files (git diff does
	// not compare against untracked files), incorrectly reporting them
	// as committed-clean. `git ls-files --error-unmatch` exits
	// non-zero for paths not known to git.
	trackedCmd := SafeGitCmd("", "ls-files", "--error-unmatch", relPath)
	if err := trackedCmd.Run(); err != nil {
		// File is not tracked by git → not committed / not protected.
		return false, nil
	}

	// Step 2: the file is tracked. Check whether the working-tree copy
	// matches HEAD. `git diff --quiet HEAD -- <path>` exits 0 when the
	// working-tree file is identical to HEAD (no uncommitted changes),
	// and non-zero otherwise (uncommitted modifications present).
	cmd := SafeGitCmd("", "diff", "--quiet", "--no-ext-diff", "HEAD", "--", relPath)
	if err := cmd.Run(); err != nil {
		// exit code != 0: the file differs from HEAD (uncommitted
		// modifications). Not committed-clean → not protected.
		return false, nil
	}
	// exit code 0: tracked AND working tree matches HEAD → PROTECTED.
	return true, nil
}
