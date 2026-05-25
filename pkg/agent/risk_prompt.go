package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

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

// recentApprovalTTL bounds how long a Gate 1 approval stays valid for
// Gate 2 consumption. Both gates fire as part of the same tool dispatch,
// so 30s is generously above any normal latency while still small enough
// that a stale entry doesn't silently approve a future invocation if the
// downstream consumer is somehow skipped.
const recentApprovalTTL = 30 * time.Second

// markShellCommandApproved records that the user just approved this
// shell command at Gate 1 (the static classifier in tool_security.go
// or the seed pre-execute hook). Gate 2 (highRiskApprovedForCommand)
// consumes the entry so the user isn't re-prompted for the same
// execution. See agent.go:recentlyApprovedShellCommands for context.
func (a *Agent) markShellCommandApproved(command string) {
	if a == nil || command == "" {
		return
	}
	a.recentlyApprovedShellCommands.Store(command, time.Now())
}

// consumeShellCommandApproval returns true if this exact command was
// recently approved by Gate 1, deleting the entry so a second
// invocation gets its own prompt. Entries older than recentApprovalTTL
// are treated as expired (defensive — both gates should fire within
// the same tool dispatch).
func (a *Agent) consumeShellCommandApproval(command string) bool {
	if a == nil || command == "" {
		return false
	}
	v, ok := a.recentlyApprovedShellCommands.LoadAndDelete(command)
	if !ok {
		return false
	}
	ts, ok := v.(time.Time)
	if !ok {
		return false
	}
	return time.Since(ts) <= recentApprovalTTL
}

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
	// Checked before any other short-circuit so the allowlist works even
	// if Gate 1 already approved in-context (no duplicate work).
	if a.IsShellCommandAllowlisted(command) {
		return true
	}

	// If the static security classifier (Gate 1, in tool_security.go)
	// already prompted the user and they approved, don't re-prompt.
	// Both gates can fire for the same command (Gate 1 = "this looks
	// dangerous", Gate 2 = "your active profile/persona gates this");
	// asking twice in a row is a UX regression that SP-058 introduced
	// when Gate 2 moved from "always reject" to "prompt and continue".
	if HasUserApproval(ctx) {
		return true
	}

	// Seed pre-execute hook path: Gate 1 ran in newPreExecuteHook
	// (seed_tool_registry.go) which has no ctx access, so it can't
	// signal approval via WithUserApproved. Instead it records the
	// approved command in a per-agent map that we drain here. Same
	// effect, different transport.
	if a.consumeShellCommandApproval(command) {
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
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && a.HasActiveWebUIClients() {
		prompt := fmt.Sprintf("High-risk shell command:\n  %s", command)
		extras := map[string]string{
			"risk_type":     "Shell command — persona risk cascade",
			"command":       command,
			"allow_options": "true", // signals the frontend to render the 4-button dialog
		}
		decision := mgr.RequestToolApprovalDecision(
			a.GetEventBus(),
			a.GetEventClientID(),
			a.GetEventUserID(),
			"shell_command",
			webuiHighRiskRiskClass,
			prompt,
			extras,
		)
		a.applyApprovalDecision(decision, command)
		return decision.Approved()
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

	prompt := "⚠  High-risk operation rejected by the active risk profile."
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
