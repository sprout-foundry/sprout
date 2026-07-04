package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// computerUseOnce guards one-time registration of the computer-use tools into
// the global registries. Registration is global (the registries are
// process-wide singletons), so it must happen at most once even across multiple
// agent creations.
var computerUseOnce sync.Once

// computerUseToolNames is the set of tool names that are restricted to the
// computer_user persona. Populated when the tools are registered; empty (and
// therefore inert) when computer use is disabled.
var computerUseToolNames = map[string]bool{}

// RegisterComputerUseTools wires the computer_user persona's desktop-control
// tools (SP-063) into the agent's registries — but only when cfg explicitly
// enables them. Idempotent and safe to call on every agent creation.
//
// Gating layers (defense in depth):
//  1. cfg.ComputerUse.Enabled must be true — off by default.
//  2. A real platform backend must be constructable (macOS+cliclick or
//     linux/X11+xdotool); otherwise nothing is registered and the reason is
//     returned for the caller to surface.
//  3. Exposure is limited to the computer_user persona's allowed_tools, and a
//     dispatch-layer guard (isComputerUseToolBlocked) rejects the tools for any
//     other active persona.
//  4. Every action is rate-limited and audited (see the wrapped backend).
func RegisterComputerUseTools(cfg *configuration.Config) error {
	if cfg == nil || cfg.ComputerUse == nil || !cfg.ComputerUse.Enabled {
		return nil
	}
	cu := cfg.ComputerUse.Resolve()

	real, err := computer_use.NewPlatformBackend()
	if err != nil {
		return errors.NewTool("computer_use", "computer use unavailable", err)
	}

	// Compose decorators: real → panicable → rate-limited → auditing.
	panicable := computer_use.NewPanicableBackend(real)
	var backend computer_use.ComputerBackend = computer_use.NewRateLimitedBackend(panicable, cu.MaxActionsPerMinute)
	auditDir := cu.AuditLogDir
	if auditDir == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			auditDir = filepath.Join(home, ".config", "sprout", "computer_use_log")
		}
	}
	if auditDir != "" {
		if ab, aerr := computer_use.NewAuditingBackend(backend, auditDir, "session"); aerr == nil {
			backend = ab
		}
	}
	computer_use.SetBackend(backend)

	// SP-063-4h: wire the destructive-app gate into the auditing backend's
	// PreActionHook. The hook fires before MouseClick, MouseDrag,
	// KeyboardPress, and Scroll — the four action methods that could
	// interact with a denylisted app.
	if cu.DestructiveAppGate {
		computer_use.SetBackendPreActionHook(computerUseDestructiveAppGateFn)
	}

	computerUseOnce.Do(func() {
		newReg := tools.GetNewToolRegistry()
		for _, h := range computer_use.Handlers() {
			if regErr := newReg.Register(h); regErr != nil {
				// Already registered (e.g. a prior call) — definitions are
				// global too, so skip re-adding.
				continue
			}
		}
		// Build the name set atomically so isComputerUseToolBlocked never
		// sees a partially-populated map (avoids data race with concurrent
		// dispatch).
		names := make(map[string]bool)
		for _, name := range computer_use.ToolNames() {
			names[name] = true
		}
		computerUseToolNames = names

		// Start the OS-chord watcher for the panic key. Best-effort: failure
		// here must not block the rest of computer-use registration.
		panicKeyChord := cu.PanicKeyChord
		if !computer_use.IsChordDisabled(panicKeyChord) {
			// Use a timeout context so an unresponsive osascript/xdotool
			// can't hang agent creation.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			watcher := computer_use.NewChordWatcher(panicKeyChord)
			startErr := watcher.Start(ctx)
			cancel()
			if startErr != nil {
				log.Printf("[computer-use] panic-key chord watcher unavailable: %v", startErr)
			}
			computer_use.SetActiveChordWatcher(watcher)
		}
	})
	return nil
}

// checkComputerUseActivation enforces the SP-063 gates required to switch into
// the computer_user persona. Returns a descriptive error (surfaced to the user)
// when any precondition fails. Called from ApplyPersona.
func (a *Agent) checkComputerUseActivation() error {
	cfg := a.GetConfig()
	if cfg == nil || cfg.ComputerUse == nil || !cfg.ComputerUse.Enabled {
		return errors.NewPermission("the computer_user persona is off by default — enable it first (set computer_use.enabled = true in settings)", nil)
	}
	if a.IsSubagent() {
		return errors.NewPermission("computer_user must be a top-level persona; it cannot be activated inside a subagent (no silent autonomous computer control)", nil)
	}
	// SP-063: reject non-interactive activation. cfg.SkipPrompt is true for
	// `sprout agent --skip-prompt`, automate workflows that run the agent, and
	// the daemon serving API requests (see cmd/agent_modes.go). Computer use
	// must never start silently — it always requires explicit interactive
	// consent, so block under all three conditions.
	if cfg.SkipPrompt {
		return errors.NewPermission("the computer_user persona cannot run under --skip-prompt or in daemon mode (computer use requires explicit interactive consent)", nil)
	}
	if support := computer_use.CheckPlatformSupport(); !support.Supported {
		return errors.NewTool("computer_use", fmt.Sprintf("computer use is unavailable on this machine: %s", support.Reason), nil)
	}
	if a.client != nil && !a.effectiveVisionSupport() {
		return errors.NewTool("computer_use", fmt.Sprintf("computer_user requires a vision-capable provider; %q has no vision support — switch to a model that accepts images", a.GetProvider()), nil)
	}
	return nil
}

// isComputerUseToolBlocked reports whether the named tool is a computer-use
// tool being invoked by a persona other than computer_user. This is the
// dispatch-layer enforcement of SP-063 Phase 6 (tools allowlisted only for the
// computer_user persona). It is inert when computer use is disabled (the name
// set is empty).
func isComputerUseToolBlocked(toolName string, agent *Agent) bool {
	if !computerUseToolNames[toolName] {
		return false
	}
	if agent == nil {
		return true
	}
	active := normalizeAgentPersonaID(agent.state.GetActivePersona())
	return active != personas.IDComputerUser
}

// ---------------------------------------------------------------------------
// SP-063: Per-session opt-in
// ---------------------------------------------------------------------------

// checkComputerUseSessionOptIn enforces the per-session consent gate for
// computer-use actions (SP-063). On the first computer-use tool call in a
// chat session it prompts the user for explicit consent via the standard
// WebUI + CLI approval cascade. Once approved (or when the workspace is on
// the persistent allowlist), subsequent calls are fast-pathed. Returns nil
// when the action may proceed, an error when it must be blocked.
//
// The check is placed in ExecuteTool AFTER isComputerUseToolBlocked so that:
//  1. Non-computer-use tools skip the check entirely (the name set acts as
//     a cheap filter).
//  2. Wrong-persona calls are rejected before reaching the prompt.
//  3. Only legitimate computer-use calls incur the (potentially blocking)
//     consent dialog.
func (a *Agent) checkComputerUseSessionOptIn(toolName string) error {
	if a == nil {
		return nil
	}

	// Fast path: already approved this session.
	a.computerUseMu.Lock()
	approved := a.computerUseSessionApproved
	a.computerUseMu.Unlock()
	if approved {
		return nil
	}

	cfg := a.GetConfig()
	if cfg == nil || cfg.ComputerUse == nil {
		// No config or no computer-use config — this gate is inert. The
		// isComputerUseToolBlocked check upstream already ensures the tools
		// are registered, so reaching here without config means the feature
		// wasn't enabled through the normal path. Allow (defense in depth
		// is handled by activation gates).
		return nil
	}

	// Auto-approve when the workspace is on the persistent allowlist.
	ws := a.effectiveCwd()
	if ws != "" && isWorkspaceComputerUseAllowlisted(ws, cfg.ComputerUse.WorkspaceAllowlist) {
		a.computerUseMu.Lock()
		a.computerUseSessionApproved = true
		a.computerUseMu.Unlock()
		if a.debug {
			a.debugLog("[computer-use] session auto-approved via workspace allowlist: %s\n", ws)
		}
		return nil
	}

	actionDesc := computerUseActionDescription(toolName)
	prompt := fmt.Sprintf(
		"The computer_user persona is about to control your desktop for the first time this session.\n\n"+
			"Workspace: %s\n"+
			"Action: %s\n\n"+
			"Allow this session to use computer use?",
		ws, actionDesc,
	)

	// ---- WebUI path ----
	// Prefer the browser dialog when a WebUI client is connected, matching
	// the existing security-approval pattern in ExecuteTool /
	// handleFileSecurityError.
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && a.HasActiveWebUIClients() {
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		defer clihooks.ResumeSteer()

		if a.debug {
			a.debugLog("[computer-use] requesting per-session opt-in via webui for %s (workspace: %s)\n", toolName, ws)
		}
		extras := map[string]string{
			"risk_type": "Computer Use Opt-In",
			"workspace": ws,
			"action":    toolName,
			"kind":      "computer_use_optin",
		}
		decision, outcome := mgr.RequestApprovalDecisionWithOutcome(
			a.GetEventBus(),
			security.ApprovalRequest{
				Kind:            security.ApprovalKindTool,
				DefaultResponse: false,
				ToolName:        toolName,
				RiskLevel:       "CAUTION",
				Reasoning:       prompt,
				ClientID:        a.GetEventClientID(),
				UserID:          a.GetEventUserID(),
				Extras:          extras,
			},
		)
		if outcome == security.ApprovalOutcomeResponded {
			return a.applyComputerUseOptInDecision(decision, ws)
		}
		// Timed out or browser disconnected — fall through to CLI prompt
		// so a user at the terminal can respond (same rationale as the
		// tool-security path).
		if a.debug {
			a.debugLog("[computer-use] webui opt-in unanswered (outcome=%d) — falling back to CLI\n", outcome)
		}
	}

	// ---- CLI fallback ----
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	if logger != nil && logger.IsInteractive() {
		if !logger.AskForConfirmation(prompt, false, true) {
			if a.debug {
				a.debugLog("[computer-use] user denied per-session opt-in (CLI) for %s\n", toolName)
			}
			return errors.NewPermission("computer use denied: the user declined the per-session opt-in", nil)
		}
		// CLI path is yes/no only — record as session-scoped approval.
		return a.applyComputerUseOptInDecision(security.ApprovalApproveOnce, ws)
	}

	// Non-interactive with no WebUI response — block for safety.
	return errors.NewPermission("computer use requires interactive opt-in consent — no approval mechanism available (re-run interactively or add the workspace to computer_use.workspace_allowlist in settings)", nil)
}

// applyComputerUseOptInDecision records the user's consent choice, persists
// "always" approvals to disk, writes an audit-log entry, and returns nil
// when the action may proceed. ApprovalDeny returns an error.
func (a *Agent) applyComputerUseOptInDecision(decision security.ApprovalDecision, workspace string) error {
	switch decision {
	case security.ApprovalDeny:
		if a.debug {
			a.debugLog("[computer-use] user denied per-session opt-in (workspace: %s)\n", workspace)
		}
		return errors.NewPermission("computer use denied: the user declined the per-session opt-in", nil)
	case security.ApprovalApproveAlways:
		// Persist the workspace root to the allowlist so future sessions
		// in this directory auto-approve.
		if err := a.persistComputerUseWorkspaceAllowlist(workspace); err != nil {
			if a.debug {
				a.debugLog("[computer-use] failed to persist workspace allowlist: %v (continuing with session approval)\n", err)
			}
			// Non-fatal: still approve for this session.
		}
		fallthrough
	default:
		// ApproveOnce, Elevate, AllowFolderSession all collapse to
		// session-scoped approval (the dialog conceptually only offers
		// once / always / deny for computer use).
		a.computerUseMu.Lock()
		a.computerUseSessionApproved = true
		a.computerUseMu.Unlock()
		computer_use.RecordSafetyEvent("opt_in", map[string]any{
			"workspace": workspace,
			"scope":     decision.String(),
		})
		if a.debug {
			a.debugLog("[computer-use] session opt-in approved (scope=%s, workspace=%s)\n", decision.String(), workspace)
		}
		return nil
	}
}

// persistComputerUseWorkspaceAllowlist appends workspace to the persistent
// ComputerUse.WorkspaceAllowlist in config and saves to disk. Idempotent.
func (a *Agent) persistComputerUseWorkspaceAllowlist(workspace string) error {
	if a == nil || workspace == "" {
		return errors.NewValidation("cannot allowlist empty workspace", nil)
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return errors.NewTool("computer_use", "no config manager — cannot persist workspace allowlist", nil)
	}
	return mgr.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.ComputerUse == nil {
			cfg.ComputerUse = &configuration.ComputerUseConfig{}
		}
		for _, w := range cfg.ComputerUse.WorkspaceAllowlist {
			if w == workspace {
				return nil // dedup
			}
		}
		cfg.ComputerUse.WorkspaceAllowlist = append(cfg.ComputerUse.WorkspaceAllowlist, workspace)
		return nil
	})
}

// isWorkspaceComputerUseAllowlisted reports whether the given workspace path
// sits under (or equals) any entry in the allowlist. Both sides are
// normalized so trailing slashes and symlinks don't cause false negatives.
func isWorkspaceComputerUseAllowlisted(workspace string, allowlist []string) bool {
	if workspace == "" || len(allowlist) == 0 {
		return false
	}
	normalized := normalizePath(workspace)
	for _, entry := range allowlist {
		if isUnderPrefix(normalized, normalizePath(entry)) {
			return true
		}
	}
	return false
}

// computerUseActionDescription returns a human-readable one-line description
// of what the named computer-use tool does, for the consent dialog.
func computerUseActionDescription(toolName string) string {
	switch toolName {
	case "take_screenshot":
		return "capture a screenshot of your screen"
	case "mouse_click":
		return "click the mouse"
	case "mouse_drag":
		return "drag the mouse"
	case "keyboard_type":
		return "type on the keyboard"
	case "keyboard_press":
		return "press a keyboard key"
	case "scroll":
		return "scroll the screen"
	default:
		return fmt.Sprintf("perform a computer-use action (%s)", toolName)
	}
}
