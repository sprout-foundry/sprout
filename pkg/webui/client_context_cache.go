//go:build !js

package webui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// clearCachedAgent removes the cached agent from both the client context
// and its active chat session. Used after a config change (e.g. switching
// away from "editor" mode) so the next agent access creates a fresh agent
// with the updated provider.
func (ws *ReactWebServer) clearCachedAgent(clientID string) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		return
	}
	ctx.Agent = nil

	// Also clear from all chat sessions so per-session agents are recreated.
	for _, cs := range ctx.ChatSessions {
		if cs != nil {
			cs.mu.Lock()
			cs.Agent = nil
			cs.mu.Unlock()
		}
	}
}

func (ws *ReactWebServer) syncAgentStateForClient(clientID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	agentInst, err := ws.getClientAgent(clientID)
	if err != nil {
		// If no provider is configured, that's expected — just return.
		if errors.Is(err, ErrNoProviderConfigured) {
			return nil
		}
		return fmt.Errorf("get client agent for state sync: %w", err)
	}

	snapshot, err := agentInst.ExportState()
	if err != nil {
		return fmt.Errorf("export agent state: %w", err)
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

// startTerminalCleanupIfNeeded starts the idle-session cleanup worker for a
// per-client TerminalManager. The server-level TM gets its worker during
// Start(); per-client TMs created later (e.g. via setClientWorkspaceRoot or
// new non-default client contexts) need their own worker to prevent PTY
// process leaks from idle hidden sessions.
//
// Safe to call while holding ws.mutex — reads ws.serverCtx via atomic.Value.
func (ws *ReactWebServer) startTerminalCleanupIfNeeded(tm *TerminalManager) {
	if tm == nil {
		return
	}
	val := ws.serverCtx.Load()
	if val == nil {
		return
	}
	ctx, ok := val.(context.Context)
	if !ok || ctx == nil {
		return
	}
	// Same intervals as the server-level worker: every 5 min, 30-min timeout, 2-hr for background.
	tm.StartCleanupWorker(ctx, 5*time.Minute, 30*time.Minute, 2*time.Hour)
}
