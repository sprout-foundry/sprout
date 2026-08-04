//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

var (
	projectsCache     []ProjectInfo
	projectsCacheTime time.Time
	projectsCacheRoot string
	projectsCacheMu   sync.RWMutex
)

// cachedProjectsIn returns the discovered projects under root, reusing a
// recent scan when one is available.
//
// The scan reads every non-hidden directory under root to depth 2, which on a
// home directory is both slow and privacy-prompt-inducing. A single page load
// calls GET /api/workspace from ~10 places (useAppInitialization, useWorkspace,
// WorkspaceBar, useFileIndex, lspClientService, …); without this the walk ran
// once per call.
// The cache holds the raw scan only. Callers that decorate the list (e.g.
// prepending the daemon root) must do so on the returned copy, or the
// decoration leaks into the next reader.
func cachedProjectsIn(root string) (projects []ProjectInfo, wasCached bool) {
	projectsCacheMu.RLock()
	if projectsCacheRoot == root && time.Since(projectsCacheTime) < projectsCacheTTL {
		cached := projectsCache
		projectsCacheMu.RUnlock()
		return cached, true
	}
	projectsCacheMu.RUnlock()

	projects = FindProjectsInDirectory(root, 2)

	projectsCacheMu.Lock()
	projectsCache = projects
	projectsCacheRoot = root
	projectsCacheTime = time.Now()
	projectsCacheMu.Unlock()

	return projects, false
}

// handleAPIStats handles API requests for server statistics
func (ws *ReactWebServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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
			stats["is_processing"] = cs.ActiveQuery
			if cs.ActiveQuery && cs.CurrentQuery != "" {
				stats["chat_current_query"] = cs.CurrentQuery
			}
			cs.mu.Unlock()
		}
	}
	ws.mutex.Unlock()

	// If the locked function didn't find an agent (clientCtx.Agent was nil),
	// try to get or create one outside the lock to avoid deadlock —
	// getClientAgent acquires ws.mutex itself.
	if stats["provider"] == "" && clientID != "" {
		if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
			populateAgentStats(stats, agentInst)
		}
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleAPIWorkspace handles API requests for reading and updating the active workspace root.
func (ws *ReactWebServer) handleAPIWorkspace(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPIWorkspaceGet(w, r)
	case http.MethodPost:
		ws.handleAPIWorkspaceSet(w, r)
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

func (ws *ReactWebServer) handleAPIWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	clientCtx := ws.getClientContextForRequest(r)
	workspaceRoot := clientCtx.WorkspaceRoot
	isProject, markers := IsProjectDirectory(workspaceRoot)

	// Home-workspace gate (SP-130): a workspace that resolves to the user's
	// home directory must force explicit selection unless the user has
	// previously consented. This catches the service-mode case where the
	// daemon starts with CWD=$HOME and IsProjectDirectory returns a false
	// positive (install creates ~/.sprout, a weight-90 marker).
	workspaceIsHome := isHomeWorkspace(workspaceRoot)
	homeConsented := hasHomeWorkspaceConsent()
	// Selection is needed when the dir is not a project, OR when it is home
	// and the user has not yet consented to running in home. A consented home
	// workspace is an intentional choice → needsSelection is false.
	needsSelection := !isProject || (workspaceIsHome && !homeConsented)

	response := map[string]interface{}{
		"daemon_root":               ws.GetDaemonRoot(),
		"workspace_root":            workspaceRoot,
		"is_project":                isProject,
		"project_markers":           markers,
		"needs_workspace_selection": needsSelection,
		"recent_workspaces":         GetRecentWorkspaces(),
		"workspace_is_home":         workspaceIsHome,
		"home_dir":                  resolveHomeDir(),
	}

	// Suggest nearby projects only when selection is actually needed. Moving
	// this out of the unconditional !isProject path means we no longer perform
	// the recursive home-directory walk on every page load once a real project
	// is selected — that walk was tripping macOS TCC media-library prompts.
	if needsSelection {
		suggested, _ := cachedProjectsIn(ws.GetDaemonRoot())
		response["suggested_projects"] = suggested
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
	writeJSON(w, http.StatusOK, response)
}

func (ws *ReactWebServer) handleAPIWorkspaceSet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req struct {
		Path        string `json:"path"`
		ConsentHome bool   `json:"consent_home"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "Path is required")
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
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":          "cannot change workspace while an agent query is active. Wait for the query to complete before switching.",
			"code":           "query_in_progress",
			"active_queries": 1,
		})
		return
	}

	// Home-workspace consent gate (SP-130): selecting the home directory as
	// the workspace is an intentional, high-blast-radius choice (the agent
	// gains access to all files under ~). Require explicit consent on the
	// request and persist it so it is not re-prompted on every launch.
	//
	// Skipped for SSH proxy requests — the remote home is not the local home,
	// and SSH workspaces are explicitly selected via the remote picker.
	// Canonicalize the path before checking so that ~ / $HOME / relative
	// paths are resolved to their real target (otherwise a raw "~" bypasses
	// this gate and only gets caught by the defense-in-depth check in
	// setClientWorkspaceRoot, which returns a generic error instead of the
	// structured consent error the frontend expects).
	if !ws.isSSHProxyRequest(r) {
		if canonicalPath, cerr := filepathAbsEval(req.Path); cerr == nil {
			if isHomeWorkspace(canonicalPath) && !hasHomeWorkspaceConsent() {
				if req.ConsentHome {
					if err := recordHomeWorkspaceConsent(); err != nil {
						ws.log().Warn("failed to record home-workspace consent", slog.Any("err", err))
					}
				} else {
					writeJSON(w, http.StatusForbidden, map[string]interface{}{
						"error": "Selecting the home directory as workspace requires explicit consent. The agent will have access to all files under your home directory.",
						"code":  "home_workspace_requires_consent",
					})
					return
				}
			}
		}
	}

	// Capture the previous workspace root before setting the new one
	previousWorkspaceRoot := ws.getWorkspaceRootForRequest(r)

	workspaceRoot, err := ws.setClientWorkspaceRoot(clientID, req.Path)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "workspace_update_failed", fmt.Sprintf("Failed to set workspace: %v", err))
		return
	}

	// Record this workspace as recently used.
	RecordWorkspace(workspaceRoot)

	ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"workspace_root":          workspaceRoot,
		"previous_workspace_root": previousWorkspaceRoot,
	})

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
	writeJSON(w, http.StatusOK, response)
}

func (ws *ReactWebServer) handleAPIWorkspaceBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	daemonRoot := ws.GetDaemonRoot()
	// Resolve symlinks so the comparison against canonicalDir (which has
	// symlinks resolved by canonicalizePath) works on macOS where
	// /var/folders is a symlink to /private/var/folders.
	resolvedDaemonRoot := daemonRoot
	if evaled, err := filepath.EvalSymlinks(daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = resolvedDaemonRoot
	}

	canonicalDir, err := canonicalizePath(dir, resolvedDaemonRoot, false)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_directory", fmt.Sprintf("Invalid directory: %v", err))
		return
	}
	if canonicalDir != resolvedDaemonRoot && !isWithinWorkspace(canonicalDir, resolvedDaemonRoot) {
		writeJSONErr(w, http.StatusForbidden, "directory_outside_daemon_root", "Directory outside daemon root")
		return
	}

	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "directory_read_failed", fmt.Sprintf("Failed to read directory: %v", err))
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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

const projectsCacheTTL = 5 * time.Minute

func (ws *ReactWebServer) handleAPIWorkspaceProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	daemonRoot := ws.GetDaemonRoot()
	scanned, cached := cachedProjectsIn(daemonRoot)

	// Check if daemon root itself is a project and prepend it. Built on a copy
	// so the prepended entry never lands in the shared cache.
	projects := scanned
	if isProj, markers := IsProjectDirectory(daemonRoot); isProj {
		rootInfo := ProjectInfo{
			Path:    daemonRoot,
			Name:    filepath.Base(daemonRoot),
			Markers: markers,
		}
		projects = append([]ProjectInfo{rootInfo}, scanned...)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects":    projects,
		"daemon_root": ws.GetDaemonRoot(),
		"cached":      cached,
	})
}
