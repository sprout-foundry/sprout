package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/factory"
	gitops "github.com/alantheprice/ledit/pkg/git"
)

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
