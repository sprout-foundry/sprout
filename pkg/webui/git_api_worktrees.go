package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
)

// WorktreeInfo contains information about a git worktree
type WorktreeInfo struct {
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	IsMain      bool   `json:"is_main"`
	IsCurrent   bool   `json:"is_current"`
	ParentPath  string `json:"parent_path,omitempty"`
	ParentBranch string `json:"parent_branch,omitempty"`
}

// handleAPIGitWorktrees handles git worktree listing requests
func (ws *ReactWebServer) handleAPIGitWorktrees(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Return an empty success response when the workspace isn't a git repository
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "not_git_repo",
			"worktrees":  []WorktreeInfo{},
			"current":    "",
		})
		return
	}

	// Get current branch
	currentBranch, err := gitOutputStringForWorkspace(ws, workspaceRoot, "branch", "--show-current")
	if err != nil {
		currentBranch = ""
	}

	// Get all worktrees using git worktree list
	// Format: worktree path | branch branch-name (HEAD detached at abc123...)
	worktreesOutput, err := gitOutputStringForWorkspace(ws, workspaceRoot, "worktree", "list", "--porcelain")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list worktrees: %v", err), http.StatusInternalServerError)
		return
	}

	worktrees := ws.parseWorktreeListOutput(worktreesOutput, strings.TrimSpace(currentBranch), workspaceRoot)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "success",
		"worktrees": worktrees,
		"current":   currentBranch,
	})
}

// parseWorktreeListOutput parses the output of `git worktree list --porcelain`
// Output format:
// worktree /path/to/worktree
// HEAD abc123def456...
// branch refs/heads/feature-xyz
// root /path/to/repo/.git
func (ws *ReactWebServer) parseWorktreeListOutput(output, currentBranch, workspaceRoot string) []WorktreeInfo {
	if output == "" {
		return []WorktreeInfo{}
	}

	lines := strings.Split(output, "\n")
	var worktrees []WorktreeInfo
	var currentWorktree WorktreeInfo

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			if !currentWorktree.IsZero() {
				worktrees = append(worktrees, currentWorktree)
				currentWorktree = WorktreeInfo{}
			}
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "worktree":
			if !currentWorktree.IsZero() {
				worktrees = append(worktrees, currentWorktree)
			}
			currentWorktree = WorktreeInfo{Path: value}
		case "HEAD":
			// HEAD <hash> (detached) or HEAD <hash>
			// We don't need the hash for our purposes
		case "branch":
			// branch refs/heads/<branch-name>
			if strings.HasPrefix(value, "refs/heads/") {
				currentWorktree.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		case "root":
			// root <path-to-git-dir>
			// This is the .git directory of the main worktree
			currentWorktree.ParentPath = filepath.Dir(value)
		}
	}

	// Don't forget the last worktree
	if !currentWorktree.IsZero() {
		worktrees = append(worktrees, currentWorktree)
	}

	// Mark the current worktree and determine main worktree
	var mainWorktree *WorktreeInfo
	for i := range worktrees {
		wt := &worktrees[i]
		if wt.Path == workspaceRoot {
			wt.IsCurrent = true
			mainWorktree = wt
		}
	}

	// Mark the main worktree
	if mainWorktree != nil {
		mainWorktree.IsMain = true
	}

	// Set parent info for all worktrees
	if mainWorktree != nil {
		for i := range worktrees {
			wt := &worktrees[i]
			if wt.Path != workspaceRoot {
				wt.ParentPath = mainWorktree.Path
				wt.ParentBranch = mainWorktree.Branch
			}
		}
	}

	return worktrees
}

// handleAPIGitWorktreeCreate handles creating a new worktree
func (ws *ReactWebServer) handleAPIGitWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		BaseRef  string `json:"base_ref,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	req.Branch = strings.TrimSpace(req.Branch)

	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}
	if req.Branch == "" {
		http.Error(w, "Branch name is required", http.StatusBadRequest)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Resolve path to absolute
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
		return
	}

	// Validate the resolved worktree path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
		http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
		return
	}

	// Validate branch name
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "check-ref-format", "--branch", req.Branch)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid branch name: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	}

	// Build the git worktree add command
	args := []string{"worktree", "add"}
	if req.BaseRef != "" {
		args = append(args, "-b", req.Branch, absPath, req.BaseRef)
	} else {
		args = append(args, "-b", req.Branch, absPath)
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create worktree: %v\nOutput: %s", err, string(output)), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_worktree_create", absPath))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Worktree created successfully",
		"path":     absPath,
		"branch":   req.Branch,
		"output":   strings.TrimSpace(string(output)),
	})
}

// handleAPIGitWorktreeRemove handles removing an existing worktree
func (ws *ReactWebServer) handleAPIGitWorktreeRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	// Resolve path to absolute
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Prevent removing the current worktree
	if absPath == workspaceRoot {
		http.Error(w, "Cannot remove the current worktree", http.StatusBadRequest)
		return
	}

	// Validate the resolved path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
		http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
		return
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "remove", absPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to remove worktree: %v\nOutput: %s", err, string(output)), http.StatusInternalServerError)
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_worktree_remove", absPath))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Worktree removed successfully",
		"path":     absPath,
		"output":   strings.TrimSpace(string(output)),
	})
}

// handleAPIGitWorktreeCheckout handles switching to a different worktree
func (ws *ReactWebServer) handleAPIGitWorktreeCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	// Resolve path to absolute
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Check if the path exists and is a valid worktree
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "list", "--porcelain")
	checkOutput, err := checkCmd.CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list worktrees: %v", err), http.StatusInternalServerError)
		return
	}

	worktreeExists := false
	for _, line := range strings.Split(string(checkOutput), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			if path == absPath {
				worktreeExists = true
				break
			}
		}
	}

	if !worktreeExists {
		http.Error(w, "Worktree not found", http.StatusBadRequest)
		return
	}

	// Validate the resolved path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
		http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
		return
	}

	// Switch workspace root directly — do NOT call setClientWorkspaceRoot
	// because it nukes all chat sessions. We preserve chat sessions but
	// clear transient state (agent, terminals) like setClientWorkspaceRoot does.
	clientID := ws.resolveClientID(r)
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.WorkspaceRoot = absPath
	if clientID == defaultWebClientID {
		ws.workspaceRoot = absPath
	}
	// Clear transient state like setClientWorkspaceRoot does
	ctx.Agent = nil
	ctx.Terminal = nil
	ws.mutex.Unlock()

	// Publish event to notify frontend of workspace change
	ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"workspace_root":          absPath,
		"previous_workspace_root": workspaceRoot,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "Switched to worktree successfully",
		"path":      absPath,
		"workspace": absPath,
	})
}

// IsZero checks if WorktreeInfo is zero value (for filtering)
func (wt WorktreeInfo) IsZero() bool {
	return wt.Path == ""
}
