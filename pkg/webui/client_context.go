package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
)

const (
	webClientIDHeader     = "X-Ledit-Client-ID"
	webClientIDQueryParam = "client_id"
	defaultWebClientID    = "default"
)

type webClientContext struct {
	WorkspaceRoot    string
	SSHHostAlias     string
	SSHSessionKey    string
	SSHLauncherURL   string
	SSHHomePath      string
	Terminal         *TerminalManager
	FileConsents     *fileConsentManager
	Agent            *agent.Agent
	AgentState       []byte
	CurrentSessionID string
	CurrentQuery    string
	ActiveQuery      bool
	LastSeenAt       time.Time

	// Multi-chat support: one client context (tab) can have multiple
	// independent chat sessions, each with its own agent state.
	ChatSessions  map[string]*chatSession
	DefaultChatID string
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

func (ws *ReactWebServer) resolveClientID(r *http.Request) string {
	if r == nil {
		return defaultWebClientID
	}
	clientID := strings.TrimSpace(r.Header.Get(webClientIDHeader))
	if clientID == "" {
		clientID = strings.TrimSpace(r.URL.Query().Get(webClientIDQueryParam))
	}
	if clientID == "" {
		clientID = defaultWebClientID
	}
	return clientID
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

	var ctx *webClientContext
	if clientID == defaultWebClientID {
		ctx = &webClientContext{
			WorkspaceRoot:  ws.workspaceRoot,
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
		}
		if ctx.FileConsents == nil {
			ctx.FileConsents = newFileConsentManager()
			ws.fileConsents = ctx.FileConsents
		}
		ctx.ensureDefaultChatSession()
	} else {
		ctx = newWebClientContext(ws.workspaceRoot, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
	}

	ws.clientContexts[clientID] = ctx
	return ctx
}

func (ws *ReactWebServer) getClientContextForRequest(r *http.Request) *webClientContext {
	return ws.getOrCreateClientContext(ws.resolveClientID(r))
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

func (ws *ReactWebServer) getWorkspaceRootForRequest(r *http.Request) string {
	return ws.getClientContextForRequest(r).WorkspaceRoot
}

func (ws *ReactWebServer) getTerminalManagerForRequest(r *http.Request) *TerminalManager {
	return ws.getClientContextForRequest(r).Terminal
}

func (ws *ReactWebServer) getFileConsentManagerForRequest(r *http.Request) *fileConsentManager {
	return ws.getClientContextForRequest(r).FileConsents
}

func (ws *ReactWebServer) getCurrentSessionIDForRequest(r *http.Request) string {
	return ws.getClientContextForRequest(r).CurrentSessionID
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
		return "", fmt.Errorf("workspace root must be a directory")
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if !isWithinWorkspace(workspaceRoot, ws.daemonRoot) && workspaceRoot != ws.daemonRoot {
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
		ws.mutex.RUnlock()
		agentInst.SetWorkspaceRoot(workspaceRoot)
		agentInst.SetEventMetadata(map[string]interface{}{"client_id": clientID})
		agentInst.EnableStreaming(func(string) {})
		return agentInst, nil
	}
	ws.mutex.RUnlock()

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	if ctx.Agent != nil {
		agentInst := ctx.Agent
		workspaceRoot := ctx.WorkspaceRoot
		ws.mutex.Unlock()
		agentInst.SetWorkspaceRoot(workspaceRoot)
		agentInst.SetEventMetadata(map[string]interface{}{"client_id": clientID})
		agentInst.EnableStreaming(func(string) {})
		return agentInst, nil
	}
	// Fallback: check if the active chat session has an agent already.
	if ctx.ChatSessions != nil && ctx.DefaultChatID != "" {
		if cs, ok := ctx.ChatSessions[ctx.DefaultChatID]; ok {
			cs.mu.Lock()
			if cs.Agent != nil {
				agentInst := cs.Agent
				cs.mu.Unlock()
				ctx.Agent = agentInst // cache for next time
				workspaceRoot := ctx.WorkspaceRoot
				ws.mutex.Unlock()
				agentInst.SetWorkspaceRoot(workspaceRoot)
				agentInst.SetEventMetadata(map[string]interface{}{"client_id": clientID})
				agentInst.EnableStreaming(func(string) {})
				return agentInst, nil
			}
			cs.mu.Unlock()
		}
	}
	workspaceRoot := ctx.WorkspaceRoot
	// Use the active chat session's state for the new agent.
	// Falls back to top-level AgentState for backward compat.
	snapshot := append([]byte(nil), ctx.getChatSessionState(ctx.getActiveChatID())...)
	ws.mutex.Unlock()

	var created *agent.Agent
	var createErr error
	err := ws.withAgentWorkspace(workspaceRoot, func() error {
		created, createErr = agent.NewAgentWithModel("")
		return createErr
	})
	if err != nil {
		return nil, err
	}
	if createErr != nil {
		return nil, createErr
	}

	created.SetEventBus(ws.eventBus)
	created.SetWorkspaceRoot(workspaceRoot)
	created.SetEventMetadata(map[string]interface{}{"client_id": clientID, "chat_id": ctx.getActiveChatID()})
	created.EnableStreaming(func(string) {})
	if len(snapshot) > 0 {
		if err := created.ImportState(snapshot); err != nil {
			return nil, err
		}
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx = ws.getOrCreateClientContextLocked(clientID)
	if ctx.Agent == nil {
		ctx.Agent = created
		ctx.CurrentSessionID = strings.TrimSpace(created.GetSessionID())
		ctx.LastSeenAt = time.Now()
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
		ws.mutex.RUnlock()
		return ws.getClientAgent(clientID)
	}
	workspaceRoot := ctx.WorkspaceRoot
	eventBus := ws.eventBus
	ws.mutex.RUnlock()

	agentInst, err := cs.getOrCreateAgent(workspaceRoot, eventBus, clientID)
	if err != nil {
		return nil, err
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

func (ws *ReactWebServer) syncAgentStateForClient(clientID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	agentInst, err := ws.getClientAgent(clientID)
	if err != nil {
		return err
	}

	snapshot, err := agentInst.ExportState()
	if err != nil {
		return err
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	// Sync to the active chat session as well as the top-level state.
	ctx.setChatSessionState(ctx.getActiveChatID(), snapshot)
	ctx.LastSeenAt = time.Now()
	if clientID == defaultWebClientID {
		ws.workspaceRoot = ctx.WorkspaceRoot
	}
	return nil
}

func (ws *ReactWebServer) setClientQueryActive(clientID string, active bool) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ActiveQuery = active
}

func (ws *ReactWebServer) hasActiveQueryForClient(clientID string) bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	ctx := ws.clientContexts[clientID]
	return ctx != nil && ctx.ActiveQuery
}

func (ws *ReactWebServer) cleanupInactiveClientContexts(maxIdle time.Duration) int {
	if maxIdle <= 0 {
		return 0
	}

	now := time.Now()
	connectedClientIDs := make(map[string]struct{})
	ws.connections.Range(func(_, value interface{}) bool {
		info, ok := value.(*ConnectionInfo)
		if !ok || info == nil {
			return true
		}
		if clientID := strings.TrimSpace(info.ClientID); clientID != "" {
			connectedClientIDs[clientID] = struct{}{}
		}
		return true
	})

	type staleContext struct {
		id       string
		terminal *TerminalManager
	}

	stale := make([]staleContext, 0)

	ws.mutex.Lock()
	for clientID, ctx := range ws.clientContexts {
		if clientID == defaultWebClientID || ctx == nil {
			continue
		}
		if _, connected := connectedClientIDs[clientID]; connected {
			continue
		}
		if ctx.ActiveQuery {
			continue
		}
		if ctx.LastSeenAt.IsZero() || now.Sub(ctx.LastSeenAt) < maxIdle {
			continue
		}
		delete(ws.clientContexts, clientID)
		stale = append(stale, staleContext{id: clientID, terminal: ctx.Terminal})
	}
	ws.lastClientContextCleanupAt = now
	ws.lastClientContextCleanupRemoved = len(stale)
	ws.totalClientContextsRemoved += len(stale)
	ws.mutex.Unlock()

	for _, clientCtx := range stale {
		if clientCtx.terminal != nil {
			_ = clientCtx.terminal.CloseAllSessions()
		}
	}

	return len(stale)
}

func (ws *ReactWebServer) startClientContextCleanupWorker(ctx context.Context, interval, maxIdle time.Duration) {
	if interval <= 0 || maxIdle <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ws.cleanupInactiveClientContexts(maxIdle)
		}
	}
}
