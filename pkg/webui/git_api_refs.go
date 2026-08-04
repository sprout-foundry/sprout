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
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.Message == "" {
		writeJSONErr(w, http.StatusBadRequest, "commit_message_required", "Commit message is required")
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
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes to commit")
		return
	}

	// Create the commit
	cmd = ws.gitCommandForWorkspace(workspaceRoot, "commit", "-m", req.Message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_commit", fmt.Sprintf("Failed to create commit: %v\nOutput: %s", err, string(output)))
		return
	}

	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent("", "git_commit", req.Message))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Commit created successfully",
		"commit":  strings.TrimSpace(string(output)),
	})
}

// handleAPIGitCommitMessage generates an AI commit message from currently staged changes
// without creating a commit and without publishing chat/query events.
func (ws *ReactWebServer) handleAPIGitCommitMessage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_available", "Agent is not available")
		return
	}

	// Verify staged changes exist (exit code 1 means there are staged changes).
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes to generate commit message")
		return
	}

	diffCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--staged")
	diffOutput, err := diffCmd.CombinedOutput()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_get_staged_diff", fmt.Sprintf("Failed to get staged diff: %v", err))
		return
	}

	diffText := strings.TrimSpace(string(diffOutput))
	if diffText == "" {
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes to generate commit message")
		return
	}

	configManager := agentInst.GetConfigManager()
	if configManager == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_configuration_unavailable", "Agent configuration is unavailable")
		return
	}

	clientType, err := configManager.GetProvider()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_resolve_provider", fmt.Sprintf("Failed to resolve provider: %v", err))
		return
	}
	model := configManager.GetModelForProvider(clientType)
	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_provider_client", fmt.Sprintf("Failed to create provider client: %v", err))
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
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_get_staged_file_status", fmt.Sprintf("Failed to get staged file status: %v", err))
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
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes to generate commit message")
		return
	}

	result, err := gitops.GenerateCommitMessageFromStagedDiff(client, gitops.CommitMessageOptions{
		Diff:        diffText,
		Branch:      branch,
		FileChanges: fileChanges,
	})
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_generate_commit_message", fmt.Sprintf("Failed to generate commit message: %v", err))
		return
	}
	commitMessage := strings.TrimSpace(result.Message)

	if commitMessage == "" {
		writeJSONErr(w, http.StatusInternalServerError, "generated_commit_message_empty", "Generated commit message was empty")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "Commit message generated",
		"commit_message": commitMessage,
		"provider":       agentInst.GetProvider(),
		"model":          agentInst.GetModel(),
		"warnings":       result.Warnings,
	})
}

func (ws *ReactWebServer) handleAPIGitRevert(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	req.Commit = strings.TrimSpace(req.Commit)
	if req.Commit == "" {
		writeJSONErr(w, http.StatusBadRequest, "commit_required", "Commit is required")
		return
	}
	if strings.HasPrefix(req.Commit, "-") {
		writeJSONErr(w, http.StatusBadRequest, "invalid_commit_hash", "Invalid commit hash")
		return
	}
	if _, err := gitOutputStringForWorkspace(ws, ws.getWorkspaceRootForRequest(r), "revert", "--no-edit", req.Commit); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_revert_commit", fmt.Sprintf("Failed to revert commit: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Commit reverted successfully",
		"commit":  req.Commit,
	})
}
