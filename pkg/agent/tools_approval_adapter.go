package agent

import (
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// toolsApprovalAdapter wraps the agent's security.ApprovalManager and event
// bus, translating calls from the tools.ApprovalManager interface to the
// security package's signature.
//
// Approval surfaces, in order:
//  1. WebUI dialog via the event bus — when a browser client is connected.
//  2. CLI prompt — when the bus went unanswered and an interactive terminal
//     is attached. Without this fallback, CLI-only sessions publish to a bus
//     nobody is listening on, block for the full approval timeout, and
//     surface a confusing "timed_out" rejection.
//  3. Legacy bus wait — headless runs preserve the previous behavior.
type toolsApprovalAdapter struct {
	agent       *Agent
	approvalMgr *security.ApprovalManager
	eventBus    *events.EventBus
	clientID    string
	userID      string

	// cliPrompt overrides the interactive CLI prompt (test seam). Non-nil
	// also marks the CLI surface as available without touching global
	// logger state.
	cliPrompt func(prompt, command string) utils.ApprovalChoice
}

// newToolsApprovalAdapter creates a tools.ApprovalManager backed by the
// agent's security approval manager and event bus.
func newToolsApprovalAdapter(agent *Agent) tools.ApprovalManager {
	if agent == nil {
		return nil
	}
	mgr := agent.GetSecurityApprovalMgr()
	if mgr == nil {
		return nil
	}
	return &toolsApprovalAdapter{
		agent:       agent,
		approvalMgr: mgr,
		eventBus:    agent.GetEventBus(),
		clientID:    agent.GetEventClientID(),
		userID:      agent.GetEventUserID(),
	}
}

// RequestApproval implements tools.ApprovalManager.
//
// requestID is intentionally ignored — the security layer generates its own.
func (a *toolsApprovalAdapter) RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) tools.ApprovalResult {
	if extras == nil {
		extras = map[string]string{}
	}

	// Surface 1: WebUI dialog, only when a browser client might answer.
	// An unanswered dialog (timeout/disconnect) is remembered but not
	// returned yet — the CLI below gets a chance to answer first.
	var busFallback tools.ApprovalResult
	triedBus := false
	if a.preferWebUI() {
		triedBus = true
		decision, outcome := a.approvalMgr.RequestApprovalDecisionWithOutcome(a.eventBus, a.newRequest(toolName, riskLevel, prompt, extras))
		if outcome == security.ApprovalOutcomeResponded {
			return approvalResultFrom(decision, outcome)
		}
		busFallback = approvalResultFrom(decision, outcome)
	}

	// Surface 2: interactive CLI prompt.
	if a.cliAvailable() {
		choice := a.cliPromptFn()(prompt, extras["command"])
		decision := approvalDecisionFromCLIChoice(choice)
		if a.agent != nil && (decision == security.ApprovalApproveAlways || decision == security.ApprovalElevate) {
			a.agent.applyApprovalDecision(decision, extras["command"])
		}
		return approvalResultFrom(decision, security.ApprovalOutcomeResponded)
	}

	// Surface 3: legacy publish-and-wait for runs with no interactive
	// surface at all (headless, subagents). Skipped when the bus was
	// already tried above — re-publishing would block a second full
	// timeout window for the same unanswered question.
	if !triedBus && a.eventBus != nil {
		decision, outcome := a.approvalMgr.RequestApprovalDecisionWithOutcome(a.eventBus, a.newRequest(toolName, riskLevel, prompt, extras))
		return approvalResultFrom(decision, outcome)
	}
	if triedBus {
		return busFallback
	}
	return tools.ApprovalResult{Approved: false, Reason: "no_channel"}
}

// preferWebUI reports whether the event-bus dialog should be tried first:
// a bus exists, this is not a subagent, and a WebUI client is connected.
func (a *toolsApprovalAdapter) preferWebUI() bool {
	return a.eventBus != nil && a.agent != nil &&
		!a.agent.IsSubagent() && a.agent.HasActiveWebUIClients()
}

// cliAvailable reports whether an interactive CLI prompt can be shown.
// False in headless mode, subagents, and skip-prompt configs. The
// subagent guard is checked before the test seam so stubbed prompts
// can never leak into subagent flows.
func (a *toolsApprovalAdapter) cliAvailable() bool {
	if a.agent == nil || a.agent.IsSubagent() {
		return false
	}
	// cliPrompt is the test seam: an injected stub stands in for an
	// attached interactive terminal, independent of agent state, and
	// bypasses the SkipPrompt check so tests can simulate the
	// interactive path on isolated agents (which set SkipPrompt=true).
	if a.cliPrompt != nil {
		return true
	}
	cfg := a.agent.GetConfig()
	if cfg != nil && cfg.SkipPrompt {
		return false
	}
	logger := a.logger()
	return logger != nil && logger.IsInteractive()
}

// cliPromptFn returns the terminal prompt implementation. Tests inject a
// stub via the adapter's cliPrompt field.
func (a *toolsApprovalAdapter) cliPromptFn() func(prompt, command string) utils.ApprovalChoice {
	if a.cliPrompt != nil {
		return a.cliPrompt
	}
	return func(prompt, command string) utils.ApprovalChoice {
		return a.logger().AskForApprovalWithOptions(prompt, command, nil)
	}
}

// logger returns the shared CLI logger with interactivity driven by the
// agent's SkipPrompt config — GetLogger re-applies the flag globally, so
// passing a hardcoded false would re-enable prompts in headless runs.
func (a *toolsApprovalAdapter) logger() *utils.Logger {
	skip := false
	if a.agent != nil {
		if cfg := a.agent.GetConfig(); cfg != nil {
			skip = cfg.SkipPrompt
		}
	}
	return utils.GetLogger(skip)
}

func (a *toolsApprovalAdapter) newRequest(toolName, riskLevel, prompt string, extras map[string]string) security.ApprovalRequest {
	return security.ApprovalRequest{
		Kind:            security.ApprovalKindTool,
		DefaultResponse: false,
		ToolName:        toolName,
		RiskLevel:       riskLevel,
		Reasoning:       prompt,
		ClientID:        a.clientID,
		UserID:          a.userID,
		Extras:          extras,
	}
}

// approvalResultFrom converts the security layer's decision+outcome to a
// tools.ApprovalResult.
func approvalResultFrom(decision security.ApprovalDecision, outcome security.ApprovalOutcome) tools.ApprovalResult {
	reason := ""
	if !decision.Approved() {
		switch outcome {
		case security.ApprovalOutcomeTimedOut:
			reason = "timed_out"
		case security.ApprovalOutcomeNoChannel:
			reason = "no_channel"
		default:
			reason = "rejected"
		}
	}
	return tools.ApprovalResult{
		Approved: decision.Approved(),
		Reason:   reason,
	}
}
