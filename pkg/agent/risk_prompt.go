package agent

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// webuiHighRiskRiskClass is the risk-class label sent to the WebUI
// approval dialog for cascade-gated high-risk operations. It mirrors
// the "CAUTION" label that the filesystem and git approval flows use
// so the browser dialog renders consistently across all three.
const webuiHighRiskRiskClass = "CAUTION"

// highRiskApprovedForCommand decides whether a high-risk shell command
// is permitted to execute. Resolution (SP-058 v2):
//
//  1. SUBAGENT (subagentDepth > 0): auto-approve. The root agent
//     accepted responsibility when it spawned the subagent — its
//     persona's system prompt (notably EA) is responsible for
//     reasoning about delegations. Routing the prompt to the user
//     mid-orchestration would break autonomous workflows. The
//     Critical tier is caught earlier in EvaluateOperationRisk and
//     never reaches this path, so subagents still can't approve
//     truly catastrophic operations (rm -rf /, fork bombs).
//
//  2. ROOT + interactive: show the command to the user via
//     AskForConfirmation. If approved, allow execution. If rejected
//     (or stdin unavailable), return false and let the caller
//     surface a security error.
//
//  3. ROOT + non-interactive: there's no one to ask, so reject.
//     Workflows / automation must use --risk-profile=permissive (or
//     unrestricted) to allow these operations through; that's a
//     deliberate opt-in.
//
// Note that the EA persona, when running AS the root agent, follows
// path 2 or 3 like any other persona. EA's autonomy only kicks in
// at path 1 — when it spawns subagents that hit high-risk gates,
// those auto-approve because EA delegated.
//
// The ctx is reserved for future cancellation hooks; AskForConfirmation
// doesn't currently accept one but we want the signature ready for
// when it does.
func (a *Agent) highRiskApprovedForCommand(ctx context.Context, command string) bool {
	_ = ctx

	// Subagents inherit root authority. The root accepted
	// responsibility by spawning them; we don't ping-pong every
	// high-risk op back to the user.
	if a.IsSubagent() {
		return true
	}

	// WebUI path: when a browser tab is connected and the security
	// approval manager is wired up, route the prompt through the
	// event bus so the dialog renders in the browser. Mirrors the
	// pattern in gitApprovalPrompterAdapter and handleFileSecurityError.
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && a.HasActiveWebUIClients() {
		prompt := fmt.Sprintf("High-risk shell command:\n  %s", command)
		extras := map[string]string{
			"risk_type": "Shell command — persona risk cascade",
			"command":   command,
		}
		return mgr.RequestToolApproval(
			a.GetEventBus(),
			a.GetEventClientID(),
			a.GetEventUserID(),
			"shell_command",
			webuiHighRiskRiskClass,
			prompt,
			extras,
		)
	}

	// Terminal / stdin path.
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	if logger == nil || !logger.IsInteractive() {
		// No interactive surface — refuse silently and let the
		// caller surface a security error. Workflows / CI runs that
		// need these operations should select a more permissive
		// profile (or `unrestricted` for sandboxed targets).
		return false
	}

	prompt := fmt.Sprintf("⚠  High-risk operation\n\nCommand:\n  %s\n\nAllow this command to run?", command)
	return logger.AskForConfirmation(prompt, false, false)
}
