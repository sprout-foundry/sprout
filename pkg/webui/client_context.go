//go:build !js

package webui

// client_context.go — the webClientContext type + construction, the
// per-client ReactWebServer accessors (resolve/touch/pause, get-or-create),
// and the active-chat lookup. SSH/workspace wiring lives in
// client_context_ssh.go; agent-state + agent acquisition in
// client_context_agent.go.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

const (
	webClientIDHeader     = "X-Sprout-Client-ID"
	webClientIDQueryParam = "client_id"
	defaultWebClientID    = "default"

	// clientIDCookieName is the name of the HTTP cookie used for cross-origin
	// session persistence. When the WebUI (Cloudflare Pages) and API (tunnel)
	// live on different domains, the header-based client ID is lost on page
	// reload because the browser does not persist custom headers. The cookie
	// survives reloads and is sent automatically by the browser on every
	// cross-origin request (credentials: 'include'), allowing the server to
	// resume the same client context without re-initialization.
	clientIDCookieName = "sprout_client_id"

	// clientIDCookieMaxAge is the maximum age of the client ID cookie (30 days).
	// This is intentionally long-lived so that users who leave a tab open or
	// return after a break can resume their session.
	clientIDCookieMaxAge = 30 * 24 * time.Hour
)

type webClientContext struct {
	WorkspaceRoot    string
	SSHHostAlias     string
	SSHSessionKey    string
	SSHLauncherURL   string
	SSHHomePath      string
	UserID           string // User ID extracted from trusted header (service mode)
	Terminal         *TerminalManager
	FileConsents     *fileConsentManager
	Agent            *agent.Agent
	AgentState       []byte
	CurrentSessionID string
	CurrentQuery     string
	ActiveQuery      bool
	LastSeenAt       time.Time

	// Paused is set when the client signals it is backgrounding (the tab went
	// hidden) rather than closing. While paused, the heartbeat monitor leaves
	// an in-flight query running (up to maxPausedQueryDuration) instead of
	// cancelling it, so a long agent run keeps going in the background and the
	// client can reattach when it returns. Cleared on reconnect, on an explicit
	// resume, or on a session_close (which cancels the run outright).
	Paused   bool
	PausedAt time.Time

	// Multi-chat support: one client context (tab) can have multiple
	// independent chat sessions, each with its own agent state.
	ChatSessions   map[string]*chatSession
	DefaultChatID  string
	nextChatNumber int

	// DeletedChats records chat IDs that were deleted but whose deletion may
	// still be settling (the delete handler removes the session from the map
	// and then recomputes the top-level ActiveQuery flag in a later lock
	// block). A query targeting an absent chat falls back to the top-level
	// ActiveQuery, which the recompute may have reset to false — allowing a
	// query to start on a chat that was just deleted. Tombstones make an
	// absent-but-deleted chat permanently non-queryable until a new chat
	// with the same ID is created (which clears the tombstone).
	DeletedChats map[string]struct{}
}

func newWebClientContext(workspaceRoot, sshHostAlias, sshSessionKey, sshLauncherURL, sshHomePath string) *webClientContext {
	ctx := &webClientContext{
		WorkspaceRoot:  workspaceRoot,
		SSHHostAlias:   strings.TrimSpace(sshHostAlias),
		SSHSessionKey:  strings.TrimSpace(sshSessionKey),
		SSHLauncherURL: strings.TrimSpace(sshLauncherURL),
		SSHHomePath:    strings.TrimSpace(sshHomePath),
		Terminal:       NewTerminalManager(workspaceRoot),
		FileConsents:   newFileConsentManager(),
		AgentState:     emptyAgentStateSnapshot(),
		LastSeenAt:     time.Now(),
		DeletedChats:   map[string]struct{}{},
	}
	ctx.ensureDefaultChatSession()
	return ctx
}

func emptyAgentStateSnapshot() []byte {
	data, _ := json.Marshal(agent.AgentState{Messages: []api.Message{}})
	return data
}

// touchClientLastSeen updates the LastSeenAt timestamp for a client context
// without creating a new context if one doesn't exist. Used by WebSocket
// read goroutines to keep the client context alive during active connections.
func (ws *ReactWebServer) touchClientLastSeen(clientID string) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.LastSeenAt = time.Now()
	}
}

// setClientPaused marks (or clears) a client as paused — the tab is backgrounded
// but expected to return. While paused, the heartbeat monitor keeps any in-flight
// query running instead of cancelling it on staleness. Cleared on reconnect /
// resume / session_close.
func (ws *ReactWebServer) setClientPaused(clientID string, paused bool) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.Paused = paused
		if paused {
			ctx.PausedAt = time.Now()
		} else {
			ctx.PausedAt = time.Time{}
		}
	}
}

func (ws *ReactWebServer) resolveClientID(r *http.Request) string {
	if r == nil {
		return defaultWebClientID
	}
	clientID := strings.TrimSpace(r.Header.Get(webClientIDHeader))
	if clientID == "" {
		clientID = strings.TrimSpace(r.URL.Query().Get(webClientIDQueryParam))
	}
	if clientID == "" {
		// Fall back to the cross-origin cookie. This is the primary
		// identification mechanism when the WebUI and API live on
		// different domains (Cloudflare Pages + tunnel).
		cookie, err := r.Cookie(clientIDCookieName)
		if err == nil && cookie.Value != "" {
			clientID = cookie.Value
		}
	}
	if clientID == "" {
		clientID = defaultWebClientID
	}
	return sanitizeClientID(clientID)
}

// sanitizeClientID removes any path traversal characters from a client ID
// to prevent directory traversal attacks when constructing config paths.
func sanitizeClientID(id string) string {
	// Remove path separators and traversal sequences
	id = strings.ReplaceAll(id, "/", "")
	id = strings.ReplaceAll(id, "\\", "")
	id = strings.ReplaceAll(id, "..", "")
	if id == "" {
		return defaultWebClientID
	}
	return id
}

// getActiveChatContext returns the client context and active chat ID for a given client ID.
// This is a convenience method to reduce repetitive mutex locking boilerplate in message handlers.
// Returns (nil, "") if the client context does not exist.
func (ws *ReactWebServer) getActiveChatContext(clientID string) (*webClientContext, string) {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	ctx := ws.clientContexts[clientID]
	var chatID string
	if ctx != nil {
		chatID = ctx.getActiveChatID()
	}
	return ctx, chatID
}

func (ws *ReactWebServer) getOrCreateClientContext(clientID string) *webClientContext {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	return ws.getOrCreateClientContextLocked(clientID)
}

func (ws *ReactWebServer) getOrCreateClientContextLocked(clientID string) *webClientContext {
	// SP-136 isolation invariants (enforced by this function):
	//   1. clientContexts is keyed by clientID — each browser tab / client
	//      session gets its own context.
	//   2. Each webClientContext owns its own WorkspaceRoot, Agent,
	//      Terminal, FileConsents, and ChatSessions.  When the workspace
	//      root changes (setClientWorkspaceRoot), old agents are released
	//      and chat sessions are reset.
	//   3. Agents are created per-chat via NewAgentWithLayersInWorkspace
	//      using the workspace-specific config directory
	//      (configuration.WorkspaceConfigDir(workspaceRoot)), ensuring
	//      per-workspace config isolation.
	//   4. Embedding managers are further isolated per workspace via
	//      embedding.AcquireManager keyed by (indexDir, workspaceRoot).
	//
	// EXCEPTIONS (intentional shared state):
	//   - The defaultWebClientID context shares ws.terminalManager and
	//     ws.fileConsents with the server.  This is the shared CLI+WebUI
	//     mode where the CLI and default WebUI tab share one conversation.
	//     Non-default clients always get fresh per-client Terminal/FileConsents.
	//   - The eventBus is shared across all clients but events route by
	//     client_id/chat_id via shouldForwardEventToConnection.
	//
	if ws.clientContexts == nil {
		ws.clientContexts = make(map[string]*webClientContext)
	}
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.LastSeenAt = time.Now()
		if ctx.Terminal == nil {
			ctx.Terminal = NewTerminalManager(ctx.WorkspaceRoot)
			ws.startTerminalCleanupIfNeeded(ctx.Terminal)
		}
		if ctx.FileConsents == nil {
			ctx.FileConsents = newFileConsentManager()
		}
		if len(ctx.AgentState) == 0 {
			ctx.AgentState = emptyAgentStateSnapshot()
		}
		// Ensure multi-chat is initialized (handles migration from old contexts
		// that were created before chat sessions were added).
		ctx.ensureDefaultChatSession()
		return ctx
	}

	// Determine workspace root for the new client context. A context that
	// selects a workspace and is later evicted (or whose daemon restarted)
	// must come back in that workspace, not the daemon's launch directory —
	// the recreated context owns every new terminal session's cwd, and the
	// client has no way to know the switch happened.
	workspaceRoot := ws.workspaceRoot
	if clientID != defaultWebClientID {
		if remembered := strings.TrimSpace(ws.clientWorkspaces[clientID]); remembered != "" {
			if info, err := os.Stat(remembered); err == nil && info.IsDir() {
				workspaceRoot = remembered
			} else {
				delete(ws.clientWorkspaces, clientID)
			}
		}
	}

	var ctx *webClientContext
	if clientID == defaultWebClientID {
		ctx = &webClientContext{
			WorkspaceRoot:  workspaceRoot,
			SSHHostAlias:   ws.sshHostAlias,
			SSHSessionKey:  ws.sshSessionKey,
			SSHLauncherURL: ws.sshLauncherURL,
			SSHHomePath:    ws.sshHomePath,
			Terminal:       ws.terminalManager,
			FileConsents:   ws.fileConsents,
			AgentState:     emptyAgentStateSnapshot(),
			LastSeenAt:     time.Now(),
		}
		if ctx.Terminal == nil {
			ctx.Terminal = NewTerminalManager(ctx.WorkspaceRoot)
			ws.terminalManager = ctx.Terminal
			ws.startTerminalCleanupIfNeeded(ctx.Terminal)
		}
		if ctx.FileConsents == nil {
			ctx.FileConsents = newFileConsentManager()
			ws.fileConsents = ctx.FileConsents
		}
		ctx.ensureDefaultChatSession()
	} else {
		ctx = newWebClientContext(workspaceRoot, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
		ws.startTerminalCleanupIfNeeded(ctx.Terminal)
	}

	ws.clientContexts[clientID] = ctx
	return ctx
}

func (ws *ReactWebServer) getClientContextForRequest(r *http.Request) *webClientContext {
	ctx := ws.getOrCreateClientContext(ws.resolveClientID(r))
	// Populate UserID from request context if not already set (avoids overwriting on every request)
	if ctx.UserID == "" {
		if userID := UserIDFromContext(r.Context()); userID != "" {
			ctx.UserID = userID
		}
	}
	return ctx
}
