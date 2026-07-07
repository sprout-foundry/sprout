//go:build !js

package webui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type gitFixReviewJob struct {
	ID            string
	SessionID     string
	ClientID      string
	WorkspaceRoot string
	Status        string
	Logs          []string
	Result        string
	Error         string
	StartedAt     time.Time
	UpdatedAt     time.Time
	streamBuf     strings.Builder
	mutex         sync.RWMutex
}

// GitStatus represents the git status response
type GitStatus struct {
	Branch    string    `json:"branch"`
	Ahead     int       `json:"ahead"`
	Behind    int       `json:"behind"`
	Staged    []GitFile `json:"staged"`
	Modified  []GitFile `json:"modified"`
	Untracked []GitFile `json:"untracked"`
	Deleted   []GitFile `json:"deleted"`
	Renamed   []GitFile `json:"renamed"`
	// Truncated indicates whether any file lists were truncated due to limits
	Truncated bool `json:"truncated"`
	InGitRepo bool `json:"in_git_repo"`
}

// GitFile represents a file with its git status
type GitFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged,omitempty"`
}

// findGitRoot walks up from dir to find the nearest directory containing a .git
// folder. Returns the resolved path, or the original dir if no git repo is found.
func findGitRoot(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return dir
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return dir
}

func (ws *ReactWebServer) getGitStatusForWorkspace(workspaceRoot string) (*GitStatus, error) {
	// Walk up the directory tree to find the nearest git repo, matching git's
	// own behavior. The workspace root may be the user's home dir or a parent
	// of the actual project, so we need to locate the repo first.
	gitRoot := findGitRoot(workspaceRoot)

	// Check if we're in a git repository
	cmd := ws.gitCommandForWorkspace(gitRoot, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		// Not in a git repository
		return &GitStatus{
			Branch:    "",
			Staged:    []GitFile{},
			Modified:  []GitFile{},
			Untracked: []GitFile{},
			Truncated: false,
			InGitRepo: false,
		}, nil
	}

	// Get branch and tracking info
	status := &GitStatus{InGitRepo: true}

	// Get current branch
	cmd = ws.gitCommandForWorkspace(gitRoot, "branch", "--show-current")
	output, err := cmd.Output()
	if err == nil {
		status.Branch = strings.TrimSpace(string(output))
	}

	// Get ahead/behind info
	cmd = ws.gitCommandForWorkspace(gitRoot, "rev-list", "--count", "--left-right", "@{u}...HEAD")
	output, err = cmd.Output()
	if err == nil {
		parts := strings.Fields(string(output))
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &status.Behind)
			fmt.Sscanf(parts[1], "%d", &status.Ahead)
		}
	}

	// Get staged changes.
	// Use tab-separated parsing so file names with spaces are preserved.
	cmd = ws.gitCommandForWorkspace(gitRoot, "diff", "--name-status", "--cached")
	output, err = cmd.Output()
	if err == nil {
		allStaged := []GitFile{}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if statusChar, path, ok := parseNameStatusLine(line); ok {
				allStaged = append(allStaged, GitFile{
					Path:   path,
					Status: statusChar,
					Staged: true,
				})
			}
		}
		if len(allStaged) > maxFilesPerSection {
			status.Staged = allStaged[:maxFilesPerSection]
			status.Truncated = true
		} else {
			status.Staged = allStaged
		}
	}

	// Get unstaged changes.
	// Use tab-separated parsing so file names with spaces are preserved.
	cmd = ws.gitCommandForWorkspace(gitRoot, "diff", "--name-status")
	output, err = cmd.Output()
	if err == nil {
		allModified := []GitFile{}
		allDeleted := []GitFile{}
		allRenamed := []GitFile{}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if statusChar, path, ok := parseNameStatusLine(line); ok {
				if statusChar == "M" {
					allModified = append(allModified, GitFile{
						Path:   path,
						Status: "M",
						Staged: false,
					})
				} else if statusChar == "D" {
					allDeleted = append(allDeleted, GitFile{
						Path:   path,
						Status: "D",
						Staged: false,
					})
				} else if statusChar == "A" {
					allModified = append(allModified, GitFile{
						Path:   path,
						Status: "A",
						Staged: false,
					})
				} else if statusChar == "R" {
					allRenamed = append(allRenamed, GitFile{
						Path:   path,
						Status: "R",
						Staged: false,
					})
				}
			}
		}
		if len(allModified) > maxFilesPerSection {
			status.Modified = allModified[:maxFilesPerSection]
			status.Truncated = true
		} else {
			status.Modified = allModified
		}
		if len(allDeleted) > maxFilesPerSection {
			status.Deleted = allDeleted[:maxFilesPerSection]
			status.Truncated = true
		} else {
			status.Deleted = allDeleted
		}
		if len(allRenamed) > maxFilesPerSection {
			status.Renamed = allRenamed[:maxFilesPerSection]
			status.Truncated = true
		} else {
			status.Renamed = allRenamed
		}
	}

	// Get untracked files
	cmd = ws.gitCommandForWorkspace(gitRoot, "ls-files", "--others", "--exclude-standard")
	output, err = cmd.Output()
	if err == nil {
		allUntracked := []GitFile{}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			allUntracked = append(allUntracked, GitFile{
				Path:   strings.TrimSpace(line),
				Status: "?",
				Staged: false,
			})
		}
		if len(allUntracked) > maxFilesPerSection {
			status.Untracked = allUntracked[:maxFilesPerSection]
			status.Truncated = true
		} else {
			status.Untracked = allUntracked
		}
	}

	return status, nil
}

// getAllGitFiles converts status to single file list for backward compatibility
func getAllGitFiles(status *GitStatus) []GitFile {
	files := []GitFile{}
	files = append(files, status.Staged...)
	files = append(files, status.Modified...)
	files = append(files, status.Untracked...)
	files = append(files, status.Deleted...)
	files = append(files, status.Renamed...)
	return files
}

// Maximum number of files to return per section in git status to prevent UI hangs
const maxFilesPerSection = 500

func (ws *ReactWebServer) gitCommandForWorkspace(workspaceRoot string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if strings.TrimSpace(workspaceRoot) != "" {
		cmd.Dir = workspaceRoot
	}
	return cmd
}

func parseNameStatusLine(line string) (status string, path string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}

	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return "", "", false
	}

	statusCode := strings.TrimSpace(parts[0])
	if statusCode == "" {
		return "", "", false
	}

	// For rename/copy entries, Git prints both old and new paths; use the new path.
	filePath := strings.TrimSpace(parts[len(parts)-1])
	if filePath == "" {
		return "", "", false
	}

	return string(statusCode[0]), filePath, true
}

func normalizeGitPath(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	// Always use forward slashes for git paths (git always uses / even on Windows)
	return filepath.ToSlash(cleaned)
}

// makeGitRelativePath converts absolute paths to workspace-relative paths
// for git operations. If the path is already relative or outside the
// workspace, it is returned unchanged.
func makeGitRelativePath(path, workspaceRoot string) string {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(workspaceRoot, path)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return path
}

func pathExistsInGitStatus(path string, status *GitStatus) bool {
	if status == nil {
		return false
	}
	all := getAllGitFiles(status)
	for _, file := range all {
		if normalizeGitPath(file.Path) == path {
			return true
		}
	}
	return false
}

func containsPath(files []GitFile, path string) bool {
	for _, file := range files {
		if normalizeGitPath(file.Path) == path {
			return true
		}
	}
	return false
}

func (ws *ReactWebServer) gitDiffAllowExitOneForWorkspace(workspaceRoot string, args ...string) (string, error) {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), nil
	}

	// `git diff --no-index` can return exit code 1 when differences exist.
	if strings.TrimSpace(string(output)) != "" {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(output), nil
		}
	}
	return "", fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(output)))
}

func truncateDiffOutput(diff string, maxBytes int) string {
	if len(diff) <= maxBytes {
		return diff
	}
	return diff[:maxBytes] + "\n\n... [diff truncated]"
}

func gitOutputStringForWorkspace(ws *ReactWebServer, workspaceRoot string, args ...string) (string, error) {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func generateCryptoID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is extremely rare but if it happens,
		// fall back to a time-based ID rather than returning all zeros.
		log.Printf("[WARN] crypto/rand.Read failed: %v, falling back to time-based ID", err)
		return fmt.Sprintf("%s-%024x", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}
