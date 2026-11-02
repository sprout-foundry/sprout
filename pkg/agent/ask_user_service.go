package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
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
	// Emit the progress_question (plan context) before the ask
	// channel is engaged, so it precedes the ask_user_request for this
	// question.
	a.publishProgressQuestion(req)
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
		a.noteQuestionRoutedToBrowser()
		return tools.AskUserWithEventBus(ctx, req, eventBus, clientID, userID, chatID, askUserMgr)
	}
	// A task has no terminal: without a browser there is nobody to answer,
	// and blocking would stall the task for the ask timeout.
	if a.IsTaskRun() {
		return "", taskAskUserError()
	}
	return tools.AskUser(ctx, req)
}

// noteQuestionRoutedToBrowser tells a terminal user that the question went
// to a connected browser tab. The terminal deliberately does not render the
// question itself (two surfaces racing for one answer), so without this
// the turn just stops — for as long as the ask timeout — with no sign of
// why. A tab left open in the background is the usual cause.
func (a *Agent) noteQuestionRoutedToBrowser() {
	if a.isNonInteractive() {
		return
	}
	console.PrintLine(console.GlyphInfo.Prefix() +
		"The agent asked a question in the browser tab connected to this session — answer it there.\n")
}
