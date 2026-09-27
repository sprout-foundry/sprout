//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

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
	} else {
		ws.rememberClientWorkspacesLocked()
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
