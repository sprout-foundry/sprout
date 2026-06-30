package agent

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent_tools/computer_use"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// checkComputerUseDestructiveAppGate enforces the destructive-app denylist
// gate for a single computer-use action. Returns nil when the action may
// proceed, or an error when it must be blocked.
//
// The gate follows this cascade:
//  1. Classify the foreground app against the denylist.
//  2. If not blocked → fast-path return nil.
//  3. If already in the per-session app allowlist → return nil.
//  4. Otherwise → prompt the user (WebUI dialog → CLI fallback → block).
//  5. On allow-always → persist the app to the override file + session allowlist.
func (a *Agent) checkComputerUseDestructiveAppGate(action string, args map[string]any, fg computer_use.ForegroundInfo) error {
	if a == nil {
		return nil
	}

	// Step 1: classify.
	cls := computer_use.DefaultLoader().IsDestructiveApp(fg)
	if !cls.IsBlocked() {
		return nil
	}

	// Step 2: check per-session allowlist.
	appKey := a.computerUseAppKey(fg)
	if a.IsAppAllowedForComputerUse(appKey) {
		return nil
	}

	// Step 3: prompt the user.
	decision := a.promptForDestructiveApp(action, args, fg, cls)

	// Step 4: handle the decision.
	switch decision {
	case computer_use.DestructiveAppDeny:
		return fmt.Errorf("destructive app blocked: %s (%s)", fg.AppName, cls.Reason)

	case computer_use.DestructiveAppAllowOnce:
		return nil

	case computer_use.DestructiveAppAllowAlways:
		// Persist to per-session allowlist.
		a.AllowAppForComputerUse(appKey)
		// Persist to override file so future sessions also skip the prompt.
		bundleID := fg.BundleID
		windowClass := fg.WindowClass
		if err := computer_use.AddAllowEntry(computer_use.DefaultLoader(), bundleID, windowClass); err != nil {
			if a.debug {
				a.debugLog("[computer-use] failed to persist allow-always override for %s: %v\n", appKey, err)
			}
			// Non-fatal: the session allowlist still works.
		}
		computer_use.RecordSafetyEvent("destructive_app_allowed_always", map[string]any{
			"app":      appKey,
			"category": string(cls.Category),
			"action":   action,
		})
		return nil
	}

	return nil
}

// computerUseAppKey returns a unique identifier for the given ForegroundInfo.
// Uses BundleID on macOS, WindowClass on Linux, AppName as fallback.
func (a *Agent) computerUseAppKey(fg computer_use.ForegroundInfo) string {
	if fg.BundleID != "" {
		return fg.BundleID
	}
	if fg.WindowClass != "" {
		return "class:" + fg.WindowClass
	}
	return "app:" + fg.AppName
}

// promptForDestructiveApp shows the user an approval dialog for the
// destructive-app gate. Mirrors the per-session opt-in cascade:
// WebUI dialog → CLI fallback → block on non-interactive.
func (a *Agent) promptForDestructiveApp(action string, args map[string]any, fg computer_use.ForegroundInfo, cls computer_use.Classification) computer_use.DestructiveAppDecision {
	if a == nil {
		return computer_use.DestructiveAppDeny
	}

	// Record classification event.
	computer_use.RecordSafetyEvent("destructive_app_classified", map[string]any{
		"app":       fg.AppName,
		"bundle_id": fg.BundleID,
		"class":     fg.WindowClass,
		"category":  string(cls.Category),
		"reason":    cls.Reason,
		"action":    action,
	})

	// Build prompt message.
	actionDesc := computerUseActionDescription(action)
	matchedApp := fg.AppName
	if matchedApp == "" {
		if fg.BundleID != "" {
			matchedApp = fg.BundleID
		} else if fg.WindowClass != "" {
			matchedApp = fg.WindowClass
		}
	}
	prompt := fmt.Sprintf(
		"The agent is about to %s in a %s app (%s): %s\n\nAllow this action?",
		actionDesc, cls.Category, matchedApp, cls.Reason,
	)

	extras := map[string]string{
		"risk_type":    "destructive_app",
		"app_name":     fg.AppName,
		"bundle_id":    fg.BundleID,
		"window_class": fg.WindowClass,
		"category":     string(cls.Category),
		"reason":       cls.Reason,
		"action":       action,
		"kind":         "destructive_app",
	}

	// ---- WebUI path ----
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && a.HasActiveWebUIClients() {
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		defer clihooks.ResumeSteer()

		if a.debug {
			a.debugLog("[computer-use] requesting destructive-app approval via webui for %s in %s\n", action, matchedApp)
		}
		decision, outcome := mgr.RequestApprovalDecisionWithOutcome(
			a.GetEventBus(),
			security.ApprovalRequest{
				Kind:            security.ApprovalKindTool,
				DefaultResponse: false,
				ToolName:        action,
				RiskLevel:       "CAUTION",
				Reasoning:       prompt,
				ClientID:        a.GetEventClientID(),
				UserID:          a.GetEventUserID(),
				Extras:          extras,
			},
		)
		if outcome == security.ApprovalOutcomeResponded {
			decision := mapDestructiveAppDecision(decision)
			computer_use.RecordSafetyEvent("destructive_app_prompt", map[string]any{
				"app":      matchedApp,
				"category": string(cls.Category),
				"action":   action,
				"decision": decision.String(),
			})
			return decision
		}
		// Timed out or browser disconnected — fall through to CLI.
		if a.debug {
			a.debugLog("[computer-use] webui destructive-app prompt unanswered (outcome=%d) — falling back to CLI\n", outcome)
		}
	}

	// ---- CLI fallback ----
	logger := utils.GetLogger(false)
	if logger != nil && logger.IsInteractive() {
		if logger.AskForConfirmation(prompt, false, true) {
			computer_use.RecordSafetyEvent("destructive_app_prompt", map[string]any{
				"app":      matchedApp,
				"category": string(cls.Category),
				"action":   action,
				"decision": "allow_once",
			})
			return computer_use.DestructiveAppAllowOnce
		}
		computer_use.RecordSafetyEvent("destructive_app_prompt", map[string]any{
			"app":      matchedApp,
			"category": string(cls.Category),
			"action":   action,
			"decision": "deny",
		})
		return computer_use.DestructiveAppDeny
	}

	// Non-interactive with no WebUI response — block for safety.
	return computer_use.DestructiveAppDeny
}

// mapDestructiveAppDecision maps a security.ApprovalDecision to the
// destructive-app decision enum.
func mapDestructiveAppDecision(d security.ApprovalDecision) computer_use.DestructiveAppDecision {
	switch d {
	case security.ApprovalDeny:
		return computer_use.DestructiveAppDeny
	case security.ApprovalApproveOnce, security.ApprovalAllowFolderSession:
		return computer_use.DestructiveAppAllowOnce
	case security.ApprovalApproveAlways, security.ApprovalElevate:
		return computer_use.DestructiveAppAllowAlways
	default:
		return computer_use.DestructiveAppDeny
	}
}
