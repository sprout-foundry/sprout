package webui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
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
}

// GitFile represents a file with its git status
type GitFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged,omitempty"`
}

func (ws *ReactWebServer) gitCommand(args ...string) *exec.Cmd {
	return ws.gitCommandForWorkspace(ws.workspaceRoot, args...)
}

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
	return cleaned
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

func (ws *ReactWebServer) gitDiffAllowExitOne(args ...string) (string, error) {
	return ws.gitDiffAllowExitOneForWorkspace(ws.workspaceRoot, args...)
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
	return "", err
}

func truncateDiffOutput(diff string, maxBytes int) string {
	if len(diff) <= maxBytes {
		return diff
	}
	return diff[:maxBytes] + "\n\n... [diff truncated]"
}

func gitOutputString(ws *ReactWebServer, args ...string) (string, error) {
	return gitOutputStringForWorkspace(ws, ws.workspaceRoot, args...)
}

func gitOutputStringForWorkspace(ws *ReactWebServer, workspaceRoot string, args ...string) (string, error) {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
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
