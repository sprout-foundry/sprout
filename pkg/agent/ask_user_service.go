package agent

import (
	"context"
	"errors"

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
		browser := func(ctx context.Context) (string, error) {
			return tools.AskUserWithEventBus(ctx, req, eventBus, clientID, userID, chatID, askUserMgr)
		}
		if a.askBothSurfaces(req) {
			return askFirstAnswer(ctx, func(ctx context.Context) (string, error) { return tools.AskUser(ctx, req) }, browser)
		}
		a.noteQuestionRoutedToBrowser()
		return browser(ctx)
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

// askBothSurfaces reports whether a question should go to the terminal and
// the connected browser at once, rather than to the browser alone. A
// terminal session with a tab open — often one left open in the background —
// otherwise sent every question to the tab, and the terminal stalled until
// the ask timeout. Credential prompts stay browser-only: the terminal's
// hidden-input read cannot be withdrawn if the browser answers first.
func (a *Agent) askBothSurfaces(req tools.AskUserRequest) bool {
	return !req.Sensitive && !a.isNonInteractive() && !a.IsTaskRun()
}

// askFirstAnswer puts one question on two surfaces and returns the first
// answer, withdrawing the other: the terminal picker closes, the browser
// dialog is cancelled. A surface that cannot ask (ErrAskUserNoChannel) or
// fails leaves the question to the other; a dismissal is the user's answer.
// Both asks have finished — the terminal restored — before it returns.
func askFirstAnswer(ctx context.Context, terminal, browser func(context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		answer string
		err    error
	}
	results := make(chan result, 2)
	for _, ask := range []func(context.Context) (string, error){terminal, browser} {
		go func(ask func(context.Context) (string, error)) {
			answer, err := ask(ctx)
			results <- result{answer, err}
		}(ask)
	}
	var first error
	for pending := 2; pending > 0; pending-- {
		r := <-results
		if r.err == nil || errors.Is(r.err, tools.ErrAskUserDismissed) {
			cancel()
			if pending == 2 {
				<-results // the withdrawn surface finishes tearing down
			}
			return r.answer, r.err
		}
		if first == nil || errors.Is(first, tools.ErrAskUserNoChannel) {
			first = r.err
		}
	}
	return "", first
}
