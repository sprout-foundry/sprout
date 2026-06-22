package agent

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// getApprovalLogWriter returns the writer for non-fatal approval-side-effect
// messages (allowlist persist failures, elevation notices). Defaults to
// stderr; tests can swap via the unexported var below.
var approvalLogWriter io.Writer = os.Stderr

func getApprovalLogWriter() io.Writer { return approvalLogWriter }

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
	// Persistent allowlist: if the user previously chose "Always approve
	// this command" for this exact string, skip the prompt entirely.
	if a.IsShellCommandAllowlisted(command) {
		return true
	}

	// Subagents inherit root authority. The root accepted
	// responsibility by spawning them; we don't ping-pong every
	// high-risk op back to the user.
	if a.IsSubagent() {
		return true
	}

	// WebUI path: when a browser tab is connected and the security
	// approval manager is wired up, route the prompt through the
	// event bus so the dialog renders in the browser. The 4-option
	// dialog returns an ApprovalDecision; we act on ApproveAlways and
	// Elevate locally before reporting back to the caller.
	//
	// Interactive only — non-interactive runs never wait on a browser
	// dialog (permissive-by-default; see isNonInteractive).
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && a.HasActiveWebUIClients() && !a.isNonInteractive() {
		prompt := fmt.Sprintf("High-risk shell command:\n  %s", command)
		extras := map[string]string{
			"risk_type":     "Shell command — persona risk cascade",
			"command":       command,
			"allow_options": "true", // signals the frontend to render the 4-button dialog
		}
		decision, outcome := mgr.RequestToolApprovalDecisionWithOutcome(
			a.GetEventBus(),
			a.GetEventClientID(),
			a.GetEventUserID(),
			"shell_command",
			webuiHighRiskRiskClass,
			prompt,
			extras,
		)
		// Only honor the browser's answer when the user actually responded.
		// On timeout / disconnect, fall through to the terminal prompt below
		// instead of denying — an unattended tab shouldn't block a user who's
		// at the CLI.
		if outcome == security.ApprovalOutcomeResponded {
			a.applyApprovalDecision(decision, command)
			return decision.Approved()
		}
	}

	// Terminal / stdin path.
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	if logger == nil || !logger.IsInteractive() {
		// Non-interactive: no one to ask. Permissive-by-default — allow
		// the operation. Critical-tier commands are caught earlier (in
		// tool_handlers_shell.go via EvaluateOperationRisk) and never
		// reach this point, so this only governs High-risk commands,
		// which are safe to auto-approve in a sandboxed non-interactive run.
		if a.isNonInteractive() {
			if a.debug {
				a.debugLog("[non-interactive] auto-approving high-risk shell command (no interactive surface): %s\n", command)
			}
			return true
		}
		// Interactive but no usable terminal surface — refuse and let the
		// caller surface a security error.
		return false
	}

	// No leading glyph — the picker renderer (pkg/console.writeSecurityHeader)
	// prepends the ⚠. Adding one here produced a doubled "⚠ ⚠".
	prompt := "High-risk operation — your active risk profile gates this command."
	choice := logger.AskForApprovalWithOptions(prompt, command)
	decision := approvalDecisionFromCLIChoice(choice)
	a.applyApprovalDecision(decision, command)
	return decision.Approved()
}

// approvalDecisionFromCLIChoice maps the CLI prompt's typed choice onto
// the shared security.ApprovalDecision so the post-prompt handling is
// the same regardless of input surface.
func approvalDecisionFromCLIChoice(c utils.ApprovalChoice) security.ApprovalDecision {
	switch c {
	case utils.ApprovalChoiceApproveOnce:
		return security.ApprovalApproveOnce
	case utils.ApprovalChoiceApproveAlways:
		return security.ApprovalApproveAlways
	case utils.ApprovalChoiceElevate:
		return security.ApprovalElevate
	default:
		return security.ApprovalDeny
	}
}

// applyApprovalDecision performs the side-effects of the user's choice:
// ApproveAlways persists the command to the allowlist; Elevate bumps the
// session profile to permissive and prints a hint about /risk-profile
// for permanent change. ApproveOnce and Deny have no side-effects beyond
// the caller's approve/reject branching.
func (a *Agent) applyApprovalDecision(decision security.ApprovalDecision, command string) {
	switch decision {
	case security.ApprovalApproveAlways:
		if err := a.PersistShellCommandAllowlist(command); err != nil {
			// Surface but don't block — the user still gets one-time
			// approval; persistence failure just means future runs
			// will re-prompt.
			fmt.Fprintf(getApprovalLogWriter(), "[approval] failed to persist allowlist entry: %v\n", err)
		}
	case security.ApprovalElevate:
		a.ElevateSessionToPermissive()
		fmt.Fprintln(getApprovalLogWriter(), "[approval] session risk profile elevated to 'permissive'. Run /risk-profile permissive to make this persistent across restarts.")
	}
}
