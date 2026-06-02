//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// handleAPIGitStage handles staging a file
func (ws *ReactWebServer) handleAPIGitStage(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reject requests when not in a git repository.
	if checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir"); checkCmd.Run() != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "not_git_repo",
		})
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

	// Reject requests when not in a git repository.
	if checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir"); checkCmd.Run() != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "not_git_repo",
		})
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

	// Reject requests when not in a git repository.
	if checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir"); checkCmd.Run() != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "not_git_repo",
		})
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

	// "Discard file changes" should leave the file looking like HEAD again —
	// regardless of whether it was modified-only, staged-only, both, deleted,
	// or untracked. The previous `git restore --` covered only the working
	// tree, which silently left staged changes in place and did nothing at
	// all for untracked files (the button looked dead from the UI's
	// perspective). Pick the right git operation per status:
	switch {
	case containsPath(status.Untracked, req.Path):
		// Untracked: nothing to restore from HEAD, so just delete the file.
		// This matches what `git clean -f -- <path>` would do.
		abs := filepath.Join(workspaceRoot, req.Path)
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("Failed to delete untracked file: %v", err), http.StatusInternalServerError)
			return
		}
	default:
		// Modified / staged / deleted / renamed: restore both index and
		// worktree back to HEAD so partial-stage cases also revert fully.
		cmd := ws.gitCommandForWorkspace(workspaceRoot, "restore", "--staged", "--worktree", "--", req.Path)
		if err := cmd.Run(); err != nil {
			http.Error(w, fmt.Sprintf("Failed to discard changes: %v", err), http.StatusInternalServerError)
			return
		}
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

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Reject requests when not in a git repository.
	if checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir"); checkCmd.Run() != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "not_git_repo",
		})
		return
	}

	// Stage all changes
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "add", "-A")
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

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Reject requests when not in a git repository.
	if checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir"); checkCmd.Run() != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "not_git_repo",
		})
		return
	}

	// Unstage all changes
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "reset", "HEAD")
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
