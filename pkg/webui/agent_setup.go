//go:build !js

package webui

import (
	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// agentSetupConfig holds the parameters for configuring an agent in the webui
// context. EventBus and UserID are skipped when empty.
type agentSetupConfig struct {
	EventBus      *events.EventBus
	WorkspaceRoot string
	ClientID      string
	ChatID        string
	UserID        string
}

// setupWebUIAgent applies the standard configuration every webui agent needs:
// EventBus, SlashCommands registry, WorkspaceRoot, event metadata for routing,
// and streaming support. This replaces the copy-pasted setter sequences in
// chat_sessions.go and client_context.go.
//
// Call this immediately after agent.NewAgentWithLayersInWorkspace (or similar
// agent constructors). The caller owns error handling for the constructor;
// this function does not return an error because none of the setters can fail.
func setupWebUIAgent(a *agent.Agent, cfg agentSetupConfig) {
	if cfg.EventBus != nil {
		a.SetEventBus(cfg.EventBus)
	}
	a.SetSlashCommands(agent_commands.NewCommandRegistry())
	a.SetWorkspaceRoot(cfg.WorkspaceRoot)
	applyWebUIAgentMetadata(a, cfg)
	a.EnableStreaming(func(string) {})
}

// rearmWebUIAgent re-applies the per-request state to an existing agent being
// reused from a prior query. Unlike setupWebUIAgent, it does NOT reset
// SlashCommands or EventBus — those are set once at creation and should not
// be re-applied to a live agent. If ws is nil, server-level wiring
// (HasActiveWebUIClients, InjectWebUIManagers) is skipped.
func rearmWebUIAgent(a *agent.Agent, ws *ReactWebServer, cfg agentSetupConfig) {
	a.SetWorkspaceRoot(cfg.WorkspaceRoot)
	applyWebUIAgentMetadata(a, cfg)
	a.EnableStreaming(func(string) {})
	if ws != nil {
		a.SetHasActiveWebUIClients(ws.HasActiveWebUIClients)
		a.InjectWebUIManagers(ws.GetSecurityPromptMgr(), ws.GetAskUserMgr())
	}
}

// applyWebUIAgentMetadata sets the event routing metadata (client_id, chat_id,
// user_id) on the agent. Shared between setup and rearm paths.
//
// A rearm that names no chat keeps the agent's chat: each agent belongs to
// one chat, and client-level rearms (getClientAgent, reached from provider,
// stats and settings requests) don't carry one. Clearing it — even mid-run —
// made the agent's events arrive without a chat_id, which the UI applies to
// whichever chat is on screen.
func applyWebUIAgentMetadata(a *agent.Agent, cfg agentSetupConfig) {
	chatID := cfg.ChatID
	if chatID == "" {
		chatID = a.GetChatID()
	}
	meta := map[string]interface{}{
		"client_id": cfg.ClientID,
		"chat_id":   chatID,
	}
	if cfg.UserID != "" {
		meta["user_id"] = cfg.UserID
	}
	a.SetEventMetadata(meta)
}
