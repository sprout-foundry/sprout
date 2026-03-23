// Package webui provides git operation handlers
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
)

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
	cmd := exec.Command("git", args...)
	if strings.TrimSpace(ws.workspaceRoot) != "" {
		cmd.Dir = ws.workspaceRoot
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
	cmd := ws.gitCommand(args...)
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

// handleAPIGitStatus handles git status requests
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := ws.getGitStatus()
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reqPath := normalizeGitPath(r.URL.Query().Get("path"))
	if reqPath == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	status, err := ws.getGitStatus()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(reqPath, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	stagedDiff, err := ws.gitDiffAllowExitOne("diff", "--cached", "--", reqPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged diff: %v", err), http.StatusInternalServerError)
		return
	}

	unstagedDiff, err := ws.gitDiffAllowExitOne("diff", "--", reqPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get unstaged diff: %v", err), http.StatusInternalServerError)
		return
	}

	// For untracked files, generate a synthetic diff against /dev/null.
	if strings.TrimSpace(stagedDiff) == "" && strings.TrimSpace(unstagedDiff) == "" && containsPath(status.Untracked, reqPath) {
		untrackedDiff, untrackedErr := ws.gitDiffAllowExitOne("diff", "--no-index", "--", "/dev/null", reqPath)
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

	status, err := ws.getGitStatus()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Stage the file
	cmd := ws.gitCommand("add", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stage file: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_stage", ""))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "File staged successfully",
		"path":    req.Path,
	})
}

// handleAPIGitUnstage handles unstaging a file
func (ws *ReactWebServer) handleAPIGitUnstage(w http.ResponseWriter, r *http.Request) {
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

	status, err := ws.getGitStatus()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Unstage the file
	cmd := ws.gitCommand("reset", "HEAD", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unstage file: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_unstage", ""))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "File unstaged successfully",
		"path":    req.Path,
	})
}

// handleAPIGitDiscard handles discarding changes to a file
func (ws *ReactWebServer) handleAPIGitDiscard(w http.ResponseWriter, r *http.Request) {
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

	status, err := ws.getGitStatus()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to validate path: %v", err), http.StatusInternalServerError)
		return
	}
	if !pathExistsInGitStatus(req.Path, status) {
		http.Error(w, "File is not part of git changes", http.StatusBadRequest)
		return
	}

	// Discard changes (checkout from HEAD)
	cmd := ws.gitCommand("checkout", "--", req.Path)
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to discard changes: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(req.Path, "git_discard", ""))
	}

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
	cmd := ws.gitCommand("add", "-A")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stage all: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent("", "git_stage_all", ""))
	}

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
	cmd := ws.gitCommand("reset", "HEAD")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unstage all: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent("", "git_unstage_all", ""))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "All changes unstaged successfully",
	})
}

// getGitStatus parses git status output
func (ws *ReactWebServer) getGitStatus() (*GitStatus, error) {
	// Check if we're in a git repository
	cmd := ws.gitCommand("rev-parse", "--git-dir")
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
	cmd = ws.gitCommand("branch", "--show-current")
	output, err := cmd.Output()
	if err == nil {
		status.Branch = strings.TrimSpace(string(output))
	}

	// Get ahead/behind info
	cmd = ws.gitCommand("rev-list", "--count", "--left-right", "@{u}...HEAD")
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
	cmd = ws.gitCommand("diff", "--name-status", "--cached")
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
	cmd = ws.gitCommand("diff", "--name-status")
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
	cmd = ws.gitCommand("ls-files", "--others", "--exclude-standard")
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
	cmd := ws.gitCommand("diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		// Exit code 1 means there ARE differences (staged changes)
		// Exit code 0 means no differences
		// We want exit code 1 to proceed
	} else {
		http.Error(w, "No staged changes to commit", http.StatusBadRequest)
		return
	}

	// Create the commit
	cmd = ws.gitCommand("commit", "-m", req.Message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create commit: %v\nOutput: %s", err, string(output)), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent("", "git_commit", req.Message))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Commit created successfully",
		"commit":  strings.TrimSpace(string(output)),
	})
}
