package webui

import (
	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// agentSetupConfig holds the parameters for configuring a newly created agent
// in the webui context. EventBus and UserID are skipped when empty.
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

	meta := map[string]interface{}{
		"client_id": cfg.ClientID,
		"chat_id":   cfg.ChatID,
	}
	if cfg.UserID != "" {
		meta["user_id"] = cfg.UserID
	}
	a.SetEventMetadata(meta)
	a.EnableStreaming(func(string) {})
}
