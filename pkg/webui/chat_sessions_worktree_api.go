//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

var unsafePathCharRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizePathComponent strips characters that are unsafe or confusing in
// file-system directory names, keeping only alphanumerics, hyphens, underscores,
// and dots. This is used to derive a worktree directory name from a git branch.
func sanitizePathComponent(s string) string {
	return unsafePathCharRe.ReplaceAllString(s, "_")
}

// handleAPIChatSessionWorktreeGet handles GET /api/chat-session/{chatID}/worktree
// Returns the worktree path for a specific chat session.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeGet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" {
		writeJSONErr(w, http.StatusBadRequest, "invalid_route", "Invalid route")
		return
	}
	chatID := parts[0]

	clientID := ws.resolveClientID(r)

	ctx := ws.getOrCreateClientContext(clientID)
	ws.mutex.RLock()
	worktreePath := ctx.getChatSessionWorktree(chatID)
	ws.mutex.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":       "success",
		"chat_id":       chatID,
		"worktree_path": worktreePath,
	})
}

// handleAPIChatSessionWorktreeSet handles POST /api/chat-session/{chatID}/worktree
// Sets the worktree path for a specific chat session.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeSet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" {
		writeJSONErr(w, http.StatusBadRequest, "invalid_route", "Invalid route")
		return
	}
	chatID := parts[0]

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	// Validate worktree path if provided
	if req.WorktreePath != "" {
		absPath, err := filepathAbsEval(req.WorktreePath)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid_worktree_path", fmt.Sprintf("Invalid worktree path: %v", err))
			return
		}

		// Validate the path is within the daemon root boundary
		ws.mutex.RLock()
		daemonRoot := ws.daemonRoot
		ws.mutex.RUnlock()
		if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
			writeJSONErr(w, http.StatusBadRequest, "path_outside_workspace", "Worktree path must stay within workspace boundary")
			return
		}

		// Check if it's a valid git worktree
		if err := ws.validateGitWorktree(absPath); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid_worktree", fmt.Sprintf("Invalid worktree: %v", err))
			return
		}
		req.WorktreePath = absPath
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	// Capture old worktree path before overwriting.
	oldWorktreePath := ctx.getChatSessionWorktree(chatID)

	if err := ctx.setChatSessionWorktree(chatID, req.WorktreePath); err != nil {
		ws.mutex.Unlock()
		writeJSONErr(w, http.StatusBadRequest, "failed_to_set_worktree", fmt.Sprintf("Failed to set worktree: %v", err))
		return
	}

	// When clearing a worktree from the active chat, if the client's
	// workspace root was pointing at that worktree, reset it to the
	// daemon root so subsequent file operations use the main workspace.
	didResetWorkspace := false
	if req.WorktreePath == "" && oldWorktreePath != "" && chatID == ctx.DefaultChatID && ctx.WorkspaceRoot == oldWorktreePath {
		ctx.WorkspaceRoot = ws.daemonRoot
		if clientID == defaultWebClientID {
			ws.workspaceRoot = ws.daemonRoot
		}
		ctx.Agent = nil
		ctx.Terminal = nil
		didResetWorkspace = true
	}

	// Get the updated session for the response
	isDefault := chatID == ctx.DefaultChatID
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	ws.log().Info("set chat session worktree", slog.String("worktree_path", req.WorktreePath), slog.String("chat_id", chatID))

	// Notify frontend if the workspace root was reset to daemon root.
	if didResetWorkspace {
		ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
			"daemon_root":             ws.GetDaemonRoot(),
			"workspace_root":          ws.daemonRoot,
			"previous_workspace_root": oldWorktreePath,
			"source":                  "worktree_clear",
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":       "Worktree set successfully",
		"chat_id":       chatID,
		"worktree_path": req.WorktreePath,
		"chat_session":  cs.chatSessionSummary(isDefault),
	})
}

// handleAPIChatSessionWorktreeSwitch handles POST /api/chat-session/{chatID}/worktree/switch
// Switches the active workspace to the specified worktree path for the current client.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeSwitch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree/switch
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" || parts[2] != "switch" {
		writeJSONErr(w, http.StatusBadRequest, "invalid_route", "Invalid route")
		return
	}
	chatID := parts[0]

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	// Validate worktree path
	if req.WorktreePath == "" {
		writeJSONErr(w, http.StatusBadRequest, "worktree_path_required", "Worktree path is required")
		return
	}

	absPath, err := filepathAbsEval(req.WorktreePath)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_worktree_path", fmt.Sprintf("Invalid worktree path: %v", err))
		return
	}

	// Validate the path is within the daemon root boundary
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
		writeJSONErr(w, http.StatusBadRequest, "path_outside_workspace", "Worktree path must stay within workspace boundary")
		return
	}

	// Validate it's a valid git worktree
	if err := ws.validateGitWorktree(absPath); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_worktree", fmt.Sprintf("Invalid worktree: %v", err))
		return
	}

	clientID := ws.resolveClientID(r)

	// Set the worktree for the chat session and switch workspace root atomically.
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if err := ctx.setChatSessionWorktree(chatID, absPath); err != nil {
		ws.mutex.Unlock()
		writeJSONErr(w, http.StatusBadRequest, "failed_to_set_worktree", fmt.Sprintf("Failed to set worktree: %v", err))
		return
	}

	// Capture the previous workspace root before switching.
	previousWorkspaceRoot := ctx.WorkspaceRoot

	// Switch workspace root directly — do NOT call setClientWorkspaceRoot
	// because it nukes all chat sessions (including the one we just updated).
	ctx.WorkspaceRoot = absPath
	if clientID == defaultWebClientID {
		ws.workspaceRoot = absPath
	}
	// Clear transient state (agent, terminals) like handleAPIGitWorktreeCheckout does.
	ctx.Agent = nil
	ctx.Terminal = nil

	// Capture response data while still holding the lock
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	if cs == nil {
		writeJSONErr(w, http.StatusInternalServerError, "chat_session_not_found", "Chat session not found after workspace switch")
		return
	}

	// Publish event so frontend can update workspace state.
	ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"workspace_root":          absPath,
		"previous_workspace_root": previousWorkspaceRoot,
		"source":                  "worktree_switch",
	})

	ws.log().Info("switched chat session worktree", slog.String("chat_id", chatID), slog.String("worktree_path", absPath))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":       "Switched to worktree successfully",
		"chat_id":       chatID,
		"worktree_path": absPath,
		"chat_session":  cs.chatSessionWithMessages(),
	})
}

// validateGitWorktree checks if a path is a valid git repository or worktree.
func (ws *ReactWebServer) validateGitWorktree(path string) error {
	// Check if .git exists (either as file or directory)
	checkCmd := ws.gitCommandForWorkspace(path, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("path is not a git repository or worktree")
	}

	return nil
}

// handleAPIChatSessionWorktree is a dispatcher for /api/chat-session/{chatID}/worktree/*
func (ws *ReactWebServer) handleAPIChatSessionWorktree(w http.ResponseWriter, r *http.Request) {
	// Extract the path after /api/chat-session/
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeJSONErr(w, http.StatusBadRequest, "invalid_route", "Invalid route")
		return
	}
	_ = parts[0] // chatID - already extracted

	// Determine which operation based on remaining path.
	// Use >= 2 so that /worktree/switch (3 parts) is also matched.
	if len(parts) >= 2 && parts[1] == "worktree" {
		// Check if it's a switch operation
		if len(parts) >= 3 && parts[2] == "switch" {
			ws.handleAPIChatSessionWorktreeSwitch(w, r)
			return
		}
		// Check if GET or POST
		if !requireMethods(w, r, http.MethodGet, http.MethodPost) {
			return
		}
		if r.Method == http.MethodGet {
			ws.handleAPIChatSessionWorktreeGet(w, r)
			return
		}
		ws.handleAPIChatSessionWorktreeSet(w, r)
		return
	} else {
		writeJSONErr(w, http.StatusBadRequest, "invalid_route", "Invalid route")
		return
	}
}

// handleAPIChatSessionCreateInWorktree handles POST /api/chat-sessions/create-in-worktree
// Creates a git worktree, creates a new chat session, associates the worktree with the chat,
// and optionally switches the workspace to the worktree.
func (ws *ReactWebServer) handleAPIChatSessionCreateInWorktree(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Branch              string `json:"branch"`
		BaseRef             string `json:"base_ref,omitempty"`
		Name                string `json:"name,omitempty"`
		AutoSwitchWorkspace bool   `json:"auto_switch_workspace,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	req.Branch = strings.TrimSpace(req.Branch)
	req.Name = strings.TrimSpace(req.Name)

	if req.Branch == "" {
		writeJSONErr(w, http.StatusBadRequest, "branch_name_required", "Branch name is required")
		return
	}

	clientID := ws.resolveClientID(r)
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Validate branch name using git's own validation
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "check-ref-format", "--branch", req.Branch)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_branch_name", fmt.Sprintf("Invalid branch name: %s", strings.TrimSpace(string(output))))
		return
	}

	// Sanitize branch name for use in worktree path (flatten slashes)
	sanitizedBranch := strings.ReplaceAll(req.Branch, "/", "-")
	// Only allow alphanumeric, hyphens, underscores, and dots in the path component
	safeBranch := sanitizePathComponent(sanitizedBranch)
	worktreePath := filepath.Join(filepath.Dir(workspaceRoot), safeBranch+"-worktree")
	var err error
	worktreePath, err = filepathAbsEval(worktreePath)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_worktree_path", fmt.Sprintf("Invalid worktree path: %v", err))
		return
	}

	// Validate the resolved worktree path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(worktreePath, daemonRoot) && worktreePath != daemonRoot {
		writeJSONErr(w, http.StatusBadRequest, "path_outside_workspace", "Worktree path must stay within workspace boundary")
		return
	}

	// Check if the worktree path already exists on disk (path collision)
	if _, statErr := os.Stat(worktreePath); statErr == nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":         "A worktree already exists at the computed path. Use a different branch name or manually remove the existing worktree first.",
			"code":          "worktree_path_conflict",
			"worktree_path": worktreePath,
		})
		return
	}

	// Create the git worktree
	args := []string{"worktree", "add"}
	if req.BaseRef != "" {
		args = append(args, "-b", req.Branch, worktreePath, req.BaseRef)
	} else {
		args = append(args, "-b", req.Branch, worktreePath)
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is due to branch already existing
		outputStr := strings.TrimSpace(string(output))
		if strings.Contains(outputStr, "already exists") || strings.Contains(outputStr, "ref already exists") {
			// Clean up any partial worktree directory that may have been created
			if removeErr := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "remove", "--force", worktreePath).Run(); removeErr != nil {
				// Also try to remove the directory if git worktree remove failed
				_ = os.RemoveAll(worktreePath)
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":         fmt.Sprintf("Branch '%s' already exists", req.Branch),
				"code":          "branch_exists",
				"worktree_path": worktreePath,
			})
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_worktree", fmt.Sprintf("Failed to create worktree: %v\nOutput: %s", err, outputStr))
		return
	}

	// Generate a unique chat ID
	chatID := generateChatID()
	name := req.Name

	// Atomically generate name (if needed) and create the chat session
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if name == "" {
		ctx.nextChatNumber++
		name = "Chat " + strconv.Itoa(ctx.nextChatNumber)
	}

	// Check if a session with this ID already exists
	if _, ok := ctx.ChatSessions[chatID]; ok {
		ws.mutex.Unlock()
		// Clean up the orphan worktree that was created before the conflict
		if removeErr := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "remove", "--force", worktreePath).Run(); removeErr != nil {
			ws.log().Warn("failed to clean up orphan worktree", slog.String("worktree_path", worktreePath), slog.Any("err", removeErr))
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Chat session with this ID already exists",
			"code":  "chat_session_exists",
			"id":    chatID,
		})
		return
	}

	cs := newChatSession(chatID, name)
	ctx.ChatSessions[chatID] = cs
	ctx.markChatCreated(chatID)
	cs.setWorktreePath(worktreePath)

	// Optionally switch the workspace root to the worktree.
	// We update WorkspaceRoot directly instead of calling setClientWorkspaceRoot
	// because setClientWorkspaceRoot resets all chat sessions, which would
	// destroy the session we just created.
	previousWorkspaceRoot := ctx.WorkspaceRoot
	if req.AutoSwitchWorkspace {
		ctx.WorkspaceRoot = worktreePath
		if clientID == defaultWebClientID {
			ws.workspaceRoot = worktreePath
		}
		// Clear transient state (agent, terminals) like other workspace-switch handlers.
		ctx.Agent = nil
		ctx.Terminal = nil
	}

	// Capture response data while still holding the lock
	chatSession := cs.chatSessionWithMessages()
	newWorkspaceRoot := ctx.WorkspaceRoot
	ws.mutex.Unlock()

	// Notify frontend of workspace change if auto-switched.
	if req.AutoSwitchWorkspace {
		ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
			"daemon_root":             ws.GetDaemonRoot(),
			"workspace_root":          worktreePath,
			"previous_workspace_root": previousWorkspaceRoot,
			"source":                  "worktree_switch",
		})
	}

	ws.log().Info("created chat session with worktree",
		slog.String("chat_id", chatID),
		slog.String("name", name),
		slog.String("worktree_path", worktreePath),
		slog.String("client_id", clientID))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "Chat session created in worktree",
		"chat_session":   chatSession,
		"worktree_path":  worktreePath,
		"branch":         req.Branch,
		"workspace_root": newWorkspaceRoot,
	})
}

// handleAPIChatSessionWorktreeList handles GET /api/chat-sessions/worktree-mappings
// Returns all chat sessions that have worktree paths, so the UI can display
// which chats are associated with which worktrees.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	clientID := ws.resolveClientID(r)

	ctx := ws.getOrCreateClientContext(clientID)
	ws.mutex.RLock()
	sessions := ctx.listChatSessions()
	ws.mutex.RUnlock()

	mappings := make([]map[string]string, 0)
	for _, info := range sessions {
		if info.WorktreePath != "" {
			mappings = append(mappings, map[string]string{
				"chat_id":       info.ID,
				"chat_name":     info.Name,
				"worktree_path": info.WorktreePath,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "success",
		"mappings": mappings,
	})
}
