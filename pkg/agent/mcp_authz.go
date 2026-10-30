package agent

// mcp_authz.go — the MCP tool authorization gate (SP-170 phase 1).
//
// MCP tools are the only tools in Sprout with no authorization step:
// executeMCPTool is reached as a special case *after* ExecuteTool reports an
// unknown tool, so it never enters the registry and never reaches the
// approval broker. This file supplies the gate both entry points call before
// any server-side call.
//
// Decision order (SP-170):
//
//  1. allowlisted → run;
//  2. an interactive surface can answer → prompt, offering once / always
//     allow this tool / always allow this server / deny;
//  3. no surface → deny with an actionable error.
//
// Rule 3 is a deliberate behaviour change following SP-168's precedent: a run
// with no human must not silently execute an ungated third-party action.

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/injection"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// mcpAuthorizationChoices are the four answers the prompt offers (SP-170).
const (
	mcpAuthzRunOnce     = "once"
	mcpAuthzAllowTool   = "allow_tool"
	mcpAuthzAllowServer = "allow_server"
	mcpAuthzDeny        = "deny"
)

// IsMCPToolAllowlisted reports whether the given server/tool pair is covered
// by Config.ApprovedMCPTools. Safe on a nil agent (returns false: an agent we
// cannot interrogate authorizes nothing).
func (a *Agent) IsMCPToolAllowlisted(server, tool string) bool {
	if a == nil {
		return false
	}
	return injection.AllowlistedMCPTool(a.approvedMCPTools(), server, tool)
}

// approvedMCPTools snapshots the configured allowlist. A nil config yields
// nil, which matches nothing — the fail-closed direction.
func (a *Agent) approvedMCPTools() []string {
	cfg := a.GetConfig()
	if cfg == nil {
		return nil
	}
	return cfg.ApprovedMCPTools
}

// PersistMCPToolAllowlist appends entry to Config.ApprovedMCPTools and saves
// to disk. `entry` is either `<server>` or `<server>__<tool>`; the caller
// builds it. Idempotent: re-adding an existing entry is a no-op.
func (a *Agent) PersistMCPToolAllowlist(entry string) error {
	if entry == "" {
		return agenterrors.NewValidation("cannot allowlist empty MCP tool entry", nil)
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return agenterrors.NewPermission("no config manager — cannot persist MCP allowlist", nil)
	}
	return mgr.UpdateConfig(func(cfg *configuration.Config) error {
		for _, e := range cfg.ApprovedMCPTools {
			if e == entry {
				return nil
			}
		}
		cfg.ApprovedMCPTools = append(cfg.ApprovedMCPTools, entry)
		return nil
	})
}

// authorizeMCPTool is the gate both MCP entry points call before any
// server-side call. It returns nil when the call may proceed and a
// SecurityError when it may not.
//
// `server` and `tool` are the identity the call will actually be made with,
// so the allowlist entry named in the error is the one that would permit
// exactly this call.
func (a *Agent) authorizeMCPTool(server, tool string) error {
	if a == nil {
		return mcpToolDeniedError(server, tool)
	}
	if a.IsMCPToolAllowlisted(server, tool) {
		return nil
	}

	choice, answered := a.promptMCPAuthorization(server, tool)
	if !answered {
		return mcpToolDeniedError(server, tool)
	}

	switch choice {
	case mcpAuthzAllowTool:
		// Persist before running so a crash mid-call cannot lose the
		// user's decision. A persistence failure is surfaced but does not
		// block this invocation — the user did answer.
		a.persistMCPAuthorization(injection.MCPToolAllowlistEntry(server, tool))
		return nil
	case mcpAuthzAllowServer:
		a.persistMCPAuthorization(injection.MCPToolAllowlistServerEntry(server))
		return nil
	case mcpAuthzRunOnce:
		return nil
	default:
		return agenterrors.NewSecurityError(fmt.Sprintf(
			"MCP tool call denied: %s/%s. The user declined approval.", server, tool), nil)
	}
}

// persistMCPAuthorization records an allowlist entry, reporting a failure
// without blocking the already-approved invocation.
func (a *Agent) persistMCPAuthorization(entry string) {
	if err := a.PersistMCPToolAllowlist(entry); err != nil {
		a.DebugLogf("[mcp-authz] failed to persist allowlist entry %q: %v\n", entry, err)
	}
}

// promptMCPAuthorization asks a human which of the four answers applies.
// It returns the choice and whether a surface actually answered: an
// unanswered prompt (no surface, or a dialog nobody responded to) is
// reported as not-answered so the caller denies rather than assuming a
// default.
//
// Surface order follows the shell path (tool_security.go / approvals
// broker): the WebUI dialog when a browser tab is connected outside a
// workflow run, then the terminal picker.
func (a *Agent) promptMCPAuthorization(server, tool string) (string, bool) {
	if choice, answered := a.promptMCPAuthorizationWebUI(server, tool); answered {
		return choice, true
	}
	return a.promptMCPAuthorizationCLI(server, tool)
}

// promptMCPAuthorizationWebUI routes the prompt through the security
// approval manager's event-bus dialog. The browser dialog is the same
// 4-option control the shell gate uses; its "always" answer maps to
// allowlisting this tool (the server-wide choice is offered on the
// terminal picker, which can label it).
func (a *Agent) promptMCPAuthorizationWebUI(server, tool string) (string, bool) {
	mgr := a.GetSecurityApprovalMgr()
	if mgr == nil || a.GetEventBus() == nil {
		return "", false
	}
	if !a.HasActiveWebUIClients() || a.IsWorkflowRun() {
		return "", false
	}

	extras := map[string]string{
		"mcp_server":    server,
		"mcp_tool":      tool,
		"allow_options": "true",
	}
	// Suspend the CLI spinner / steer reader / streaming for the duration of
	// the wait, exactly as the shell broker does around the same call: the
	// dialog blocks for the approval timeout, and leaving those running means
	// they compete with the user's keystrokes for the whole wait.
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeIndicator()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	decision, outcome := mgr.RequestToolApprovalDecisionWithOutcome(
		a.GetEventBus(), a.GetEventClientID(), a.GetEventUserID(),
		"mcp:"+server+"/"+tool, "CAUTION",
		fmt.Sprintf("MCP tool call: %s/%s", server, tool),
		extras,
	)
	if outcome != security.ApprovalOutcomeResponded {
		// Timed out or the browser disconnected: fall through to the
		// terminal rather than treating silence as a deny.
		return "", false
	}
	switch decision {
	case security.ApprovalApproveAlways:
		return mcpAuthzAllowTool, true
	case security.ApprovalApproveOnce:
		return mcpAuthzRunOnce, true
	default:
		return mcpAuthzDeny, true
	}
}

// promptMCPAuthorizationCLI offers the four choices on the terminal. The
// logger is the same interactive surface the shell gate uses; when it is not
// interactive (SkipPrompt, no TTY, a subagent, or a workflow run) nothing
// answers and the caller denies.
func (a *Agent) promptMCPAuthorizationCLI(server, tool string) (string, bool) {
	if a.IsWorkflowRun() || a.IsNonInteractive() {
		return "", false
	}
	// Parallel subagents would interleave two pickers on one terminal, so the
	// CLI surface is offered only when at most one subagent is running —
	// mirroring the broker's subagentCLISafe rule. A single subagent may
	// prompt on the parent's terminal (its turn is blocked inside
	// run_subagent, so the TTY is free), labelled so the user knows whose
	// call it is.
	if a.IsSubagent() && a.ActiveSubagentCount() > 1 {
		return "", false
	}
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	if logger == nil || !logger.IsInteractive() {
		return "", false
	}

	subagentLabel := ""
	if a.IsSubagent() {
		subagentLabel = fmt.Sprintf("Subagent (depth %d) is running: ", a.SubagentDepth())
	}

	prompt := subagentLabel + fmt.Sprintf(
		"MCP tool call requires approval: %s/%s\n"+
			"  Allowlist entry for just this tool: %s\n"+
			"  Allowlist entry for the whole server: %s",
		server, tool,
		injection.MCPToolAllowlistEntry(server, tool),
		injection.MCPToolAllowlistServerEntry(server),
	)
	fmt.Println(prompt)

	// The picker is the same console select list the CLI's own commands use.
	// It must not go through PromptChoice: that path requires a.ui, which no
	// production code assigns, so every prompt would report "no surface" and
	// silently deny — turning a prompt into a wall.
	sl := console.NewSelectList(console.SelectListOptions{
		Title: "MCP tool authorization",
		Items: []console.SelectItem{
			{Label: "Run once", Value: mcpAuthzRunOnce},
			{Label: "Always allow this tool", Detail: injection.MCPToolAllowlistEntry(server, tool), Value: mcpAuthzAllowTool},
			{Label: "Always allow this server", Detail: injection.MCPToolAllowlistServerEntry(server), Value: mcpAuthzAllowServer},
			{Label: "Deny", Value: mcpAuthzDeny},
		},
	})
	choice, ok, err := sl.Run(context.Background())
	if err != nil || !ok {
		// Nothing answered (picker aborted or failed): treat as no surface.
		return "", false
	}
	return choice, true
}

// mcpToolDeniedError builds the no-surface denial. It is deliberately
// actionable in the style of the SP-168 judge denial: it names the tool,
// says why nobody could be asked, and gives the exact allowlist entry that
// would permit the call plus where to set it — so the agent can take a
// different path instead of retrying into the same wall.
func mcpToolDeniedError(server, tool string) error {
	entry := injection.MCPToolAllowlistEntry(server, tool)
	return agenterrors.NewSecurityError(fmt.Sprintf(
		"MCP tool call denied: %s/%s is not allowlisted and this run has no interactive surface to ask. "+
			"Add %q to approved_mcp_tools in your Sprout config (or %q to allow every tool on the %s server), "+
			"or take a different approach that does not call this tool. Do not retry this exact call — it will be denied again.",
		server, tool, entry, injection.MCPToolAllowlistServerEntry(server), server), nil)
}
