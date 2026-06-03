//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

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
			"message":         "not_git_repo",
			"current":         "",
			"branches":        []string{},
			"local_branches":  []string{},
			"remote_branches": []string{},
		})
		return
	}

	currentBranch, err := gitOutputStringForWorkspace(ws, workspaceRoot, "branch", "--show-current")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get current branch: %v", err), http.StatusInternalServerError)
		return
	}

	// Query local branches from refs/heads
	localBranchesOutput, err := gitOutputStringForWorkspace(ws, workspaceRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list local branches: %v", err), http.StatusInternalServerError)
		return
	}

	// Query remote branches from refs/remotes
	remoteBranchesOutput, err := gitOutputStringForWorkspace(ws, workspaceRoot, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list remote branches: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse local branches
	localBranches := []string{}
	for _, line := range strings.Split(localBranchesOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		localBranches = append(localBranches, line)
	}

	// Parse remote branches, skipping HEAD symrefs
	remoteBranches := []string{}
	for _, line := range strings.Split(remoteBranchesOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip HEAD symrefs like origin/HEAD
		if strings.HasSuffix(line, "/HEAD") {
			continue
		}
		remoteBranches = append(remoteBranches, line)
	}

	// Combine branches: local first, then remote
	allBranches := append([]string{}, localBranches...)
	allBranches = append(allBranches, remoteBranches...)

	// Sort alphabetically
	sort.Strings(allBranches)

	// Move current branch to the top if it exists
	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch != "" {
		for i, branch := range allBranches {
			if branch == currentBranch {
				// Remove from current position and insert at beginning
				allBranches = append(allBranches[:i], allBranches[i+1:]...)
				allBranches = append([]string{currentBranch}, allBranches...)
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":         "success",
		"current":         currentBranch,
		"branches":        allBranches,
		"local_branches":  localBranches,
		"remote_branches": remoteBranches,
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
	if strings.HasPrefix(req.Branch, "-") {
		http.Error(w, "Invalid branch name", http.StatusBadRequest)
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
		"success": true,
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
