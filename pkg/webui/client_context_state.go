//go:build !js

package webui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// SyncSharedAgentState exports the shared agent's state (conversation history,
// session ID, etc.) into the WebUI's default chat session. Called by the CLI's
// ProcessQuery wrapper after each CLI query completes, so the browser tab has
// fresh history when it reconnects or refreshes.
//
// Only meaningful in shared-agent mode (ws.agent != nil). In daemon mode this
// is a no-op because each chat manages its own agent independently.
func (ws *ReactWebServer) SyncSharedAgentState(agentInst *agent.Agent) error {
	if !ws.IsSharedMode() || agentInst == nil {
		return nil
	}
	snapshot, err := agentInst.ExportState()
	if err != nil {
		return fmt.Errorf("export agent state: %w", err)
	}
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
	ctx.ensureDefaultChatSession()
	if cs, ok := ctx.ChatSessions[defaultChatID]; ok {
		cs.mu.Lock()
		cs.AgentState = append([]byte(nil), snapshot...)
		cs.LastActiveAt = time.Now()
		cs.mu.Unlock()
	}
	ctx.AgentState = append([]byte(nil), snapshot...)
	ctx.LastSeenAt = time.Now()
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

// resolveWorkspaceRootForChat returns the appropriate workspace root for a given chat session.
// If the chat session has a worktree path set, it returns that. Otherwise, it returns the
// client context's workspace root.
func (ws *ReactWebServer) resolveWorkspaceRootForChat(clientID, chatID string) string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		return ""
	}
	wtPath := ctx.getChatSessionWorktree(chatID)
	if wtPath != "" {
		return wtPath
	}
	return ctx.WorkspaceRoot
}

// userIDForClient safely retrieves the UserID for a given clientID.
// Returns empty string if the client context doesn't exist or has no UserID.
func (ws *ReactWebServer) userIDForClient(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return ""
	}

	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	if ctx := ws.clientContexts[clientID]; ctx != nil {
		return ctx.UserID
	}
	return ""
}
