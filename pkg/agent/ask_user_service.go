package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// agentAskUserService implements tools.AskUserService by delegating to
// the agent's existing webui-routing logic in tool_handlers_interaction.go.
// Constructed per tool dispatch in tool_security.go so it captures the
// current event-bus, client/chat/user IDs, and WebUI-presence state.
type agentAskUserService struct {
	agent *Agent
}

func newAgentAskUserService(a *Agent) tools.AskUserService {
	if a == nil {
		return nil
	}
	return &agentAskUserService{agent: a}
}

func (s *agentAskUserService) Ask(ctx context.Context, req tools.AskUserRequest) (string, error) {
	a := s.agent
	eventBus := a.GetEventBus()
	clientID := a.GetEventClientID()
	userID := a.GetEventUserID()
	chatID := a.GetEventChatID()
	askUserMgr := a.security.GetAskUserMgr()

	hasActiveWebUI := eventBus != nil && askUserMgr != nil && a.HasActiveWebUIClients()
	if a.debug {
		a.Logger().Debug("[ask_user/service] hasActiveWebUI=%v options=%d header=%q\n", hasActiveWebUI, len(req.Options), req.Header)
	}
	if hasActiveWebUI {
		return tools.AskUserWithEventBus(ctx, req, eventBus, clientID, userID, chatID, askUserMgr)
	}
	return tools.AskUser(ctx, req)
}
