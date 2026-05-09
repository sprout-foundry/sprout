package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const maxDiffBytes = 200000

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

	// Return empty diffs gracefully when not in a git repository.
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":       "success",
			"path":          reqPath,
			"has_staged":    false,
			"has_unstaged":  false,
			"staged_diff":   "",
			"unstaged_diff": "",
			"diff":          "No diff available for this file.",
		})
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
	if strings.TrimSpace(stagedDiff) == "" && strings.TrimSpace(unstagedDiff) == "" {
		absPath := reqPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workspaceRoot, absPath)
		}
		// Only try the synthetic diff if the file actually exists on disk
		// (it may be clean/committed, in which case we just return empty diffs).
		if _, statErr := os.Stat(absPath); statErr == nil {
			// Check if the file is tracked by git. If it is, skip the synthetic diff.
			cmd := ws.gitCommandForWorkspace(workspaceRoot, "ls-files", "--error-unmatch", "--", reqPath)
			if cmd.Run() != nil {
				// File is not tracked, so it's untracked - generate synthetic diff
				untrackedDiff, untrackedErr := ws.gitDiffAllowExitOneForWorkspace(workspaceRoot, "diff", "--no-index", "--", "/dev/null", reqPath)
				if untrackedErr == nil {
					unstagedDiff = untrackedDiff
				}
			}
			// If the file IS tracked, we leave diffs empty (file is clean)
		}
	}

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
