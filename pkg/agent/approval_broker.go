package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// BrokerDecision is the typed verdict returned by RequestApproval.
type BrokerDecision struct {
	Approved   bool
	Decision   security.ApprovalDecision
	Outcome    security.ApprovalOutcome
	Surface    string         // "webui" or "cli" — which surface answered
	Assessment RiskAssessment // echoed for caller diagnostics
	// Analysis holds the LLM-derived security analysis when available (SP-124).
	// Nil when: tracking disabled, no LLM call needed (Low risk), LLM timed
	// out, LLM errored, or analysis JSON failed to parse. Callers must treat
	// nil as "fall back to static classifier reasoning".
	Analysis *SecurityAnalysis
}

// RequestApproval performs the unified approval flow for a RiskAssessment.
//
// For Low-risk / no-prompt-needed assessments, returns early with
// (BrokerDecision{Approved: true, Assessment: assessment}, nil).
//
// For Critical / IsHardBlock assessments, returns a SecurityError without
// consulting any approval surface (hard-blocks are unconditional).
//
// For Medium/High/IntentConfirmation assessments:
//  1. Checks fast-bypass paths (persistent allowlist, unsafe-mode, session
//     elevation, unsafe-shell)
//  2. Tries WebUI first if available
//  3. Falls back to CLI (using AskForApprovalWithOptions for shell_command
//     with 4-option cascade, or AskForConfirmation for other tools)
//  4. For non-interactive with no surface: permissive auto-approve
//
// It returns (BrokerDecision, error) — non-nil error means deny/hard-block,
// nil means approved (or auto-approved).
func (a *Agent) RequestApproval(assessment RiskAssessment, toolName string, args map[string]interface{}) (BrokerDecision, error) {
	// --- Command policy check (shell_command only) ---
	// MUST be the very first check, before the Low-risk early return.
	// If placed after the Low-risk check, "deny" and "ask" actions silently
	// fail for commands the classifier rates as SAFE/Low.
	skipAllowlist := false
	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			cfg := a.GetConfig()
			if cfg != nil && cfg.CommandPolicies != nil {
				if action, matchedPattern, matched := EvaluateCommandPolicy(cmd, cfg.CommandPolicies); matched {
					switch action {
					case configuration.CommandPolicyDeny:
						a.logSecurityDecision(toolName, args, assessment, "blocked")
						return BrokerDecision{
								Approved:   false,
								Decision:   security.ApprovalDeny,
								Surface:    "command-policy",
								Assessment: assessment,
							}, agenterrors.NewSecurityErrorWithAssessment(
								fmt.Sprintf("blocked by command policy: %s — %s", matchedPattern, assessment.Reason),
								assessment.Explain(), nil,
							)
					case configuration.CommandPolicyAllow:
						// Auto-approve: skip classifier, risk profile, and interactive prompt.
						// Critical-tier hard blocks can never be overridden by user policy.
						if assessment.IsHardBlock || assessment.Level == configuration.RiskLevelCritical {
							a.logSecurityDecision(toolName, args, assessment, "blocked")
							return BrokerDecision{
									Approved:   false,
									Decision:   security.ApprovalDeny,
									Surface:    "command-policy",
									Assessment: assessment,
								}, agenterrors.NewSecurityErrorWithAssessment(
									fmt.Sprintf("critical operation cannot be overridden by allow policy: %s", matchedPattern),
									assessment.Explain(), nil,
								)
						}
						return BrokerDecision{
							Approved:   true,
							Decision:   security.ApprovalApproveOnce,
							Surface:    "command-policy",
							Assessment: assessment,
						}, nil
					case configuration.CommandPolicyAsk:
						// Force interactive prompt: skip the allowlist bypass below.
						// The classifier risk is still computed for display.
						skipAllowlist = true
					}
				}
			}
		}
	}

	// Low risk, no prompt needed — auto-approve
	if assessment.Level == configuration.RiskLevelLow && !assessment.RequiresIntentConfirmation {
		return BrokerDecision{
			Approved:   true,
			Assessment: assessment,
		}, nil
	}

	// Critical / hard-block — unconditional deny
	if assessment.IsHardBlock || assessment.Level == configuration.RiskLevelCritical {
		a.logSecurityDecision(toolName, args, assessment, "blocked")
		return BrokerDecision{
				Approved:   false,
				Decision:   security.ApprovalDeny,
				Surface:    "none",
				Assessment: assessment,
			}, agenterrors.NewSecurityErrorWithAssessment(
				fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", toolName, assessment.Reason), assessment.Explain(), nil,
			)
	}

	// --- Fast bypass paths ---

	// Persistent allowlist for shell commands (skipped when a command policy
	// "ask" rule matched — those must always prompt)
	if !skipAllowlist && toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" && a.IsShellCommandAllowlisted(cmd) {
			return BrokerDecision{
				Approved:   true,
				Decision:   security.ApprovalApproveOnce,
				Surface:    "allowlist",
				Assessment: assessment,
			}, nil
		}
	}

	// Unsafe mode
	if a.GetUnsafeMode() {
		if a.debug {
			a.debugLog("[UNLOCK] RequestApproval auto-approve (unsafe mode): %s\n", toolName)
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Surface:    "unsafe-mode",
			Assessment: assessment,
		}, nil
	}

	// Session elevation
	if a.IsSessionElevated() {
		if a.debug {
			a.debugLog("[UNLOCK] RequestApproval auto-approve (session elevated): %s\n", toolName)
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Surface:    "session-elevated",
			Assessment: assessment,
		}, nil
	}

	// --unsafe-shell bypasses Medium-tier shell prompts
	if a.GetUnsafeShellMode() && toolName == "shell_command" &&
		assessment.Level == configuration.RiskLevelMedium &&
		!assessment.RequiresIntentConfirmation {
		if a.debug {
			a.debugLog("[UNLOCK] RequestApproval auto-approve (unsafe-shell): %s\n", toolName)
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Surface:    "unsafe-shell",
			Assessment: assessment,
		}, nil
	}

	// --- SP-124 Phase 1: optional LLM analysis for shell commands ---
	// Only run for Medium/High risk shell commands (skip Low; skip Critical
	// hard-blocks since those don't reach prompts). On error or timeout,
	// securityAnalysis stays nil and we fall through to the static-classifier
	// prompt.
	var securityAnalysis *SecurityAnalysis
	if toolName == "shell_command" &&
		(assessment.Level == configuration.RiskLevelMedium ||
			assessment.Level == configuration.RiskLevelHigh) {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			// Cache check — identical commands in the same session reuse
			// the cached analysis.
			if cached, ok := a.getSecurityAnalysisCache().Get(cmd); ok {
				securityAnalysis = cached
			} else {
				ctx, cancel := context.WithTimeout(a.interruptCtx, 2*time.Second)
				sa, err := AnalyzeShellCommand(ctx, a, cmd, a.effectiveCwd())
				cancel()
				if err == nil && sa != nil {
					securityAnalysis = sa
					a.getSecurityAnalysisCache().Set(cmd, sa)
				}
				// On error or timeout: securityAnalysis stays nil; fall through
				// to static-classifier prompt.
			}
		}
	}

	// --- Interactive approval surfaces ---

	isSubagent := a.IsSubagent()

	// WebUI path — interactive only, non-interactive runs skip
	hasInteractiveSurface := !a.isNonInteractive() && !isSubagent && a.HasActiveWebUIClients()
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && hasInteractiveSurface {
		// Suspend CLI spinner and steer reader before blocking on the
		// webui response — prevents terminal corruption during the wait.
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		defer clihooks.ResumeIndicator()
		defer clihooks.ResumeSteer()

		// Build extras for the dialog
		extras := map[string]string{
			"risk_level": string(assessment.Level),
		}
		switch toolName {
		case "shell_command":
			if cmd, ok := args["command"].(string); ok && cmd != "" {
				extras["command"] = cmd
				extras["allow_options"] = "true"
			}
		case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
			if path, ok := args["path"].(string); ok && path != "" {
				extras["target"] = path
			}
		}
		if assessment.RequiresIntentConfirmation {
			extras["intent_confirmation"] = "true"
		}

		// SP-124 Phase 1: attach LLM analysis to the WebUI extras so the
		// approval dialog can display it (Phase 2).
		if securityAnalysis != nil {
			jsonBytes, _ := json.Marshal(securityAnalysis)
			extras["security_analysis"] = string(jsonBytes)
		}

		riskLabel := string(assessment.Level)
		if toolName == "shell_command" && assessment.RequiresIntentConfirmation {
			riskLabel = "INTENT"
		}

		decision, outcome := mgr.RequestToolApprovalDecisionWithOutcome(
			a.GetEventBus(),
			a.GetEventClientID(),
			a.GetEventUserID(),
			toolName,
			riskLabel,
			assessment.Reason,
			extras,
		)

		// Only honor when the user actually responded; on timeout/disconnect
		// fall through to CLI prompt below.
		if outcome == security.ApprovalOutcomeResponded {
			// Apply side effects for shell commands
			if toolName == "shell_command" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					a.applyApprovalDecision(decision, cmd)
				}
			}
			if !decision.Approved() {
				a.logSecurityDecision(toolName, args, assessment, "blocked")
				return BrokerDecision{
						Approved:   false,
						Decision:   decision,
						Outcome:    outcome,
						Surface:    "webui",
						Assessment: assessment,
					}, agenterrors.NewSecurityErrorWithAssessment(
						fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, assessment.Reason), assessment.Explain(), nil,
					)
			}
			a.logSecurityDecision(toolName, args, assessment, "approved")
			if toolName == "run_automate" {
				if wf, ok := args["workflow"].(string); ok && wf != "" {
					a.MarkWorkflowApprovedInSession(wf)
				}
			}
			return BrokerDecision{
				Approved:   true,
				Decision:   decision,
				Outcome:    outcome,
				Surface:    "webui",
				Assessment: assessment,
				Analysis:   securityAnalysis,
			}, nil
		}
		// Outcome was TimedOut/NoChannel — fall through to CLI
		if a.debug {
			a.debugLog("[APPROVAL] webui approval unanswered (outcome=%v) for %s — falling back to CLI\n", outcome, toolName)
		}
	}

	// CLI path
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

	if canPrompt {
		// For shell_command: use the 4-option approval picker (AskForApprovalWithOptions)
		if toolName == "shell_command" {
			// SP-093-2: per-part picker (opt-in via EditApprovalConfig.ShellCommand).
			if cfg != nil && cfg.EditApproval != nil && cfg.EditApproval.ShellCommand &&
				args["command"] != "" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					proposal := NewShellProposal(cmd)
					pickerCtx, pickerCancel := context.WithTimeout(context.Background(), utils.ApprovalPromptTimeout)
					decisions, pickErr := a.RequestShellApproval(pickerCtx, proposal)
					pickerCancel()
					if pickErr != nil {
						a.logSecurityDecision(toolName, args, assessment, "blocked")
						return BrokerDecision{
								Approved: false, Decision: security.ApprovalDeny,
								Surface: "cli", Assessment: assessment,
							}, agenterrors.NewSecurityErrorWithAssessment(
								fmt.Sprintf("security rejected: %s — picker error: %v", toolName, pickErr),
								assessment.Explain(), nil,
							)
					}
					// Per-part decision: any rejection -> deny whole command.
					allApproved := true
					for _, part := range proposal.Parts {
						if approved, ok := decisions[part.ID]; !ok || !approved {
							allApproved = false
							break
						}
					}
					if !allApproved {
						a.logSecurityDecision(toolName, args, assessment, "blocked")
						return BrokerDecision{
								Approved: false, Decision: security.ApprovalDeny,
								Outcome: security.ApprovalOutcomeResponded, Surface: "cli",
								Assessment: assessment,
							}, agenterrors.NewSecurityErrorWithAssessment(
								fmt.Sprintf("security rejected: %s — one or more parts denied.", toolName),
								assessment.Explain(), nil,
							)
					}
					// All approved — persist decisions map and return.
					a.applyApprovalDecision(security.ApprovalApproveOnce, cmd)
					a.logSecurityDecision(toolName, args, assessment, "approved")
					return BrokerDecision{
						Approved: true, Decision: security.ApprovalApproveOnce,
						Outcome: security.ApprovalOutcomeResponded, Surface: "cli",
						Assessment: assessment,
						Analysis:   securityAnalysis,
					}, nil
				}
			}

			if cmd, ok := args["command"].(string); ok && cmd != "" {
				prompt := "Security Warning — " + string(assessment.Level)
				if assessment.RequiresIntentConfirmation {
					prompt = "High-risk operation — your active risk profile gates this command."
				}
				// SP-124 Phase 3: convert the agent-level analysis to the
				// leaf-level utils view so the arrow-key picker can render
				// it above the option list. nil when the analyzer timed
				// out / errored / wasn't produced; the picker omits the
				// panel in that case.
				var analysisView *utils.SecurityAnalysisView
				if securityAnalysis != nil {
					analysisView = &utils.SecurityAnalysisView{
						Summary:         securityAnalysis.Summary,
						Modifies:        securityAnalysis.Modifies,
						RiskAssessment:  securityAnalysis.RiskAssessment,
						Recommendation:  securityAnalysis.Recommendation,
					}
				}
				choice := logger.AskForApprovalWithOptions(prompt, cmd, analysisView)
				decision := approvalDecisionFromCLIChoice(choice)
				a.applyApprovalDecision(decision, cmd)
				if !decision.Approved() {
					a.logSecurityDecision(toolName, args, assessment, "blocked")
					return BrokerDecision{
							Approved:   false,
							Decision:   decision,
							Outcome:    security.ApprovalOutcomeResponded,
							Surface:    "cli",
							Assessment: assessment,
						}, agenterrors.NewSecurityErrorWithAssessment(
							fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, assessment.Reason), assessment.Explain(), nil,
						)
				}
				a.logSecurityDecision(toolName, args, assessment, "approved")
				if toolName == "run_automate" {
					if wf, ok := args["workflow"].(string); ok && wf != "" {
						a.MarkWorkflowApprovedInSession(wf)
					}
				}
				return BrokerDecision{
					Approved:   true,
					Decision:   decision,
					Outcome:    security.ApprovalOutcomeResponded,
					Surface:    "cli",
					Assessment: assessment,
					Analysis:   securityAnalysis,
				}, nil
			}
		}

		// For non-shell tools: simple yes/no
		prompt := fmt.Sprintf("⚠  Security Warning — %s\n\nReasoning: %s\n\nDo you want to proceed?",
			strings.ToUpper(string(assessment.Level)), assessment.Reason)
		if !logger.AskForConfirmation(prompt, false, false) {
			a.logSecurityDecision(toolName, args, assessment, "blocked")
			return BrokerDecision{
					Approved:   false,
					Decision:   security.ApprovalDeny,
					Outcome:    security.ApprovalOutcomeResponded,
					Surface:    "cli",
					Assessment: assessment,
				}, agenterrors.NewSecurityErrorWithAssessment(
					fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, assessment.Reason), assessment.Explain(), nil,
				)
		}
		a.logSecurityDecision(toolName, args, assessment, "approved")
		if toolName == "run_automate" {
			if wf, ok := args["workflow"].(string); ok && wf != "" {
				a.MarkWorkflowApprovedInSession(wf)
			}
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Outcome:    security.ApprovalOutcomeResponded,
			Surface:    "cli",
			Assessment: assessment,
			Analysis:   securityAnalysis,
		}, nil
	}

	// Non-interactive: permissive-by-default
	if a.isNonInteractive() {
		if a.debug {
			a.debugLog("[non-interactive] auto-approving %s (level: %s) — no interactive surface\n",
				toolName, assessment.Level)
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Surface:    "non-interactive",
			Assessment: assessment,
			Analysis:   securityAnalysis,
		}, nil
	}

	// No interactive surface at all — fail safe
	a.logSecurityDecision(toolName, args, assessment, "blocked")
	return BrokerDecision{
			Approved:   false,
			Decision:   security.ApprovalDeny,
			Outcome:    security.ApprovalOutcomeNoChannel,
			Surface:    "none",
			Assessment: assessment,
		}, agenterrors.NewSecurityErrorWithAssessment(
			fmt.Sprintf("security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm.",
				toolName, assessment.Reason), assessment.Explain(), nil,
		)
}
