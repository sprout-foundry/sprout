package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// IsFileContentCommitted reports whether filePath is tracked by git and its
// working-tree content matches HEAD (committed-clean). Outside a repository,
// or for untracked files, it reports false with no error.
func IsFileContentCommitted(filePath string) (bool, error) {
	info, err := FileCommitState(filePath)
	return info.CommittedClean, err
}

// FileCommitInfo describes a file's state relative to git HEAD.
type FileCommitInfo struct {
	// CommittedClean: tracked, and the working-tree content matches HEAD.
	CommittedClean bool
	// LastCommit is the commit time of the latest commit touching the
	// file (zero when untracked or unknown).
	LastCommit time.Time
}

// FileCommitState inspects filePath in the repository that contains it.
// The repository is found from the file's own directory, not the process
// CWD: the two differ whenever sprout runs outside the workspace (daemon,
// WebUI), and resolving from the CWD silently reported every file as
// uncommitted there — disabling the guards that keep a revert from undoing
// committed work.
func FileCommitState(filePath string) (FileCommitInfo, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return FileCommitInfo{}, nil
	}
	dir := filepath.Dir(absPath)
	for {
		if _, statErr := os.Stat(dir); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return FileCommitInfo{}, nil
		}
		dir = parent
	}
	out, err := SafeGitCmd(dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return FileCommitInfo{}, nil // not in a repository: no git protection applies
	}
	gitRoot := strings.TrimSpace(string(out))
	if evaled, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaled
	}
	resolved := absPath
	if evaled, err := filepath.EvalSymlinks(absPath); err == nil {
		resolved = evaled
	} else if evaledDir, err := filepath.EvalSymlinks(filepath.Dir(absPath)); err == nil {
		resolved = filepath.Join(evaledDir, filepath.Base(absPath))
	}
	relPath, err := filepath.Rel(gitRoot, resolved)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return FileCommitInfo{}, nil
	}

	// Untracked files are never protected; `git diff` below would exit 0
	// for them and misreport them as committed-clean.
	if err := SafeGitCmd(gitRoot, "ls-files", "--error-unmatch", relPath).Run(); err != nil {
		return FileCommitInfo{}, nil
	}
	var info FileCommitInfo
	if out, err := SafeGitCmd(gitRoot, "log", "-1", "--format=%ct", "--", relPath).Output(); err == nil {
		if secs, convErr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); convErr == nil {
			info.LastCommit = time.Unix(secs, 0)
		}
	}
	// `git diff --quiet HEAD -- <path>` exits 0 when the working tree
	// matches HEAD.
	info.CommittedClean = SafeGitCmd(gitRoot, "diff", "--quiet", "--no-ext-diff", "HEAD", "--", relPath).Run() == nil
	return info, nil
}

// CommittedFilePaths returns a set of absolute filesystem paths for
// files that are tracked by git AND whose working-tree content is
// identical to HEAD (committed-clean). This is the batch equivalent of
// IsFileContentCommitted: instead of two subprocess calls per file, it
// runs just two commands total (git ls-files + git diff --name-only HEAD)
// and builds the full set in one pass.
//
// All git commands are run with cmd.Dir=workDir so the function does
// not depend on the process CWD — it resolves the repo containing
// workDir regardless of where the agent process is running.
//
// Callers use this to identify working-tree deltas caused by git
// operations (merge, checkout, reset, pull) that should NOT be recorded
// as recoverable agent edits — a file whose post-operation content
// matches HEAD was aligned to a committed state by git, not edited by
// the agent, so there is nothing legitimate to "recover" back to.
//
// Returns (nil, nil) when workDir is not inside a git repository — no
// git protection applies and callers should record all deltas.
func CommittedFilePaths(workDir string) (map[string]bool, error) {
	if workDir == "" {
		return nil, nil
	}

	// Resolve the repo root containing workDir.
	rootCmd := SafeGitCmd(workDir, "rev-parse", "--show-toplevel")
	rootOut, err := rootCmd.Output()
	if err != nil {
		return nil, nil // not a git repo
	}
	root := strings.TrimSpace(string(rootOut))
	if root == "" {
		return nil, nil
	}

	// The workDir itself may be a symlink — the workspace walker records
	// paths using the original (pre-symlink) workDir, while git resolves
	// symlinks and reports the real path. We must build absolute paths
	// in BOTH forms so the caller can match regardless of which form the
	// walker used. This mirrors the symlink resolution strategy in
	// GetFileGitPath and the agent's isOutsideWorkspace helper.
	rootResolved := root
	if evaled, e := filepath.EvalSymlinks(root); e == nil {
		rootResolved = evaled
	}

	// Step 1: enumerate every tracked file (repo-relative paths,
	// null-terminated for robustness against spaces / special chars).
	trackedCmd := SafeGitCmd(workDir, "ls-files", "-z")
	trackedOut, err := trackedCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	// Step 2: enumerate files that differ from HEAD. `git diff
	// --name-only` (without --quiet) exits 0 on success regardless of
	// whether differences exist; the differing paths are in stdout.
	diffCmd := SafeGitCmd(workDir, "diff", "--name-only", "-z", "--no-ext-diff", "HEAD")
	diffOut, err := diffCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only HEAD: %w", err)
	}

	// Build the "differs from HEAD" set for O(1) lookup.
	differs := make(map[string]bool)
	for _, p := range strings.Split(string(diffOut), "\x00") {
		if p != "" {
			differs[p] = true
		}
	}

	// Committed-clean = tracked AND NOT in the diff set. Resolve each
	// repo-relative path to absolute filesystem paths in BOTH the raw
	// git-root form and the symlink-resolved form, so callers using
	// either representation of the workspace can match.
	committed := make(map[string]bool)
	for _, p := range strings.Split(string(trackedOut), "\x00") {
		if p == "" {
			continue
		}
		if differs[p] {
			continue
		}
		committed[filepath.Join(root, p)] = true
		if rootResolved != root {
			committed[filepath.Join(rootResolved, p)] = true
		}
		// Also add paths using the original workDir as prefix, which may
		// be a symlink that the workspace walker used un-resolved (e.g.,
		// macOS /var → /private/var). Without this, the caller's pending
		// change paths won't match the committed set.
		absWork, _ := filepath.Abs(workDir)
		if absWork != "" && absWork != root && absWork != rootResolved {
			committed[filepath.Join(absWork, p)] = true
		}
	}
	return committed, nil
}
