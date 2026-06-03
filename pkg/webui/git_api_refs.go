//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

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
		"success": true,
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
	var branch string
	branchOutput, err := ws.gitCommandForWorkspace(workspaceRoot, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		// Initial commit: HEAD doesn't exist yet, use empty branch.
		branch = ""
	} else {
		branch = strings.TrimSpace(string(branchOutput))
	}

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

func (ws *ReactWebServer) handleAPIGitRevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.Commit = strings.TrimSpace(req.Commit)
	if req.Commit == "" {
		http.Error(w, "Commit is required", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(req.Commit, "-") {
		http.Error(w, "Invalid commit hash", http.StatusBadRequest)
		return
	}
	if _, err := gitOutputStringForWorkspace(ws, ws.getWorkspaceRootForRequest(r), "revert", "--no-edit", req.Commit); err != nil {
		http.Error(w, fmt.Sprintf("Failed to revert commit: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Commit reverted successfully",
		"commit":  req.Commit,
	})
}
