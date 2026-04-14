package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
)

// handleAPIStats handles API requests for server statistics
func (ws *ReactWebServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)
	// Ensure the client context exists before gathering stats and build the
	// response under the lock — getOrCreateClientContextLocked may mutate the
	// map, and the subsequent read must observe a consistent snapshot.
	ws.mutex.Lock()
	ws.getOrCreateClientContextLocked(clientID)
	stats := ws.gatherStatsForClientIDLocked(clientID)
	// Add chat session info to the stats response
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		stats["active_chat_id"] = ctx.getActiveChatID()
		stats["chat_session_count"] = len(ctx.ChatSessions)
		if cs := ctx.getChatSession(chatID); cs != nil {
			cs.mu.Lock()
			stats["chat_id"] = chatID
			stats["chat_is_processing"] = cs.ActiveQuery
			if cs.ActiveQuery && cs.CurrentQuery != "" {
				stats["chat_current_query"] = cs.CurrentQuery
			}
			cs.mu.Unlock()
		}
	}
	ws.mutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAPIWorkspace handles API requests for reading and updating the active workspace root.
func (ws *ReactWebServer) handleAPIWorkspace(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPIWorkspaceGet(w, r)
	case http.MethodPost:
		ws.handleAPIWorkspaceSet(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *ReactWebServer) handleAPIWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	clientCtx := ws.getClientContextForRequest(r)
	response := map[string]interface{}{
		"daemon_root":    ws.GetDaemonRoot(),
		"workspace_root": clientCtx.WorkspaceRoot,
	}
	if clientCtx.SSHHostAlias != "" {
		sshContext := map[string]interface{}{
			"host_alias":  clientCtx.SSHHostAlias,
			"session_key": clientCtx.SSHSessionKey,
			"is_remote":   true,
			"launch_mode": "ssh",
		}
		if clientCtx.SSHLauncherURL != "" {
			sshContext["launcher_url"] = clientCtx.SSHLauncherURL
		}
		if clientCtx.SSHHomePath != "" {
			sshContext["home_path"] = clientCtx.SSHHomePath
		}
		response["ssh_context"] = sshContext
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ws *ReactWebServer) handleAPIWorkspaceSet(w http.ResponseWriter, r *http.Request) {
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

	clientID := ws.resolveClientID(r)

	// When running behind the SSH proxy, the frontend sends the locally-resolved
	// absolute path. We need to store the remote path format instead so that
	// relative path resolution (like for pasted images) works correctly on the
	// remote backend.
	// 
	// The SSH session already has the remote workspace path (e.g., "$HOME/project").
	// We use that directly instead of the locally-resolved absolute path.
	if req.Path != "" && ws.isSSHProxyRequest(r) {
		session := ws.getSSHSessionForProxyRequest(r)
		if session != nil && session.RemoteWorkspacePath != "" {
			// Use the remote workspace path directly - it's what the remote
			// backend needs for correct relative path resolution
			req.Path = session.RemoteWorkspacePath
		}
	}

	// Reject workspace changes only for the window that currently owns an active run.
	if ws.hasActiveQueryForClient(clientID) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "cannot change workspace while an agent query is active. Wait for the query to complete before switching.",
			"code":           "query_in_progress",
			"active_queries": 1,
		})
		return
	}

	// Capture the previous workspace root before setting the new one
	previousWorkspaceRoot := ws.getWorkspaceRootForRequest(r)

	workspaceRoot, err := ws.setClientWorkspaceRoot(clientID, req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to set workspace: %v", err), http.StatusBadRequest)
		return
	}

	ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"workspace_root":          workspaceRoot,
		"previous_workspace_root": previousWorkspaceRoot,
	})

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"daemon_root":    ws.GetDaemonRoot(),
		"message":        "Workspace updated",
		"workspace_root": workspaceRoot,
	}
	clientCtx := ws.getClientContextForRequest(r)
	if clientCtx.SSHHostAlias != "" {
		sshContext := map[string]interface{}{
			"host_alias":  clientCtx.SSHHostAlias,
			"session_key": clientCtx.SSHSessionKey,
			"is_remote":   true,
			"launch_mode": "ssh",
		}
		if clientCtx.SSHLauncherURL != "" {
			sshContext["launcher_url"] = clientCtx.SSHLauncherURL
		}
		if clientCtx.SSHHomePath != "" {
			sshContext["home_path"] = clientCtx.SSHHomePath
		}
		response["ssh_context"] = sshContext
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (ws *ReactWebServer) handleAPIWorkspaceBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	daemonRoot := ws.GetDaemonRoot()
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = daemonRoot
	}

	canonicalDir, err := canonicalizePath(dir, daemonRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid directory: %v", err), http.StatusBadRequest)
		return
	}
	if canonicalDir != daemonRoot && !isWithinWorkspace(canonicalDir, daemonRoot) {
		http.Error(w, "Directory outside daemon root", http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := map[string]interface{}{
			"name": entry.Name(),
			"path": filepath.Join(canonicalDir, entry.Name()),
			"type": "file",
		}
		if entry.IsDir() {
			fileInfo["type"] = "directory"
		}
		fileInfo["size"] = info.Size()
		fileInfo["modified"] = info.ModTime().Unix()
		files = append(files, fileInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "success",
		"path":           canonicalDir,
		"daemon_root":    daemonRoot,
		"workspace_root": ws.getWorkspaceRootForRequest(r),
		"files":          files,
	})
}

// isSSHProxyRequest checks if the request came through the SSH proxy tunnel.
// It does this by checking if the request path starts with "/ssh/" or if the
// remote client is connected via SSH.
func (ws *ReactWebServer) isSSHProxyRequest(r *http.Request) bool {
	// Check if the path indicates an SSH proxy request
	if strings.HasPrefix(r.URL.Path, "/ssh/") {
		return true
	}
	// Also check the client context for SSH session
	clientID := ws.resolveClientID(r)
	if clientID != "" {
		ctx := ws.getOrCreateClientContext(clientID)
		if ctx != nil && ctx.SSHSessionKey != "" {
			return true
		}
	}
	return false
}

// getSSHSessionForProxyRequest looks up the SSH session for a proxied request.
// Returns nil if not an SSH proxy request.
func (ws *ReactWebServer) getSSHSessionForProxyRequest(r *http.Request) *sshWorkspaceSession {
	// Extract session key from /ssh/{sessionKey}/ path
	if !strings.HasPrefix(r.URL.Path, "/ssh/") {
		return nil
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/ssh/")
	var sessionKey string
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		sessionKey = trimmed[:idx]
	} else {
		sessionKey = trimmed
	}
	sessionKey, _ = url.PathUnescape(sessionKey)

	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	return ws.sshSessions[sessionKey]
}

