// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" {
		http.Error(w, "Invalid route", http.StatusBadRequest)
		return
	}
	chatID := parts[0]

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	worktreePath := ctx.getChatSessionWorktree(chatID)
	ws.mutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "success",
		"chat_id":       chatID,
		"worktree_path": worktreePath,
	})
}

// handleAPIChatSessionWorktreeSet handles POST /api/chat-session/{chatID}/worktree
// Sets the worktree path for a specific chat session.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" {
		http.Error(w, "Invalid route", http.StatusBadRequest)
		return
	}
	chatID := parts[0]

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate worktree path if provided
	if req.WorktreePath != "" {
		absPath, err := filepath.Abs(req.WorktreePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
			return
		}

		// Validate the path is within the daemon root boundary
		ws.mutex.RLock()
		daemonRoot := ws.daemonRoot
		ws.mutex.RUnlock()
		if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
			http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
			return
		}

		// Check if it's a valid git worktree
		if err := ws.validateGitWorktree(absPath); err != nil {
			http.Error(w, fmt.Sprintf("Invalid worktree: %v", err), http.StatusBadRequest)
			return
		}
		req.WorktreePath = absPath
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if err := ctx.setChatSessionWorktree(chatID, req.WorktreePath); err != nil {
		ws.mutex.Unlock()
		http.Error(w, fmt.Sprintf("Failed to set worktree: %v", err), http.StatusBadRequest)
		return
	}

	// Get the updated session for the response
	isDefault := chatID == ctx.DefaultChatID
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionWorktreeSet: set worktree %q for chat session %s", req.WorktreePath, chatID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "Worktree set successfully",
		"chat_id":       chatID,
		"worktree_path": req.WorktreePath,
		"chat_session":  cs.chatSessionSummary(isDefault),
	})
}

// handleAPIChatSessionWorktreeSwitch handles POST /api/chat-session/{chatID}/worktree/switch
// Switches the active workspace to the specified worktree path for the current client.
func (ws *ReactWebServer) handleAPIChatSessionWorktreeSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract chatID from URL path: /api/chat-session/{chatID}/worktree/switch
	path := strings.TrimPrefix(r.URL.Path, "/api/chat-session/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "worktree" || parts[2] != "switch" {
		http.Error(w, "Invalid route", http.StatusBadRequest)
		return
	}
	chatID := parts[0]

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		WorktreePath string `json:"worktree_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate worktree path
	if req.WorktreePath == "" {
		http.Error(w, "Worktree path is required", http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(req.WorktreePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
		return
	}

	// Validate the path is within the daemon root boundary
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(absPath, daemonRoot) && absPath != daemonRoot {
		http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
		return
	}

	// Validate it's a valid git worktree
	if err := ws.validateGitWorktree(absPath); err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree: %v", err), http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)

	// Set the worktree for the chat session and switch workspace root atomically.
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if err := ctx.setChatSessionWorktree(chatID, absPath); err != nil {
		ws.mutex.Unlock()
		http.Error(w, fmt.Sprintf("Failed to set worktree: %v", err), http.StatusBadRequest)
		return
	}

	// Switch workspace root directly — do NOT call setClientWorkspaceRoot
	// because it nukes all chat sessions (including the one we just updated).
	ctx.WorkspaceRoot = absPath
	if clientID == defaultWebClientID {
		ws.workspaceRoot = absPath
	}

	// Capture response data while still holding the lock
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	if cs == nil {
		http.Error(w, "Chat session not found after workspace switch", http.StatusInternalServerError)
		return
	}

	log.Printf("handleAPIChatSessionWorktreeSwitch: switched chat session %s to worktree %s", chatID, absPath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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
		http.Error(w, "Invalid route", http.StatusBadRequest)
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
		if r.Method == http.MethodGet {
			ws.handleAPIChatSessionWorktreeGet(w, r)
			return
		} else if r.Method == http.MethodPost {
			ws.handleAPIChatSessionWorktreeSet(w, r)
			return
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	} else {
		http.Error(w, "Invalid route", http.StatusBadRequest)
		return
	}
}

// handleAPIChatSessionCreateInWorktree handles POST /api/chat-sessions/create-in-worktree
// Creates a git worktree, creates a new chat session, associates the worktree with the chat,
// and optionally switches the workspace to the worktree.
func (ws *ReactWebServer) handleAPIChatSessionCreateInWorktree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Branch = strings.TrimSpace(req.Branch)
	req.Name = strings.TrimSpace(req.Name)

	if req.Branch == "" {
		http.Error(w, "Branch name is required", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Validate branch name using git's own validation
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "check-ref-format", "--branch", req.Branch)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid branch name: %s", strings.TrimSpace(string(output))), http.StatusBadRequest)
		return
	}

	// Sanitize branch name for use in worktree path (flatten slashes)
	sanitizedBranch := strings.ReplaceAll(req.Branch, "/", "-")
	// Only allow alphanumeric, hyphens, underscores, and dots in the path component
	safeBranch := sanitizePathComponent(sanitizedBranch)
	worktreePath := filepath.Join(filepath.Dir(workspaceRoot), safeBranch+"-worktree")
	var err error
	worktreePath, err = filepath.Abs(worktreePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid worktree path: %v", err), http.StatusBadRequest)
		return
	}

	// Validate the resolved worktree path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(worktreePath, daemonRoot) && worktreePath != daemonRoot {
		http.Error(w, "Worktree path must stay within workspace boundary", http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("Failed to create worktree: %v\nOutput: %s", err, string(output)), http.StatusInternalServerError)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Chat session with this ID already exists",
			"code":  "chat_session_exists",
			"id":    chatID,
		})
		return
	}

	cs := newChatSession(chatID, name)
	ctx.ChatSessions[chatID] = cs
	cs.setWorktreePath(worktreePath)

	// Optionally switch the workspace root to the worktree.
	// We update WorkspaceRoot directly instead of calling setClientWorkspaceRoot
	// because setClientWorkspaceRoot resets all chat sessions, which would
	// destroy the session we just created.
	if req.AutoSwitchWorkspace {
		ctx.WorkspaceRoot = worktreePath
		if clientID == defaultWebClientID {
			ws.workspaceRoot = worktreePath
		}
	}

	// Capture response data while still holding the lock
	chatSession := cs.chatSessionWithMessages()
	newWorkspaceRoot := ctx.WorkspaceRoot
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionCreateInWorktree: created chat session %s (%s) with worktree %s for client %s",
		chatID, name, worktreePath, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "Chat session created in worktree",
		"chat_session":   chatSession,
		"worktree_path":  worktreePath,
		"branch":         req.Branch,
		"workspace_root": newWorkspaceRoot,
	})
}
