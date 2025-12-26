// Package webui provides git operation handlers
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
)

// GitStatus represents the git status response
type GitStatus struct {
	Branch   string     `json:"branch"`
	Ahead    int        `json:"ahead"`
	Behind   int        `json:"behind"`
	Staged   []GitFile  `json:"staged"`
	Modified []GitFile  `json:"modified"`
	Untracked []GitFile `json:"untracked"`
	Deleted  []GitFile  `json:"deleted"`
	Renamed  []GitFile  `json:"renamed"`
}

// GitFile represents a file with its git status
type GitFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged,omitempty"`
}

// handleAPIGitStatus handles git status requests
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := getGitStatus()
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

	// Stage the file
	cmd := exec.Command("git", "add", req.Path)
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

	// Unstage the file
	cmd := exec.Command("git", "reset", "HEAD", "--", req.Path)
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

	// Discard changes (checkout from HEAD)
	cmd := exec.Command("git", "checkout", "--", req.Path)
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
	cmd := exec.Command("git", "add", "-A")
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
	cmd := exec.Command("git", "reset", "HEAD")
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
func getGitStatus() (*GitStatus, error) {
	// Check if we're in a git repository
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		// Not in a git repository
		return &GitStatus{
			Branch:   "",
			Staged:   []GitFile{},
			Modified: []GitFile{},
			Untracked: []GitFile{},
		}, nil
	}

	// Get branch and tracking info
	status := &GitStatus{}

	// Get current branch
	cmd = exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()
	if err == nil {
		status.Branch = strings.TrimSpace(string(output))
	}

	// Get ahead/behind info
	cmd = exec.Command("git", "rev-list", "--count", "--left-right", "@{u}...HEAD")
	output, err = cmd.Output()
	if err == nil {
		parts := strings.Fields(string(output))
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &status.Behind)
			fmt.Sscanf(parts[1], "%d", &status.Ahead)
		}
	}

	// Get staged changes
	cmd = exec.Command("git", "diff", "--name-status", "--cached")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				status.Staged = append(status.Staged, GitFile{
					Path:   strings.TrimSpace(parts[1]),
					Status: string(parts[0][0]),
					Staged: true,
				})
			}
		}
	}

	// Get unstaged changes
	cmd = exec.Command("git", "diff", "--name-status")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				code := string(parts[0])
				statusChar := code[0]
				if statusChar == 'M' {
					status.Modified = append(status.Modified, GitFile{
						Path:   strings.TrimSpace(parts[1]),
						Status: "M",
						Staged: false,
					})
				} else if statusChar == 'D' {
					status.Deleted = append(status.Deleted, GitFile{
						Path:   strings.TrimSpace(parts[1]),
						Status: "D",
						Staged: false,
					})
				} else if statusChar == 'A' {
					status.Modified = append(status.Modified, GitFile{
						Path:   strings.TrimSpace(parts[1]),
						Status: "A",
						Staged: false,
					})
				} else if statusChar == 'R' {
					status.Renamed = append(status.Renamed, GitFile{
						Path:   strings.TrimSpace(parts[1]),
						Status: "R",
						Staged: false,
					})
				}
			}
		}
	}

	// Get untracked files
	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
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
		Message string `json:"message"`
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
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		// Exit code 1 means there ARE differences (staged changes)
		// Exit code 0 means no differences
		// We want exit code 1 to proceed
	} else {
		http.Error(w, "No staged changes to commit", http.StatusBadRequest)
		return
	}

	// Create the commit
	cmd = exec.Command("git", "commit", "-m", req.Message)
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
