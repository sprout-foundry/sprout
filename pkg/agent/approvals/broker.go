// Package approvals: the approval broker (SP-141 phase 3, increment 5).
// RequestApproval is the unified approval flow for a RiskAssessment:
// command-policy, allowlist, unsafe/elevated bypasses, optional LLM
// security analysis, then the WebUI / CLI interactive surfaces, with
// permissive auto-approve as the last resort in non-interactive mode.
// It operates on the ApprovalAgent seam (approval_agent.go) so it lives
// here, out of the god package.
package approvals

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
	Surface    string            // "webui" or "cli" — which surface answered
	Assessment RiskAssessment    // echoed for caller diagnostics
	Analysis   *SecurityAnalysis // LLM-derived security analysis when available; nil otherwise
}

// RequestApproval performs the unified approval flow for a RiskAssessment.
// Low-risk auto-approves. Critical/hard-blocks deny unconditionally.
// Medium/High/IntentConfirmation checks bypass paths then tries WebUI, CLI,
// or falls back to permissive auto-approve in non-interactive mode.
func RequestApproval(a ApprovalAgent, assessment RiskAssessment, toolName string, args map[string]interface{}) (BrokerDecision, error) {
	// Command policy check (shell_command only) — must run before Low-risk early return.
	skipAllowlist := false
	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			cfg := a.GetConfig()
			if cfg != nil && cfg.CommandPolicies != nil {
				if action, matchedPattern, matched := EvaluateCommandPolicy(cmd, cfg.CommandPolicies); matched {
					switch action {
					case configuration.CommandPolicyDeny:
						a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
							a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
		a.LogSecurityDecision(toolName, args, assessment, "blocked")
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

	// Persistent allowlist for shell commands (skipped when a command policy "ask" rule matched)
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
		if a.DebugEnabled() {
			a.DebugLogf("[UNLOCK] RequestApproval auto-approve (unsafe mode): %s\n", toolName)
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
		if a.DebugEnabled() {
			a.DebugLogf("[UNLOCK] RequestApproval auto-approve (session elevated): %s\n", toolName)
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
		if a.DebugEnabled() {
			a.DebugLogf("[UNLOCK] RequestApproval auto-approve (unsafe-shell): %s\n", toolName)
		}
		return BrokerDecision{
			Approved:   true,
			Decision:   security.ApprovalApproveOnce,
			Surface:    "unsafe-shell",
			Assessment: assessment,
		}, nil
	}

	// Optional LLM analysis for shell commands (Medium/High risk only).
	// On error or timeout, securityAnalysis stays nil and we fall through.
	var securityAnalysis *SecurityAnalysis
	if toolName == "shell_command" &&
		(assessment.Level == configuration.RiskLevelMedium ||
			assessment.Level == configuration.RiskLevelHigh) {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			// Cache check — identical commands in the same session reuse the cached analysis.
			key := ChainCacheKey(cmd)
			if cached, ok := a.GetSecurityAnalysisCache().Get(key); ok {
				securityAnalysis = cached
			} else {
				ctx, cancel := context.WithTimeout(a.InterruptCtx(), 2*time.Second)
				sa, err := AnalyzeShellCommand(ctx, a.Client(), a.GetModel(), cmd, a.EffectiveCwd())
				cancel()
				if err == nil && sa != nil {
					securityAnalysis = sa
					a.GetSecurityAnalysisCache().Set(key, sa)
				}
				// On error or timeout: securityAnalysis stays nil; fall through.
			}
		}
	}

	// --- Surface availability (SP-168) ---

	isSubagent := a.IsSubagent()
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)

	// WebUI path. When a browser tab is connected, the WebUI IS the
	// interactive surface — the TTY status of os.Stdin is irrelevant. A
	// subagent's dialog is answerable the same way: subagents share the
	// parent's ApprovalManager and WebUI-client callback (SP-168,
	// createSubagent), so the request it publishes resolves against the
	// same pending map the parent's responses hit. A workflow/automate
	// run is excluded: even with a browser attached, nobody is guaranteed
	// to be at it, so the run must not block on the dialog and instead
	// resolves from the risk profile (non-interactive permissive).
	webUICanAnswer := a.HasActiveWebUIClients() && !a.IsWorkflowRun()

	// Subagents can prompt on the parent's terminal (SP-168): the parent's
	// turn is blocked inside run_subagent, so the TTY is free. When several
	// subagents run in parallel (run_parallel_subagents), interleaved
	// terminal prompts would corrupt each other — with more than one active
	// subagent, fall through to the mode default (non-interactive) instead.
	subagentCLISafe := !isSubagent || a.ActiveSubagentCount() <= 1
	canPrompt := logger != nil && logger.IsInteractive() && subagentCLISafe && !a.IsWorkflowRun()
	// surfaceAvailable (SP-168): a human can answer this request right now.
	// The auto risk judge defers "review" verdicts to a human only when a
	// surface is actually reachable.
	surfaceAvailable := webUICanAnswer || canPrompt
	if a.DebugEnabled() {
		a.DebugLogf("[APPROVAL] webUICanAnswer=%v canPrompt=%v (isSubagent=%v, hasWebUIClients=%v, hasMgr=%v, hasEventBus=%v)\n",
			webUICanAnswer, canPrompt, isSubagent, a.HasActiveWebUIClients(), a.GetSecurityApprovalMgr() != nil, a.GetEventBus() != nil)
	}

	// --- Auto risk judge (SP-168) ---
	// When enabled (the default), the LLM risk analysis computed above is
	// the approver for Medium/High shell commands — the "auto" mode:
	//   approve → run the command without a human prompt;
	//   reject  → fail fast with the LLM's reasoning, so the agent takes a
	//             different path instead of blocking on a timeout;
	//   review  → keep the human prompt when a surface can answer, or fail
	//             fast when it cannot (autonomous runs must not block or
	//             auto-approve an uncertain command).
	// Low-risk and hard-blocked commands never reach here. A nil analysis
	// (no LLM client, timeout, or not a Medium/High shell command) skips
	// the judge and the mode defaults apply: interactive surfaces prompt,
	// non-interactive runs auto-approve Medium from the risk profile.
	if toolName == "shell_command" && cfg != nil && cfg.AutoRiskJudge &&
		securityAnalysis != nil && !assessment.IsHardBlock {
		switch securityAnalysis.Recommendation {
		case "approve":
			if a.DebugEnabled() {
				a.DebugLogf("[APPROVAL] auto risk judge approved %s — %s\n", toolName, securityAnalysis.Summary)
			}
			a.LogSecurityDecision(toolName, args, assessment, "approved")
			return BrokerDecision{
				Approved: true, Decision: security.ApprovalApproveOnce,
				Outcome: security.ApprovalOutcomeResponded, Surface: "llm-judge",
				Assessment: assessment, Analysis: securityAnalysis,
			}, nil
		case "reject":
			a.LogSecurityDecision(toolName, args, assessment, "blocked")
			return BrokerDecision{
					Approved: false, Decision: security.ApprovalDeny,
					Outcome: security.ApprovalOutcomeResponded, Surface: "llm-judge",
					Assessment: assessment, Analysis: securityAnalysis,
				}, agenterrors.NewSecurityErrorWithAssessment(
					fmt.Sprintf("security rejected by risk judge: %s — %s. The risk judge judged this command too risky to run; take a different approach (a safer command, a narrower target, or confirm with the user via ask_user).",
						toolName, securityAnalysis.Summary), assessment.Explain(), nil,
				)
		case "review":
			if surfaceAvailable {
				break // fall through to the human surface below
			}
			a.LogSecurityDecision(toolName, args, assessment, "blocked")
			return BrokerDecision{
					Approved: false, Decision: security.ApprovalDeny,
					Outcome: security.ApprovalOutcomeNoChannel, Surface: "llm-judge",
					Assessment: assessment, Analysis: securityAnalysis,
				}, agenterrors.NewSecurityErrorWithAssessment(
					fmt.Sprintf("security review required: %s — %s. The risk judge needs a human reviewer for this command and this run has no interactive surface. Use a safer approach or confirm with the user via ask_user.",
						toolName, securityAnalysis.Summary), assessment.Explain(), nil,
				)
		default:
			// Missing or unrecognized recommendation: no verdict. Keep the
			// mode defaults (interactive surfaces prompt; non-interactive
			// runs auto-approve Medium from the risk profile).
		}
	}

	// --- Interactive approval surfaces ---

	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && webUICanAnswer {
		// Suspend CLI spinner before blocking on the webui response.
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		clihooks.SuspendStreaming()
		defer clihooks.ResumeIndicator()
		defer clihooks.ResumeSteer()
		defer clihooks.ResumeStreaming()

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

		// Attach LLM analysis to the WebUI extras for display.
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
			if toolName == "shell_command" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					a.ApplyApprovalDecision(decision, cmd)
				}
			}
			if !decision.Approved() {
				a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
			a.LogSecurityDecision(toolName, args, assessment, "approved")
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
		if a.DebugEnabled() {
			a.DebugLogf("[APPROVAL] webui approval unanswered (outcome=%v) for %s — falling back to CLI\n", outcome, toolName)
		}
	}

	// CLI path. (cfg, logger, subagentCLISafe, canPrompt are computed above,
	// before the auto risk judge, so the judge can tell whether a human can
	// answer — SP-168.)
	if a.DebugEnabled() {
		a.DebugLogf("[APPROVAL] CLI path: canPrompt=%v (isSubagent=%v)\n",
			canPrompt, isSubagent)
	}

	if canPrompt {
		// A subagent's prompt says whose command it is, so a user answering
		// at the parent's terminal knows what is being approved (SP-168).
		subagentLabel := ""
		if isSubagent {
			subagentLabel = fmt.Sprintf("Subagent (depth %d) is running: ", a.SubagentDepth())
		}

		// For shell_command: use the 4-option approval picker.
		if toolName == "shell_command" {
			// Per-part picker (opt-in via EditApprovalConfig.ShellCommand).
			if cfg != nil && cfg.EditApproval != nil && cfg.EditApproval.ShellCommand &&
				args["command"] != "" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					pickerCtx, pickerCancel := context.WithTimeout(a.InterruptCtx(), utils.ApprovalPromptTimeout)
					decisions, partIDs, pickErr := a.ApproveShellCommandParts(pickerCtx, cmd)
					pickerCancel()
					if pickErr != nil {
						a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
					for _, partID := range partIDs {
						if approved, ok := decisions[partID]; !ok || !approved {
							allApproved = false
							break
						}
					}
					if !allApproved {
						a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
					a.ApplyApprovalDecision(security.ApprovalApproveOnce, cmd)
					a.LogSecurityDecision(toolName, args, assessment, "approved")
					return BrokerDecision{
						Approved: true, Decision: security.ApprovalApproveOnce,
						Outcome: security.ApprovalOutcomeResponded, Surface: "cli",
						Assessment: assessment,
						Analysis:   securityAnalysis,
					}, nil
				}
			}

			if cmd, ok := args["command"].(string); ok && cmd != "" {
				prompt := subagentLabel + "Security Warning — " + string(assessment.Level)
				if assessment.RequiresIntentConfirmation {
					prompt = subagentLabel + "High-risk operation — your active risk profile gates this command."
				}
				// Convert the agent-level analysis to the leaf-level utils view
				// so the arrow-key picker can render it above the option list.
				var analysisView *utils.SecurityAnalysisView
				if securityAnalysis != nil {
					analysisView = &utils.SecurityAnalysisView{
						Summary:              securityAnalysis.Summary,
						Modifies:             securityAnalysis.Modifies,
						RiskAssessment:       securityAnalysis.RiskAssessment,
						Recommendation:       securityAnalysis.Recommendation,
						ChainLength:          securityAnalysis.ChainLength,
						ChainSubcommands:     securityAnalysis.ChainSubcommands,
						ChainClassifications: securityAnalysis.ChainClassifications,
					}
				}
				choice := logger.AskForApprovalWithOptions(prompt, cmd, analysisView)
				decision := ApprovalDecisionFromCLIChoice(choice)
				a.ApplyApprovalDecision(decision, cmd)
				if !decision.Approved() {
					a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
				a.LogSecurityDecision(toolName, args, assessment, "approved")
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
		prompt := subagentLabel + fmt.Sprintf("⚠  Security Warning — %s\n\nReasoning: %s\n\nDo you want to proceed?",
			strings.ToUpper(string(assessment.Level)), assessment.Reason)
		if !logger.AskForConfirmation(prompt, false, false) {
			a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
		a.LogSecurityDecision(toolName, args, assessment, "approved")
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
	if a.IsNonInteractive() {
		if a.DebugEnabled() {
			a.DebugLogf("[non-interactive] auto-approving %s (level: %s) — no interactive surface\n",
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
	a.LogSecurityDecision(toolName, args, assessment, "blocked")
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
