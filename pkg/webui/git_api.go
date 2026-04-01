// Package webui provides git operation handlers
package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/factory"
	gitops "github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/utils"
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

// handleAPIGitStatus handles git status requests
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := ws.getGitStatusForWorkspace(ws.getWorkspaceRootForRequest(r))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get git status: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"status":  status,
		"files":   getAllGitFiles(status), // Backward compatibility
	})
}

// handleAPIGitDiff handles git diff requests for a specific file
func (ws *ReactWebServer) handleAPIGitDiff(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reqPath := normalizeGitPath(r.URL.Query().Get("path"))
	if reqPath == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	reqPath = makeGitRelativePath(reqPath, workspaceRoot)

	status, err := ws.getGitStatusForWorkspace(workspaceRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(reqPath, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	stagedDiff, err := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--cached", "--", reqPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged diff: %v", err), http.StatusInternalServerError)
		return
	}

	unstagedDiff, err := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--", reqPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get unstaged diff: %v", err), http.StatusInternalServerError)
		return
	}

	// For untracked files, generate a synthetic diff against /dev/null.
	if strings.TrimSpace(stagedDiff) == "" && strings.TrimSpace(unstagedDiff) == "" && containsPath(status.Untracked, reqPath) {
		untrackedDiff, untrackedErr := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--no-index", "--", "/dev/null", reqPath)
		if untrackedErr == nil {
			unstagedDiff = untrackedDiff
		}
	}

	const maxDiffBytes = 200000
	stagedDiff = truncateDiffOutput(stagedDiff, maxDiffBytes)
	unstagedDiff = truncateDiffOutput(unstagedDiff, maxDiffBytes)

	var combined strings.Builder
	if strings.TrimSpace(stagedDiff) != "" {
		combined.WriteString("### Staged changes\n")
		combined.WriteString(stagedDiff)
		if !strings.HasSuffix(stagedDiff, "\n") {
			combined.WriteString("\n")
		}
	}
	if strings.TrimSpace(unstagedDiff) != "" {
		if combined.Len() > 0 {
			combined.WriteString("\n")
		}
		combined.WriteString("### Unstaged changes\n")
		combined.WriteString(unstagedDiff)
		if !strings.HasSuffix(unstagedDiff, "\n") {
			combined.WriteString("\n")
		}
	}
	if combined.Len() == 0 {
		combined.WriteString("No diff available for this file.")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "success",
		"path":          reqPath,
		"has_staged":    strings.TrimSpace(stagedDiff) != "",
		"has_unstaged":  strings.TrimSpace(unstagedDiff) != "",
		"staged_diff":   stagedDiff,
		"unstaged_diff": unstagedDiff,
		"diff":          combined.String(),
	})
}

// handleAPIGitStage handles staging a file
func (ws *ReactWebServer) handleAPIGitStage(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}
	req.Path = normalizeGitPath(req.Path)
	if req.Path == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	req.Path = makeGitRelativePath(req.Path, workspaceRoot)

	status, err := ws.getGitStatusForWorkspace(workspaceRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Stage the file
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "add", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stage file: %v", err), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_stage", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "File staged successfully",
		"path":    req.Path,
	})
}

// handleAPIGitUnstage handles unstaging a file
func (ws *ReactWebServer) handleAPIGitUnstage(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}
	req.Path = normalizeGitPath(req.Path)
	if req.Path == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	req.Path = makeGitRelativePath(req.Path, workspaceRoot)

	status, err := ws.getGitStatusForWorkspace(workspaceRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Unstage the file
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "reset", "HEAD", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unstage file: %v", err), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_unstage", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "File unstaged successfully",
		"path":    req.Path,
	})
}

// handleAPIGitDiscard handles discarding changes to a file
func (ws *ReactWebServer) handleAPIGitDiscard(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}
	req.Path = normalizeGitPath(req.Path)
	if req.Path == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Convert absolute paths to workspace-relative for git operations.
	req.Path = makeGitRelativePath(req.Path, workspaceRoot)

	status, err := ws.getGitStatusForWorkspace(workspaceRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Discard changes / restore deleted file — use git restore which works for
	// both modified and deleted working-tree files.
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "restore", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to discard changes: %v", err), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_discard", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Changes discarded successfully",
		"path":    req.Path,
	})
}

// handleAPIGitStageAll handles staging all changes
func (ws *ReactWebServer) handleAPIGitStageAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Stage all changes
	cmd := ws.gitCommandForWorkspace(ws.getWorkspaceRootForRequest(r), "add", "-A")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stage all: %v", err), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_stage_all", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "All changes staged successfully",
	})
}

// handleAPIGitUnstageAll handles unstaging all changes
func (ws *ReactWebServer) handleAPIGitUnstageAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Unstage all changes
	cmd := ws.gitCommandForWorkspace(ws.getWorkspaceRootForRequest(r), "reset", "HEAD")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unstage all: %v", err), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_unstage_all", ""))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "All changes unstaged successfully",
	})
}

// getGitStatus parses git status output
func (ws *ReactWebServer) getGitStatus() (*GitStatus, error) {
	return ws.getGitStatusForWorkspace(ws.workspaceRoot)
}

func (ws *ReactWebServer) getGitStatusForWorkspace(workspaceRoot string) (*GitStatus, error) {
	// Check if we're in a git repository
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		// Not in a git repository
		return &GitStatus{
			Branch:    "",
			Staged:    []GitFile{},
			Modified:  []GitFile{},
			Untracked: []GitFile{},
		}, nil
	}

	// Get branch and tracking info
	status := &GitStatus{}

	// Get current branch
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "branch", "--show-current")
	output, err := cmd.Output()
	if err == nil {
		status.Branch = strings.TrimSpace(string(output))
	}

	// Get ahead/behind info
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "rev-list", "--count", "--left-right", "@{u}...HEAD")
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
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "diff", "--name-status", "--cached")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if statusChar, path, ok := parseNameStatusLine(line); ok {
				status.Staged = append(status.Staged, GitFile{
					Path:   path,
					Status: statusChar,
					Staged: true,
				})
			}
		}
	}

	// Get unstaged changes.
	// Use tab-separated parsing so file names with spaces are preserved.
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "diff", "--name-status")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if statusChar, path, ok := parseNameStatusLine(line); ok {
				if statusChar == "M" {
					status.Modified = append(status.Modified, GitFile{
						Path:   path,
						Status: "M",
						Staged: false,
					})
				} else if statusChar == "D" {
					status.Deleted = append(status.Deleted, GitFile{
						Path:   path,
						Status: "D",
						Staged: false,
					})
				} else if statusChar == "A" {
					status.Modified = append(status.Modified, GitFile{
						Path:   path,
						Status: "A",
						Staged: false,
					})
				} else if statusChar == "R" {
					status.Renamed = append(status.Renamed, GitFile{
						Path:   path,
						Status: "R",
						Staged: false,
					})
				}
			}
		}
	}

	// Get untracked files
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "ls-files", "--others", "--exclude-standard")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			status.Untracked = append(status.Untracked, GitFile{
				Path:   strings.TrimSpace(line),
				Status: "?",
				Staged: false,
			})
		}
	}

	return status, nil
}

func (ws *ReactWebServer) handleAPIGitBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Return an empty success response when the workspace isn't a git repository
	// instead of a 500, to avoid spurious console errors in the client.
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "not_git_repo",
			"current":  "",
			"branches": []string{},
		})
		return
	}

	currentBranch, err := gitOutputStringForWorkspace(ws, workspaceRoot, "branch", "--show-current")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get current branch: %v", err), http.StatusInternalServerError)
		return
	}

	branchesOutput, err := gitOutputStringForWorkspace(ws, workspaceRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list branches: %v", err), http.StatusInternalServerError)
		return
	}

	branches := []string{}
	for _, line := range strings.Split(branchesOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		branches = append(branches, line)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "success",
		"current":  currentBranch,
		"branches": branches,
	})
}

func (ws *ReactWebServer) handleAPIGitCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Branch = strings.TrimSpace(req.Branch)
	if req.Branch == "" {
		http.Error(w, "Branch is required", http.StatusBadRequest)
		return
	}

	if _, err := gitOutputStringForWorkspace(ws, ws.getWorkspaceRootForRequest(r), "checkout", req.Branch); err != nil {
		http.Error(w, fmt.Sprintf("Failed to checkout branch: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Branch checked out successfully",
		"branch":  req.Branch,
	})
}

func (ws *ReactWebServer) handleAPIGitCreateBranch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "Branch name is required", http.StatusBadRequest)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "check-ref-format", "--branch", req.Name)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid branch name: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	}

	if _, err := gitOutputStringForWorkspace(ws, workspaceRoot, "checkout", "-b", req.Name); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create branch: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Branch created successfully",
		"branch":  req.Name,
	})
}

func (ws *ReactWebServer) handleAPIGitPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	output, err := gitOutputStringForWorkspace(ws, ws.getWorkspaceRootForRequest(r), "pull", "--ff-only")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to pull: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Pull completed",
		"output":  output,
	})
}

func (ws *ReactWebServer) handleAPIGitPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	output, err := gitOutputStringForWorkspace(ws, ws.getWorkspaceRootForRequest(r), "push")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to push: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Push completed",
		"output":  output,
	})
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

// handleAPIGitCommit handles git commit with message
func (ws *ReactWebServer) handleAPIGitCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Commit message is required", http.StatusBadRequest)
		return
	}

	// Check if there are staged changes
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		// Exit code 1 means there ARE differences (staged changes)
		// Exit code 0 means no differences
		// We want exit code 1 to proceed
	} else {
		http.Error(w, "No staged changes to commit", http.StatusBadRequest)
		return
	}

	// Create the commit
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "commit", "-m", req.Message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create commit: %v\nOutput: %s", err, string(output)), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_commit", req.Message))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Commit created successfully",
		"commit":  strings.TrimSpace(string(output)),
	})
}

// handleAPIGitCommitMessage generates an AI commit message from currently staged changes
// without creating a commit and without publishing chat/query events.
func (ws *ReactWebServer) handleAPIGitCommitMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		http.Error(w, "Agent is not available", http.StatusServiceUnavailable)
		return
	}

	// Verify staged changes exist (exit code 1 means there are staged changes).
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		http.Error(w, "No staged changes to generate commit message", http.StatusBadRequest)
		return
	}

	diffCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--staged")
	diffOutput, err := diffCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged diff: %v", err), http.StatusInternalServerError)
		return
	}

	diffText := strings.TrimSpace(string(diffOutput))
	if diffText == "" {
		http.Error(w, "No staged changes to generate commit message", http.StatusBadRequest)
		return
	}

	configManager := agentInst.GetConfigManager()
	if configManager == nil {
		http.Error(w, "Agent configuration is unavailable", http.StatusServiceUnavailable)
		return
	}

	clientType, err := configManager.GetProvider()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to resolve provider: %v", err), http.StatusInternalServerError)
		return
	}
	model := configManager.GetModelForProvider(clientType)
	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create provider client: %v", err), http.StatusInternalServerError)
		return
	}

	// Match /commit flow: detect branch and staged file actions.
	branchOutput, err := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get branch name: %v", err), http.StatusInternalServerError)
		return
	}
	branch := strings.TrimSpace(string(branchOutput))

	stagedFilesOutput, err := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--name-status").CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged file status: %v", err), http.StatusInternalServerError)
		return
	}

	fileChanges := make([]gitops.CommitFileChange, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(stagedFilesOutput)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		filePath := strings.Join(parts[1:], " ")
		fileChanges = append(fileChanges, gitops.CommitFileChange{
			Status: status,
			Path:   filePath,
		})
	}
	if len(fileChanges) == 0 {
		http.Error(w, "No staged changes to generate commit message", http.StatusBadRequest)
		return
	}

	result, err := gitops.GenerateCommitMessageFromStagedDiff(client, gitops.CommitMessageOptions{
		Diff:        diffText,
		Branch:      branch,
		FileChanges: fileChanges,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate commit message: %v", err), http.StatusInternalServerError)
		return
	}
	commitMessage := strings.TrimSpace(result.Message)

	if commitMessage == "" {
		http.Error(w, "Generated commit message was empty", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "Commit message generated",
		"commit_message": commitMessage,
		"provider":       agentInst.GetProvider(),
		"model":          agentInst.GetModel(),
		"warnings":       result.Warnings,
	})
}

// handleAPIGitDeepReview performs the same deep staged review flow as /review-deep,
// but without routing through /api/query so it doesn't pollute chat history.
func (ws *ReactWebServer) handleAPIGitDeepReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		http.Error(w, "Agent is not available", http.StatusServiceUnavailable)
		return
	}

	// Exit code 1 means staged changes exist.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		http.Error(w, "No staged changes found", http.StatusBadRequest)
		return
	}

	diffCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached")
	stagedDiffBytes, err := diffCmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged diff: %v", err), http.StatusInternalServerError)
		return
	}

	stagedDiff := string(stagedDiffBytes)
	stagedDiff = truncateDiffOutput(stagedDiff, 200000)
	if strings.TrimSpace(stagedDiff) == "" {
		http.Error(w, "No actual diff content found in staged changes", http.StatusBadRequest)
		return
	}

	cfg, err := configuration.LoadOrInitConfig(true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	logger := utils.GetLogger(true)
	optimizer := utils.NewDiffOptimizerForReview()
	optimizer.WorkingDir = workspaceRoot
	optimizedDiff := optimizer.OptimizeDiff(stagedDiff)

	service := codereview.NewCodeReviewService(cfg, logger)
	agentClient := service.GetDefaultAgentClient()

	activeProvider := strings.TrimSpace(agentInst.GetProvider())
	activeModel := strings.TrimSpace(agentInst.GetModel())
	if activeProvider != "" {
		if sessionClient, err := factory.CreateProviderClient(api.ClientType(activeProvider), activeModel); err == nil {
			agentClient = sessionClient
		}
	}

	if agentClient == nil {
		http.Error(w, "Failed to initialize review client", http.StatusInternalServerError)
		return
	}

	reviewCtx := &codereview.ReviewContext{
		Diff:             optimizedDiff.OptimizedContent,
		Config:           cfg,
		Logger:           logger,
		AgentClient:      agentClient,
		ProjectType:      ws.gitReviewDetectProjectType(workspaceRoot),
		CommitMessage:    ws.gitReviewExtractStagedChangesSummary(workspaceRoot),
		KeyComments:      gitReviewExtractKeyCommentsFromDiff(stagedDiff),
		ChangeCategories: gitReviewCategorizeChanges(stagedDiff),
		FullFileContext:  ws.gitReviewExtractFileContextForChanges(workspaceRoot, stagedDiff),
	}

	if len(optimizedDiff.FileSummaries) > 0 {
		var summaryInfo strings.Builder
		summaryInfo.WriteString("\n\nLarge files optimized for review:\n")
		for file, summary := range optimizedDiff.FileSummaries {
			summaryInfo.WriteString(fmt.Sprintf("- %s: %s\n", file, summary))
		}
		reviewCtx.Diff += summaryInfo.String()
	}

	opts := &codereview.ReviewOptions{
		Type:             codereview.StagedReview,
		SkipPrompt:       true,
		RollbackOnReject: false,
	}

	reviewResponse, err := service.PerformAgenticReview(reviewCtx, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Deep review failed: %v", err), http.StatusInternalServerError)
		return
	}

	reviewOutput := fmt.Sprintf("%s\n%s\n\nStatus: %s\n\nFeedback:\n%s",
		"[list] AI CODE REVIEW (DEEP PASS)",
		strings.Repeat("═", 50),
		strings.ToUpper(reviewResponse.Status),
		reviewResponse.Feedback)

	if strings.TrimSpace(reviewResponse.DetailedGuidance) != "" {
		reviewOutput += fmt.Sprintf("\n\nDetailed Guidance:\n%s", reviewResponse.DetailedGuidance)
	}
	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		reviewOutput += fmt.Sprintf("\n\nSuggested New Prompt:\n%s", reviewResponse.NewPrompt)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":              "Deep review completed",
		"status":               reviewResponse.Status,
		"feedback":             reviewResponse.Feedback,
		"detailed_guidance":    reviewResponse.DetailedGuidance,
		"suggested_new_prompt": reviewResponse.NewPrompt,
		"review_output":        reviewOutput,
		"provider":             agentInst.GetProvider(),
		"model":                agentInst.GetModel(),
		"warnings":             optimizedDiff.Warnings,
	})
}

// handleAPIGitDeepReviewFix runs the fix workflow and blocks until completion (legacy API).
func (ws *ReactWebServer) handleAPIGitDeepReviewFix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		http.Error(w, "review_output is required", http.StatusBadRequest)
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start fix workflow: %v", err), http.StatusInternalServerError)
		return
	}

	for {
		status, _, _, result, jobErr := job.snapshot(0)
		if status == "completed" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":    "Fix workflow completed",
				"result":     strings.TrimSpace(result),
				"session_id": job.SessionID,
			})
			return
		}
		if status == "error" {
			http.Error(w, fmt.Sprintf("Failed to run fix workflow: %s", jobErr), http.StatusInternalServerError)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// handleAPIGitDeepReviewFixStart starts an isolated full-agent fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		http.Error(w, "review_output is required", http.StatusBadRequest)
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start fix workflow: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "Fix workflow started",
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

// handleAPIGitDeepReviewFixStatus returns incremental status/logs for a running fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := strings.TrimSpace(r.URL.Query().Get("job_id"))
	if jobID == "" {
		http.Error(w, "job_id is required", http.StatusBadRequest)
		return
	}

	since := 0
	if rawSince := strings.TrimSpace(r.URL.Query().Get("since")); rawSince != "" {
		_, _ = fmt.Sscanf(rawSince, "%d", &since)
		if since < 0 {
			since = 0
		}
	}

	ws.fixReviewMu.RLock()
	job, ok := ws.fixReviewJobs[jobID]
	ws.fixReviewMu.RUnlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	// Authorization: only the client that started the job can query its status.
	// Jobs with an empty ClientID pre-date access control (backward compat)
	// and are accessible by any client. No new jobs should have empty ClientID.
	requestClientID := ws.resolveClientID(r)
	if job.ClientID != "" && job.ClientID != requestClientID {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	status, logs, next, result, jobErr := job.snapshot(since)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "success",
		"job_id":     job.ID,
		"session_id": job.SessionID,
		"status":     status,
		"logs":       logs,
		"next_index": next,
		"result":     result,
		"error":      jobErr,
	})
}

func (ws *ReactWebServer) startFixReviewJob(reviewOutput, clientID, fixPrompt string, selectedItems []string) (*gitFixReviewJob, string, error) {
	var prompt string
	if len(selectedItems) > 0 {
		selectedSection := strings.Join(selectedItems, "\n\n")
		prompt = fmt.Sprintf("Use these selected review items as input:\n\n%s", selectedSection)
		if strings.TrimSpace(fixPrompt) != "" {
			prompt += fmt.Sprintf("\n\nAdditional instructions from the user:\n%s", fixPrompt)
		}
		prompt += "\n\nFirst validate that each of these selected review items is a valid issue, then use subagents to address the valid ones. When resolved, use a code review subagent to review the solution and iterate until the issues are resolved."
	} else {
		fixInstructions := "First validate that all of these review items are valid issues, then use subagents to address any of the valid issues. When they are resolved, use a code review subagent to review the solution and fix any issues that come out of it and iterate through the process until the issues are resolved."
		prompt = fmt.Sprintf("Use this deep review output as input:\n\n%s\n\n%s", reviewOutput, fixInstructions)
		if strings.TrimSpace(fixPrompt) != "" {
			prompt += fmt.Sprintf("\n\nAdditional instructions from the user:\n%s", fixPrompt)
		}
	}

	provider := ""
	model := ""
	workspaceRoot := ""
	if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
		provider = strings.TrimSpace(agentInst.GetProvider())
		model = strings.TrimSpace(agentInst.GetModel())
		workspaceRoot = agentInst.GetWorkspaceRoot()
	}
	// Fallback: if getClientAgent failed or returned empty workspace root,
	// resolve from the client context directly.
	if strings.TrimSpace(workspaceRoot) == "" {
		if clientCtx := ws.getOrCreateClientContext(clientID); clientCtx != nil {
			workspaceRoot = clientCtx.WorkspaceRoot
		}
	}

	jobID := generateCryptoID("rfx")
	sessionID := generateCryptoID("rfxs")
	job := &gitFixReviewJob{
		ID:            jobID,
		SessionID:     sessionID,
		ClientID:      clientID,
		WorkspaceRoot: workspaceRoot,
		Status:        "running",
		Logs:          []string{"Starting isolated fix session..."},
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	ws.fixReviewMu.Lock()
	ws.fixReviewJobs[jobID] = job
	ws.fixReviewMu.Unlock()

	go ws.runFixReviewJob(job, prompt, provider, model, workspaceRoot)

	return job, prompt, nil
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

func (ws *ReactWebServer) runFixReviewJob(job *gitFixReviewJob, prompt, provider, model, workspaceRoot string) {
	// When running in daemon mode, the process CWD is the daemon's directory, not the
	// client's workspace. Many downstream code paths (agent init, SaveState, context
	// discovery, git operations) rely on os.Getwd(), so we must switch to the correct
	// workspace directory before creating the agent.
	// Use withAgentWorkspace to serialize CWD changes via workspaceExecMu, avoiding
	// races with other goroutines that also change CWD (e.g. withAgentWorkspace calls
	// from getClientAgent / regular chat).
	workspaceRoot = strings.TrimSpace(workspaceRoot)

	if workspaceRoot == "" {
		job.setError("No workspace root resolved; cannot run fix review in daemon mode. " +
			"Ensure the browser has set a workspace before triggering fix review.")
		return
	}

	err := ws.withAgentWorkspace(workspaceRoot, func() error {
		job.appendLog(fmt.Sprintf("Changed CWD to workspace: %s", workspaceRoot))

		reviewAgent, agentErr := agent.NewAgentWithModel("")
		if agentErr != nil {
			job.setError(fmt.Sprintf("Failed to initialize isolated agent: %v", agentErr))
			return agentErr
		}
		defer reviewAgent.Shutdown()

		reviewAgent.SetWorkspaceRoot(workspaceRoot)

		reviewAgent.SetSessionID(job.SessionID)
		if p := strings.TrimSpace(provider); p != "" {
			if err := reviewAgent.SetProvider(api.ClientType(p)); err != nil {
				job.appendLog(fmt.Sprintf("Warning: failed to set provider %s: %v", p, err))
			}
		}
		if m := strings.TrimSpace(model); m != "" {
			if err := reviewAgent.SetModel(m); err != nil {
				job.appendLog(fmt.Sprintf("Warning: failed to set model %s: %v", m, err))
			}
		}

		reviewAgent.SetStreamingEnabled(true)
		reviewAgent.SetStreamingCallback(func(text string) {
			job.appendStreamText(text)
		})

		job.appendLog("Running fix workflow with full agentic path...")
		result, procErr := reviewAgent.ProcessQuery(prompt)
		job.flushStreamBuffer()
		if procErr != nil {
			job.setError(procErr.Error())
			return procErr
		}
		job.setCompleted(strings.TrimSpace(result))
		return nil
	})

	// If withAgentWorkspace itself failed (e.g. CWD change error) and we haven't
	// already recorded a more specific error via job.setError, record it now.
	if err != nil && job.Status == "running" {
		job.setError(fmt.Sprintf("Workspace setup failed: %v", err))
	}
}

func (j *gitFixReviewJob) appendLog(line string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) appendStreamText(text string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	j.streamBuf.WriteString(text)
	raw := j.streamBuf.String()
	if raw == "" {
		return
	}

	parts := strings.Split(raw, "\n")
	for _, part := range parts[:len(parts)-1] {
		line := strings.TrimSpace(part)
		if line == "" {
			continue
		}
		j.Logs = append(j.Logs, line)
	}

	j.streamBuf.Reset()
	j.streamBuf.WriteString(parts[len(parts)-1])
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) flushStreamBuffer() {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	line := strings.TrimSpace(j.streamBuf.String())
	j.streamBuf.Reset()
	if line == "" {
		return
	}
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) setCompleted(result string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Status = "completed"
	j.Result = result
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) setError(err string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Status = "error"
	j.Error = strings.TrimSpace(err)
	if j.Error == "" {
		j.Error = "Unknown error"
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) snapshot(since int) (status string, logs []string, nextIndex int, result string, err string) {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	total := len(j.Logs)
	if since < 0 {
		since = 0
	}
	if since > total {
		since = total
	}
	chunk := make([]string, total-since)
	copy(chunk, j.Logs[since:])

	return j.Status, chunk, total, j.Result, j.Error
}

func (ws *ReactWebServer) gitReviewDetectProjectType(workspaceRoot string) string {
	projectMarkers := []struct {
		name string
		file string
	}{
		{name: "Go project", file: "go.mod"},
		{name: "Node.js project", file: "package.json"},
		{name: "Python project", file: "requirements.txt"},
		{name: "Python project", file: "setup.py"},
		{name: "Python project", file: "pyproject.toml"},
		{name: "Rust project", file: "Cargo.toml"},
		{name: "Ruby project", file: "Gemfile"},
	}

	for _, marker := range projectMarkers {
		if _, err := os.Stat(filepath.Join(workspaceRoot, marker.file)); err == nil {
			return marker.name
		}
	}
	return ""
}

func (ws *ReactWebServer) gitReviewExtractStagedChangesSummary(workspaceRoot string) string {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && strings.TrimSpace(statLines[0]) != "" {
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}
	return ""
}

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	keyComments := make([]string, 0, 8)
	currentFile := ""

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}

		if strings.HasPrefix(line, "+") && (strings.Contains(line, "//") || strings.Contains(line, "#")) {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "+"))
			if gitReviewIsImportantComment(comment) {
				keyComments = append(keyComments, fmt.Sprintf("- %s: %s", currentFile, comment))
			}
		}
	}

	if len(keyComments) == 0 {
		return ""
	}
	if len(keyComments) > 10 {
		keyComments = keyComments[:10]
	}
	return strings.Join(keyComments, "\n")
}

func gitReviewIsImportantComment(comment string) bool {
	commentUpper := strings.ToUpper(comment)
	keywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}

	for _, keyword := range keywords {
		if strings.Contains(commentUpper, keyword) {
			return true
		}
	}
	return strings.HasPrefix(comment, "//") && len(comment) > 50
}

func gitReviewCategorizeChanges(diff string) string {
	lines := strings.Split(diff, "\n")
	categories := make(map[string]int)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index") {
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedLine := strings.TrimPrefix(line, "+")
			if strings.Contains(strings.ToUpper(addedLine), "SECURITY") ||
				strings.Contains(addedLine, "filesystem.ErrOutsideWorkingDirectory") ||
				strings.Contains(addedLine, "WithSecurityBypass") {
				categories["Security fixes/improvements"]++
			}
			if strings.Contains(addedLine, "error") ||
				strings.Contains(addedLine, "Err") ||
				strings.Contains(addedLine, "return nil") ||
				strings.Contains(addedLine, "if err") {
				categories["Error handling"]++
			}
			if strings.Contains(addedLine, "require(") ||
				strings.Contains(addedLine, "github.com/") ||
				strings.Contains(addedLine, "go.mod") {
				categories["Dependency updates"]++
			}
			if strings.Contains(addedLine, "Test") || strings.Contains(addedLine, "test") {
				categories["Test changes"]++
			}
		}

		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			categories["Code removal/refactoring"]++
		}
	}

	if len(categories) == 0 {
		return ""
	}

	linesOut := make([]string, 0, len(categories))
	for category, count := range categories {
		linesOut = append(linesOut, fmt.Sprintf("- %s (%d changes)", category, count))
	}
	return strings.Join(linesOut, "\n")
}

func (ws *ReactWebServer) gitReviewExtractFileContextForChanges(workspaceRoot, diff string) string {
	lines := strings.Split(diff, "\n")
	changedFiles := make(map[string]bool)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				changedFiles[strings.TrimPrefix(parts[3], "b/")] = true
			}
		}
	}

	contextParts := make([]string, 0, len(changedFiles))
	for relPath := range changedFiles {
		if !ws.gitReviewIsValidRepoFilePath(workspaceRoot, relPath) || gitReviewShouldSkipFileForContext(relPath) {
			continue
		}

		absPath := filepath.Join(workspaceRoot, relPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		fileLines := strings.Split(string(content), "\n")
		maxLines := 500
		if len(fileLines) < maxLines {
			maxLines = len(fileLines)
		}
		if maxLines > 0 {
			contextParts = append(contextParts, fmt.Sprintf("### %s\n```go\n%s\n```", relPath, strings.Join(fileLines[:maxLines], "\n")))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}
	return strings.Join(contextParts, "\n\n")
}

// GitCommit is a single commit entry in the git log response.
type GitCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Date      string `json:"date"`
	Message   string `json:"message"`
	RefNames  string `json:"ref_names,omitempty"`
}

// handleAPIGitLog returns a paginated list of past commits.
// Query params: limit (default 30, max 100), offset (default 0).
func (ws *ReactWebServer) handleAPIGitLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Verify git repo
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		http.Error(w, "Not a git repository", http.StatusBadRequest)
		return
	}

	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		fmt.Sscanf(raw, "%d", &limit)
		if limit <= 0 {
			limit = 30
		}
		if limit > 100 {
			limit = 100
		}
	}

	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		fmt.Sscanf(raw, "%d", &offset)
		if offset < 0 {
			offset = 0
		}
	}

	// Use a custom format to parse structured commit data.
	// Format: hash<NUL>author<NUL>date<NUL>ref_names<NUL>message
	// %H=full hash, %an=author name, %aI=author date (strict ISO 8601),
	// %D=ref names, %s=subject line.
	format := "%H%x00%an%x00%aI%x00%D%x00%s"
	args := []string{
		"log",
		fmt.Sprintf("--skip=%d", offset),
		fmt.Sprintf("-n%d", limit),
		fmt.Sprintf("--format=%s", format),
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get git log: %v", err), http.StatusInternalServerError)
		return
	}

	commits := make([]GitCommit, 0, limit)
	for _, block := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "\x00", 5)
		if len(parts) < 5 {
			continue
		}

		hash := parts[0]
		shortHash := hash
		if len(hash) > 7 {
			shortHash = hash[:7]
		}

		commits = append(commits, GitCommit{
			Hash:      hash,
			ShortHash: shortHash,
			Author:    parts[1],
			Date:      parts[2],
			Message:   parts[4],
			RefNames:  strings.TrimSpace(parts[3]),
		})
	}

	// Get total commit count for pagination — only needed when we received a full page.
	totalCount := 0
	if len(commits) == limit {
		countCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-list", "--count", "HEAD")
		countOutput, countErr := countCmd.Output()
		if countErr == nil {
			fmt.Sscanf(strings.TrimSpace(string(countOutput)), "%d", &totalCount)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"commits": commits,
		"offset":  offset,
		"limit":   limit,
		"total":   totalCount,
	})
}

// handleAPIGitCommitShow returns the full diff and metadata for a single commit.
func (ws *ReactWebServer) handleAPIGitCommitShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	// Validate that the hash refers to an actual commit.
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "cat-file", "-t", hash)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid commit hash: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	} else if strings.TrimSpace(string(output)) != "commit" {
		http.Error(w, "hash does not refer to a commit", http.StatusBadRequest)
		return
	}

	// Get commit metadata.
	format := "%H%x00%an%x00%aI%x00%D%x00%s"
	metaCmd := ws.gitCommandForWorkspace(workspaceRoot, "log", "-1", fmt.Sprintf("--format=%s", format), hash)
	metaOutput, err := metaCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit metadata: %v", err), http.StatusInternalServerError)
		return
	}
	metaStr := strings.TrimSpace(string(metaOutput))
	metaParts := strings.SplitN(metaStr, "\x00", 5)
	if len(metaParts) < 5 {
		http.Error(w, "Failed to parse commit metadata", http.StatusInternalServerError)
		return
	}

	fullHash := metaParts[0]
	shortHash := fullHash
	if len(fullHash) > 7 {
		shortHash = fullHash[:7]
	}

	// Get the diff.
	showCmd := ws.gitCommandForWorkspace(workspaceRoot, "show", "--format=", "--patch", hash)
	showOutput, err := showCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit diff: %v", err), http.StatusInternalServerError)
		return
	}
	diff := string(showOutput)
	diff = truncateDiffOutput(diff, 500000)

	// Get file list with name-status.
	nameStatusCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff-tree", "--no-commit-id", "--name-status", "-r", hash)
	nameStatusOutput, err := nameStatusCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get commit files: %v", err), http.StatusInternalServerError)
		return
	}

	files := make([]GitFile, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(nameStatusOutput)), "\n") {
		if statusChar, path, ok := parseNameStatusLine(line); ok {
			files = append(files, GitFile{
				Path:   path,
				Status: statusChar,
			})
		}
	}

	// Get stat summary.
	statCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff-tree", "--no-commit-id", "--stat", "-r", hash)
	statOutput, err := statCmd.CombinedOutput()
	stats := ""
	if err == nil {
		stats = strings.TrimSpace(string(statOutput))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "success",
		"hash":      fullHash,
		"short_hash": shortHash,
		"author":    metaParts[1],
		"date":      metaParts[2],
		"ref_names": strings.TrimSpace(metaParts[3]),
		"subject":   metaParts[4],
		"files":      files,
		"diff":       diff,
		"stats":      stats,
	})
}

func gitReviewShouldSkipFileForContext(filePath string) bool {
	if utils.ClassifyReviewFile(filePath).SkipForReview {
		return true
	}

	if strings.HasSuffix(filePath, ".sum") ||
		strings.HasSuffix(filePath, ".lock") ||
		strings.HasSuffix(filePath, "package-lock.json") ||
		strings.HasSuffix(filePath, "yarn.lock") {
		return true
	}
	if strings.Contains(filePath, ".min.") ||
		strings.HasSuffix(filePath, ".map") ||
		strings.Contains(filePath, "node_modules/") {
		return true
	}
	if strings.HasSuffix(filePath, ".pb.go") ||
		strings.Contains(filePath, "_generated.go") ||
		strings.Contains(filePath, "_generated.") {
		return true
	}
	if strings.HasSuffix(filePath, "coverage.out") ||
		strings.HasSuffix(filePath, "coverage.html") ||
		strings.HasSuffix(filePath, ".test") ||
		strings.HasSuffix(filePath, ".out") {
		return true
	}
	if strings.HasSuffix(filePath, ".svg") ||
		strings.HasSuffix(filePath, ".png") ||
		strings.HasSuffix(filePath, ".jpg") ||
		strings.HasSuffix(filePath, ".ico") {
		return true
	}
	return strings.Contains(filePath, "vendor/") || strings.Contains(filePath, ".git/")
}

func (ws *ReactWebServer) gitReviewIsValidRepoFilePath(workspaceRoot, relPath string) bool {
	if strings.Contains(relPath, "..") {
		return false
	}

	cleanRel := filepath.Clean(relPath)
	absPath, err := filepath.Abs(filepath.Join(workspaceRoot, cleanRel))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) || absPath == absRoot
}
