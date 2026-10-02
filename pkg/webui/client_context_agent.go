//go:build !js

package webui

// client_context_agent.go — the agent-state persistence + agent-acquisition
// path for client contexts (setAgentStateForClient/Chat, getClientAgent,
// getChatAgent), split out of client_context.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

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

// setAgentStateForClientChat writes a state snapshot into ONE chat session.
// Unlike setAgentStateForClient it never redirects to the active chat when
// the caller asked for a specific one, and it does NOT clobber the
// top-level AgentState/CurrentSessionID when the target is a background
// chat — setChatSessionState updates those unconditionally, which would
// misreport the client's current session (e.g. /api/sessions) after a
// restore into a non-active chat. The top-level slots update only when the
// target IS the active/default chat.
func (ws *ReactWebServer) setAgentStateForClientChat(clientID, chatID string, snapshot []byte) {
	if len(snapshot) == 0 {
		snapshot = emptyAgentStateSnapshot()
	}

	sessionID := ""
	var state agent.AgentState
	if err := json.Unmarshal(snapshot, &state); err == nil {
		sessionID = strings.TrimSpace(state.SessionID)
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	if ctx.ChatSessions == nil {
		// No chat-session map (legacy single-chat context): the only state
		// slot IS the top level.
		ctx.AgentState = append([]byte(nil), snapshot...)
		ctx.CurrentSessionID = sessionID
		ctx.LastSeenAt = time.Now()
		return
	}
	if chatID == "" {
		chatID = ctx.DefaultChatID
	}
	if cs, ok := ctx.ChatSessions[chatID]; ok {
		cs.mu.Lock()
		cs.AgentState = append([]byte(nil), snapshot...)
		cs.CurrentSessionID = sessionID
		cs.LastActiveAt = time.Now()
		cs.mu.Unlock()
	}
	if ctx.getActiveChatID() == chatID {
		ctx.AgentState = append([]byte(nil), snapshot...)
		ctx.CurrentSessionID = sessionID
	}
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
		rearmWebUIAgent(agentInst, ws, agentSetupConfig{
			WorkspaceRoot: workspaceRoot,
			ClientID:      clientID,
			UserID:        userID,
		})
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
				rearmWebUIAgent(agentInst, ws, agentSetupConfig{
					WorkspaceRoot: workspaceRoot,
					ClientID:      clientID,
					UserID:        userID,
				})
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
		rearmWebUIAgent(agentInst, ws, agentSetupConfig{
			WorkspaceRoot: workspaceRoot,
			ClientID:      clientID,
			UserID:        userID,
		})
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
	if !isProviderAvailableInWorkspace(workspaceRoot) {
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
		workspaceDir = configuration.WorkspaceConfigDir(workspaceRoot)
		// Ensure the workspace .sprout/ dir exists with a .gitignore
		// covering personal overrides and state directories.
		if err := configuration.EnsureWorkspaceConfigDir(workspaceRoot); err != nil {
			ws.log().Warn("failed to ensure workspace config dir", slog.String("workspace_root", workspaceRoot), slog.Any("err", err))
		}
		// Auto-bootstrap workspace config when opening a git repo that
		// doesn't have .sprout/config.json yet. Same logic as
		// PersistentPreRunE auto-detection, applied at workspace-switch time.
		gitPath := filepath.Join(workspaceRoot, ".git")
		if info, statErr := os.Stat(gitPath); statErr == nil && info.IsDir() {
			configPath := filepath.Join(workspaceDir, "config.json")
			if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
				if err := configuration.BootstrapIsolatedConfig(workspaceDir); err != nil {
					ws.log().Warn("isolated daemon workspace configuration bootstrap failed", slog.String("workspace_root", workspaceRoot), slog.Any("err", err))
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

	ws.mutex.RLock()
	chatID := ""
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		chatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	setupWebUIAgent(created, agentSetupConfig{
		EventBus:      ws.eventBus,
		WorkspaceRoot: workspaceRoot,
		ClientID:      clientID,
		ChatID:        chatID,
		UserID:        userID,
	})
	created.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
	created.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())
	ws.watchWakeupTurns(created, clientID, chatID)

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
	if ctx.Agent != nil {
		// Lost the creation race. `created` is fully constructed — its MCP
		// servers and background watchers are already running — so it must
		// be shut down, not just dropped on the floor.
		ws.releaseAgents("agent_creation_race", created)
	}
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
		workspaceDir = configuration.WorkspaceConfigDir(workspaceRoot)
	}

	// In shared mode (CLI + WebUI in the same process), seed the default
	// chat session with the CLI's agent instance so both frontends share
	// one conversation history, one session, and one state. This bypasses
	// the lazy-create path in getOrCreateAgent.
	//
	// SP-136 isolation guarantee: the seed is gated on BOTH
	// clientID == defaultWebClientID AND chatID == defaultChatID.
	// Therefore a WebUI session for a different workspace (which uses a
	// non-default clientID) never receives the CLI agent.  Furthermore,
	// even if the CLI agent IS seeded, getOrCreateAgent calls
	// rearmWebUIAgent which re-sets the workspace root to the context's
	// WorkspaceRoot — preventing workspace-A agents from leaking into
	// workspace-B sessions.
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
	ws.watchWakeupTurns(agentInst, clientID, chatID)

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
