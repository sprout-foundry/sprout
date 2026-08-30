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
	Surface    string            // "webui" or "cli" — which surface answered
	Assessment RiskAssessment    // echoed for caller diagnostics
	Analysis   *SecurityAnalysis // LLM-derived security analysis when available; nil otherwise
}

// RequestApproval performs the unified approval flow for a RiskAssessment.
// Low-risk auto-approves. Critical/hard-blocks deny unconditionally.
// Medium/High/IntentConfirmation checks bypass paths then tries WebUI, CLI,
// or falls back to permissive auto-approve in non-interactive mode.
func (a *Agent) RequestApproval(assessment RiskAssessment, toolName string, args map[string]interface{}) (BrokerDecision, error) {
	// Command policy check (shell_command only) — must run before Low-risk early return.
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

	// Optional LLM analysis for shell commands (Medium/High risk only).
	// On error or timeout, securityAnalysis stays nil and we fall through.
	var securityAnalysis *SecurityAnalysis
	if toolName == "shell_command" &&
		(assessment.Level == configuration.RiskLevelMedium ||
			assessment.Level == configuration.RiskLevelHigh) {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			// Cache check — identical commands in the same session reuse the cached analysis.
			key := ChainCacheKey(cmd)
			if cached, ok := a.getSecurityAnalysisCache().Get(key); ok {
				securityAnalysis = cached
			} else {
				ctx, cancel := context.WithTimeout(a.interruptCtx, 2*time.Second)
				sa, err := AnalyzeShellCommand(ctx, a, cmd, a.effectiveCwd())
				cancel()
				if err == nil && sa != nil {
					securityAnalysis = sa
					a.getSecurityAnalysisCache().Set(key, sa)
				}
				// On error or timeout: securityAnalysis stays nil; fall through.
			}
		}
	}

	// --- Interactive approval surfaces ---

	isSubagent := a.IsSubagent()

	// WebUI path. When a browser tab is connected, the WebUI IS the
	// interactive surface — the TTY status of os.Stdin is irrelevant.
	webUICanAnswer := !isSubagent && a.HasActiveWebUIClients()
	if a.debug {
		a.debugLog("[APPROVAL] webUICanAnswer=%v (isSubagent=%v, hasWebUIClients=%v, hasMgr=%v, hasEventBus=%v)\n",
			webUICanAnswer, isSubagent, a.HasActiveWebUIClients(), a.GetSecurityApprovalMgr() != nil, a.GetEventBus() != nil)
	}
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

	if a.debug {
		a.debugLog("[APPROVAL] CLI path: canPrompt=%v (logger=%v, isInteractive=%v, isSubagent=%v)\n",
			canPrompt, logger != nil, logger != nil && logger.IsInteractive(), isSubagent)
	}

	if canPrompt {
		// For shell_command: use the 4-option approval picker.
		if toolName == "shell_command" {
			// Per-part picker (opt-in via EditApprovalConfig.ShellCommand).
			if cfg != nil && cfg.EditApproval != nil && cfg.EditApproval.ShellCommand &&
				args["command"] != "" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					proposal := NewShellProposal(cmd)
					pickerCtx, pickerCancel := context.WithTimeout(a.interruptCtx, utils.ApprovalPromptTimeout)
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
