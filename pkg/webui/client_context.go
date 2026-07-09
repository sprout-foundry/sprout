//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
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

	// Determine workspace root for the new client context.
	workspaceRoot := ws.workspaceRoot

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
		ctx = newWebClientContext(ws.workspaceRoot, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
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

func (ws *ReactWebServer) clearClientSSHContextForSessionKey(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	for clientID, ctx := range ws.clientContexts {
		if ctx == nil || strings.TrimSpace(ctx.SSHSessionKey) != sessionKey {
			continue
		}
		ctx.SSHHostAlias = ""
		ctx.SSHSessionKey = ""
		ctx.SSHLauncherURL = ""
		ctx.SSHHomePath = ""
		ctx.LastSeenAt = time.Now()

		if clientID == defaultWebClientID {
			ws.sshHostAlias = ""
			ws.sshSessionKey = ""
			ws.sshLauncherURL = ""
			ws.sshHomePath = ""
		}
	}
}

func (ws *ReactWebServer) setClientWorkspaceRoot(clientID, path string) (string, error) {
	workspaceRoot, err := filepathAbsEval(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root %q must be a directory", workspaceRoot)
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	// Resolve daemonRoot the same way to handle symlink differences
	// (macOS /var/folders has symlinks that can cause mismatches).
	resolvedDaemonRoot := ws.daemonRoot
	if evaled, err := filepath.EvalSymlinks(ws.daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}

	if !isWithinWorkspace(workspaceRoot, resolvedDaemonRoot) && workspaceRoot != resolvedDaemonRoot {
		return "", fmt.Errorf("workspace root must stay within daemon root %s", ws.daemonRoot)
	}

	if ws.clientContexts == nil {
		ws.clientContexts = make(map[string]*webClientContext)
	}
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ctx = newWebClientContext(ws.workspaceRoot, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
		ws.clientContexts[clientID] = ctx
	}

	if ctx.Terminal != nil {
		if err := ctx.Terminal.CloseAllSessions(); err != nil {
			return "", fmt.Errorf("close terminal sessions: %w", err)
		}
	}
	if ctx.FileConsents != nil {
		ctx.FileConsents.clearAll()
	}

	ctx.WorkspaceRoot = workspaceRoot
	ctx.SSHHostAlias = ""
	ctx.SSHSessionKey = ""
	ctx.SSHLauncherURL = ""
	ctx.SSHHomePath = ""
	ctx.Terminal = NewTerminalManager(workspaceRoot)
	ws.startTerminalCleanupIfNeeded(ctx.Terminal)
	ctx.Agent = nil
	ctx.AgentState = emptyAgentStateSnapshot()
	ctx.CurrentSessionID = ""
	ctx.ActiveQuery = false
	ctx.CurrentQuery = ""
	// Reset chat sessions on workspace change — keep only the default,
	// which starts fresh.
	ctx.ChatSessions = nil
	ctx.DefaultChatID = ""
	ctx.nextChatNumber = 0
	if ctx.FileConsents == nil {
		ctx.FileConsents = newFileConsentManager()
	}
	ctx.ensureDefaultChatSession()
	ctx.LastSeenAt = time.Now()

	if clientID == defaultWebClientID {
		ws.workspaceRoot = workspaceRoot
		ws.sshHostAlias = ""
		ws.sshSessionKey = ""
		ws.sshLauncherURL = ""
		ws.sshHomePath = ""
		ws.terminalManager = ctx.Terminal
		ws.fileConsents = ctx.FileConsents
	}

	return workspaceRoot, nil
}

func (ws *ReactWebServer) withAgentWorkspace(workspaceRoot string, fn func() error) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return fn()
	}

	ws.workspaceExecMu.Lock()
	defer ws.workspaceExecMu.Unlock()

	originalWD, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current working directory: %w", err)
	}
	if err := os.Chdir(workspaceRoot); err != nil {
		return fmt.Errorf("change working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	return fn()
}

func (ws *ReactWebServer) setAgentStateForClient(clientID string, snapshot []byte) {
	if len(snapshot) == 0 {
		snapshot = emptyAgentStateSnapshot()
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	// Update both the top-level state (backward compat) and the active chat session.
	ctx.setChatSessionState(ctx.getActiveChatID(), snapshot)
	ctx.LastSeenAt = time.Now()
}

func (ws *ReactWebServer) getClientAgent(clientID string) (*agent.Agent, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && ctx.Agent != nil {
		agentInst := ctx.Agent
		workspaceRoot := ctx.WorkspaceRoot
		terminal := ctx.Terminal
		userID := ctx.UserID // Capture before releasing lock
		ws.mutex.RUnlock()
		agentInst.SetWorkspaceRoot(workspaceRoot)
		meta := map[string]interface{}{"client_id": clientID}
		if userID != "" {
			meta["user_id"] = userID
		}
		agentInst.SetEventMetadata(meta)
		agentInst.EnableStreaming(func(string) {})
		agentInst.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
		agentInst.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())
		// Wire the TerminalManager from the client context into the agent for WebUI mode.
		if terminal != nil {
			agentInst.SetTerminalManager(terminal)
		}
		return agentInst, nil
	}
	// Fallback: check if the active chat session has an agent already.
	if ctx := ws.clientContexts[clientID]; ctx != nil && ctx.ChatSessions != nil && ctx.DefaultChatID != "" {
		if cs, ok := ctx.ChatSessions[ctx.DefaultChatID]; ok {
			cs.mu.Lock()
			if cs.Agent != nil {
				agentInst := cs.Agent
				terminal := ctx.Terminal
				userID := ctx.UserID // Capture before releasing lock
				cs.mu.Unlock()
				ctx.Agent = agentInst // cache for next time
				workspaceRoot := ctx.WorkspaceRoot
				ws.mutex.RUnlock()
				agentInst.SetWorkspaceRoot(workspaceRoot)
				meta := map[string]interface{}{"client_id": clientID}
				if userID != "" {
					meta["user_id"] = userID
				}
				agentInst.SetEventMetadata(meta)
				agentInst.EnableStreaming(func(string) {})
				agentInst.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
				agentInst.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())
				// Wire the TerminalManager from the client context into the agent for WebUI mode.
				if terminal != nil {
					agentInst.SetTerminalManager(terminal)
				}
				return agentInst, nil
			}
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	if ctx.Agent != nil {
		agentInst := ctx.Agent
		workspaceRoot := ctx.WorkspaceRoot
		terminal := ctx.Terminal
		userID := ctx.UserID // Capture before releasing lock
		ws.mutex.Unlock()
		agentInst.SetWorkspaceRoot(workspaceRoot)
		meta := map[string]interface{}{"client_id": clientID}
		if userID != "" {
			meta["user_id"] = userID
		}
		agentInst.SetEventMetadata(meta)
		agentInst.EnableStreaming(func(string) {})
		agentInst.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
		agentInst.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())
		// Wire the TerminalManager from the client context into the agent for WebUI mode.
		if terminal != nil {
			agentInst.SetTerminalManager(terminal)
		}
		return agentInst, nil
	}
	workspaceRoot := ctx.WorkspaceRoot
	snapshot := append([]byte(nil), ctx.AgentState...)
	userID := ctx.UserID // Capture before releasing lock
	ws.mutex.Unlock()

	// Fast check: if no provider is configured, return immediately with a
	// sentinel error instead of attempting expensive agent creation.
	// NOTE: A narrow TOCTOU race exists between this config read and the
	// config read inside agent.NewAgentWithModel. Acceptable since the worst
	// case is a single unnecessary retry after the user configures a provider.
	if !isProviderAvailable() {
		return nil, ErrNoProviderConfigured
	}

	var created *agent.Agent
	var createErr error

	// Compute layered config directories: global + workspace (no session file)
	configBase, err := configuration.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config directory: %w", err)
	}

	// Workspace config is in {workspaceRoot}/.sprout/ (if workspace exists)
	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = filepath.Join(workspaceRoot, configuration.ConfigDirName)
		// Auto-bootstrap workspace config when opening a git repo that
		// doesn't have .sprout/config.json yet. Same logic as
		// PersistentPreRunE auto-detection, applied at workspace-switch time.
		gitPath := filepath.Join(workspaceRoot, ".git")
		if info, statErr := os.Stat(gitPath); statErr == nil && info.IsDir() {
			configPath := filepath.Join(workspaceDir, "config.json")
			if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
				if err := configuration.BootstrapIsolatedConfig(workspaceDir); err != nil {
					log.Printf("[debug] daemon workspace: failed to bootstrap isolated config for %s: %v", workspaceRoot, err)
				}
			}
		}
	}

	created, createErr = agent.NewAgentWithLayersInWorkspace(configBase, workspaceDir, workspaceRoot, "")
	if createErr != nil {
		if errors.Is(createErr, agent.ErrModelNotAvailable) || errors.Is(createErr, agent.ErrProviderNotConfigured) {
			return nil, createErr
		}
		return nil, fmt.Errorf("create agent: %w", createErr)
	}

	created.SetEventBus(ws.eventBus)
	created.SetWorkspaceRoot(workspaceRoot)
	// Get chat_id while holding the lock
	ws.mutex.RLock()
	chatID := ""
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		chatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()
	// Build metadata map
	meta := map[string]interface{}{
		"client_id": clientID,
		"chat_id":   chatID,
	}
	if userID != "" {
		meta["user_id"] = userID
	}
	created.SetEventMetadata(meta)
	created.EnableStreaming(func(string) {})
	created.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
	created.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())

	// Wire the TerminalManager from the client context into the agent for WebUI mode.
	// CLI mode does not set this (agent.terminalManager stays nil).
	ws.mutex.Lock()
	if wsCtx := ws.clientContexts[clientID]; wsCtx != nil && wsCtx.Terminal != nil {
		created.SetTerminalManager(wsCtx.Terminal)
	}
	ws.mutex.Unlock()

	if len(snapshot) > 0 {
		if err := created.ImportState(snapshot); err != nil {
			return nil, fmt.Errorf("import agent state: %w", err)
		}
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx = ws.getOrCreateClientContextLocked(clientID)
	if ctx.Agent == nil {
		ctx.Agent = created
		ctx.CurrentSessionID = strings.TrimSpace(created.GetSessionID())
		ctx.LastSeenAt = time.Now()
		// Also store in the active chat session for multi-chat support.
		if activeChatID := ctx.getActiveChatID(); activeChatID != "" {
			if cs := ctx.getChatSession(activeChatID); cs != nil {
				cs.mu.Lock()
				if cs.Agent == nil {
					cs.Agent = created
					cs.CurrentSessionID = ctx.CurrentSessionID
				}
				cs.mu.Unlock()
			}
		}
	}
	return ctx.Agent, nil
}

// getChatAgent returns the agent for a specific chat session, creating one
// lazily if needed. This enables concurrent queries across multiple chats
// since each chat has its own agent instance. Falls back to getClientAgent
// when the chat session infrastructure is not available.
func (ws *ReactWebServer) getChatAgent(clientID, chatID string) (*agent.Agent, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.RUnlock()
		return nil, fmt.Errorf("client context not found")
	}
	if ctx.ChatSessions == nil {
		ws.mutex.RUnlock()
		return ws.getClientAgent(clientID)
	}
	if chatID == "" {
		chatID = ctx.getActiveChatID()
	}
	cs, ok := ctx.ChatSessions[chatID]
	if !ok {
		// Create the chat session if it doesn't exist yet
		ws.mutex.RUnlock()
		ws.mutex.Lock()
		ctx = ws.getOrCreateClientContextLocked(clientID)
		if ctx.ChatSessions == nil {
			ctx.ChatSessions = make(map[string]*chatSession)
		}
		if _, exists := ctx.ChatSessions[chatID]; !exists {
			ctx.ChatSessions[chatID] = &chatSession{
				ID:        chatID,
				Name:      chatID,
				CreatedAt: time.Now(),
			}
		}
		cs = ctx.ChatSessions[chatID]
		ws.mutex.Unlock()
		// Re-acquire read lock for the rest of the function
		ws.mutex.RLock()
		ctx = ws.clientContexts[clientID]
	}
	workspaceRoot := ctx.WorkspaceRoot
	eventBus := ws.eventBus
	terminal := ctx.Terminal
	userID := ctx.UserID // Capture before releasing lock
	ws.mutex.RUnlock()

	// Compute layered config directories: global + workspace (no session file)
	configBase, err := configuration.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config directory: %w", err)
	}
	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = filepath.Join(workspaceRoot, configuration.ConfigDirName)
	}

	// In shared mode (CLI + WebUI in the same process), seed the default
	// chat session with the CLI's agent instance so both frontends share
	// one conversation history, one session, and one state. This bypasses
	// the lazy-create path in getOrCreateAgent.
	if ws.IsSharedMode() && clientID == defaultWebClientID && chatID == defaultChatID {
		if ws.agent != nil && cs.Agent == nil {
			cs.mu.Lock()
			if cs.Agent == nil {
				cs.Agent = ws.agent
			}
			cs.mu.Unlock()
		}
	}

	agentInst, err := cs.getOrCreateAgent(workspaceRoot, configBase, workspaceDir, eventBus, clientID, userID, ws.withAgentWorkspace)
	if err != nil {
		if errors.Is(err, agent.ErrModelNotAvailable) || errors.Is(err, agent.ErrProviderNotConfigured) {
			return nil, err
		}
		return nil, fmt.Errorf("get or create chat agent: %w", err)
	}

	// Wire WebUI-owned managers and client-presence callback so that
	// ask_user, security approvals, and security prompts route through
	// the shared manager instances that the WebSocket handlers resolve
	// responses on. Without this injection, each chat-session agent uses
	// its own default managers and ask_user/approval requests either fall
	// through to stdin (ask_user) or time out (approvals).
	agentInst.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
	agentInst.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())

	// Wire the TerminalManager from the client context into the agent for WebUI mode.
	// CLI mode does not set this (agent.terminalManager stays nil).
	if terminal != nil {
		agentInst.SetTerminalManager(terminal)
	}

	// Keep the client-level Agent in sync with the active chat's agent for
	// backward compatibility with code paths that use getClientAgent.
	if chatID != "" {
		ws.mutex.Lock()
		if ctx := ws.clientContexts[clientID]; ctx != nil && ctx.DefaultChatID == chatID {
			ctx.Agent = agentInst
		}
		ws.mutex.Unlock()
	}

	return agentInst, nil
}
