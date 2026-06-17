package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ExecuteTool executes a tool with standardized parameter validation and error handling
func (r *ToolRegistry) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent) ([]api.ImageData, string, error) {
	handler, found := tools.GetNewToolRegistry().Lookup(toolName)
	if !found {
		return nil, "", fmt.Errorf("unknown tool '%s'", toolName)
	}

	// SP-063: computer-use tools are restricted to the computer_user persona.
	// Inert unless computer use is enabled (the restricted-name set is empty).
	if isComputerUseToolBlocked(toolName, agent) {
		return nil, "", fmt.Errorf("tool %q is only available to the computer_user persona", toolName)
	}

	if agent != nil && agent.debug {
		agent.debugLog("[tool] tool dispatched via new registry: %s\n", toolName)
	}

	// CRITICAL: Depth-based subagent nesting prevention
	// Agents at or beyond the maximum nesting depth cannot spawn further subagents.
	// This prevents runaway agent chains while allowing configurable multi-level nesting
	// (e.g., EA (depth=0) → orchestrator (depth=1) → coder/tester (depth=2)).
	// ask_user is NOT blocked for subagents — they share the event bus and questions
	// are routed through the same WebUI/CLI prompt mechanism as the primary agent.
	if agent != nil && !agent.CanSpawnSubagents() {
		if toolName == "run_subagent" || toolName == "run_parallel_subagents" {
			errMsg := fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
				"This restriction prevents runaway agent chains and ensures proper task delegation. "+
				"If you need additional work done, please complete your current task and return "+
				"your results to the parent agent for further delegation.",
				agent.SubagentDepth(), agent.MaxSubagentDepth())
			if agent != nil && agent.debug {
				agent.debugLog("[NO] Blocked subagent tool '%s' at depth %d (max: %d)\n", toolName, agent.SubagentDepth(), agent.MaxSubagentDepth())
			}
			return nil, "", agenterrors.NewSecurityError(errMsg, nil)
		}
	}

	// Security validation — classify and block/prompt dangerous operations
	if secResult := tools.ClassifyToolCall(toolName, args); secResult.ShouldBlock || secResult.ShouldPrompt || secResult.IntentConfirmation {
		// Workflow-declared auto-approval for run_automate. When the
		// workflow JSON sets requires_approval: false, the model can
		// invoke it without a user prompt — designed for validation
		// workflows referenced from AGENTS.md that the model must run
		// before declaring work done. Failure to read the file falls
		// through to the normal approval path (fail-safe).
		workflowAutoApproved := false
		if agent != nil && toolName == "run_automate" && secResult.IntentConfirmation {
			if wf, ok := args["workflow"].(string); ok && wf != "" {
				if !workflowRequiresApproval(wf) {
					if agent.debug {
						agent.debugLog("[UNLOCK] run_automate %q has requires_approval=false — skipping intent prompt\n", wf)
					}
					workflowAutoApproved = true
				}
			}
		}
		// In-session re-authorization for run_automate. Once the user has
		// explicitly approved a workflow during this chat session, subsequent
		// calls (e.g. retries kicked off by the primary agent after a failure)
		// don't need to re-prompt — the user opted in once for this workflow.
		alreadyApprovedInSession := false
		if !workflowAutoApproved && agent != nil && toolName == "run_automate" && secResult.IntentConfirmation {
			if wf, ok := args["workflow"].(string); ok && agent.IsWorkflowApprovedInSession(wf) {
				if agent.debug {
					agent.debugLog("[UNLOCK] run_automate %q already approved in this session — skipping intent prompt\n", wf)
				}
				alreadyApprovedInSession = true
			}
		}
		if workflowAutoApproved || alreadyApprovedInSession {
			// fall through to handler execution below
		} else if agent != nil && agent.staticGateAutoApprove(secResult) && !secResult.IntentConfirmation {
			// Unsafe mode or session elevation — skip the prompt for
			// non-hard-block operations. See staticGateAutoApprove.
			// IntentConfirmation is never auto-approved — it's about
			// explicit user intent, not risk bypass.
			if agent.debug {
				agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated): bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
			}
		} else if agent != nil && agent.GetUnsafeShellMode() && toolName == "shell_command" && !secResult.IsHardBlock && secResult.Risk.String() != "DANGEROUS" && !secResult.IntentConfirmation {
			// --unsafe-shell bypasses CAUTION-tier shell prompts across all
			// modes (CLI and WebUI) so the flag behaves consistently regardless
			// of UI. DANGEROUS and hard-block operations still require approval.
			// IntentConfirmation is never auto-approved here either.
			if agent.debug {
				agent.debugLog("[UNLOCK] Unsafe shell mode: bypassing shell security prompt for %s (risk: %s)\n", toolName, secResult.Risk)
			}
		} else if agent == nil && (secResult.ShouldBlock || secResult.IntentConfirmation) {
			// Defense-in-depth: no agent context available for approval,
			// so reject operations that require it.
			return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security: %s — %s (no agent context for approval)", toolName, secResult.Reasoning), nil)
		} else if agent != nil {
			// Check if we're running as a subagent — subagents cannot prompt
			isSubagent := agent.IsSubagent()

			// When true, the browser dialog conclusively answered (approve or
			// deny); skip the CLI fallback. Stays false when the webui path is
			// unavailable or the dialog went unanswered (timeout / disconnect),
			// in which case we fall through to the terminal prompt below so an
			// unattended browser tab can't dead-end the agent.
			approvedViaWebUI := false

			// Prefer webui approval path when a browser tab is connected.
			// When the process has an active webui client, the query likely
			// originated from the browser. Sending the approval request through
			// the event bus ensures the dialog appears in the webui. The CLI
			// interactive prompt is unreliable in this case because stdin may
			// belong to the terminal that launched the server — the user is
			// interacting via the browser, not the terminal.
			if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent && agent.HasActiveWebUIClients() {
				// WEBUI: request approval via event bus for the browser dialog
				if agent.debug {
					agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", toolName, secResult.Risk)
				}
				// Build extras with context the webui dialog needs (command, target, risk type)
				extras := map[string]string{}
				if secResult.IntentConfirmation {
					extras["intent_confirmation"] = "true"
				}
				if secResult.RiskType != "" {
					extras["risk_type"] = formatRiskType(secResult.RiskType)
				}
				switch toolName {
				case "shell_command":
					if cmd, ok := args["command"].(string); ok && cmd != "" {
						extras["command"] = cmd
					}
				case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
					if path, ok := args["path"].(string); ok && path != "" {
						extras["target"] = path
					}
				case "git":
					if op, ok := args["operation"].(string); ok && op != "" {
						extras["target"] = fmt.Sprintf("git %s", op)
					}
				case "run_automate":
					if wf, ok := args["workflow"].(string); ok && wf != "" {
						extras["target"] = fmt.Sprintf("workflow: %s", wf)
					}
				}
				approved, outcome := mgr.RequestToolApprovalWithOutcome(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, secResult.Risk.String(), secResult.Reasoning, extras)
				if outcome == security.ApprovalOutcomeResponded {
					if !approved {
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", toolName, secResult.Reasoning), nil)
					}
					// Signal Gate 2 (persona cascade) that this command
					// already passed an interactive approval so it doesn't
					// re-prompt for the same execution (SP-058 follow-up).
					agent.recordGateApproval(toolName, args)
					if toolName == "run_automate" {
						if wf, ok := args["workflow"].(string); ok {
							agent.MarkWorkflowApprovedInSession(wf)
						}
					}
					approvedViaWebUI = true
				} else if agent.debug {
					// Timed out or the browser disconnected — don't treat an
					// unanswered dialog as a deny. Fall through to the CLI
					// prompt below so a user at the terminal can respond.
					agent.debugLog("[APPROVAL] webui approval unanswered (outcome=%d) for %s — falling back to CLI prompt\n", outcome, toolName)
				}
			}
			if !approvedViaWebUI {
				// CLI: prompt user interactively via terminal stdin
				agentConfig := agent.GetConfig()
				logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
				canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

				if canPrompt {
					var prompt string
					if secResult.IntentConfirmation {
						prompt = buildIntentConfirmationPrompt(toolName, args, secResult)
					} else {
						prompt = buildSecurityPrompt(toolName, args, secResult)
					}
					if !logger.AskForConfirmation(prompt, false, false) {
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", toolName, secResult.Reasoning), nil)
					}
					// Same approval-propagation as the webui branch above.
					agent.recordGateApproval(toolName, args)
					if toolName == "run_automate" {
						if wf, ok := args["workflow"].(string); ok {
							agent.MarkWorkflowApprovedInSession(wf)
						}
					}
				} else if secResult.ShouldBlock {
					// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security block: %s — %s", toolName, secResult.Reasoning), nil)
				} else if secResult.IntentConfirmation {
					// NON-INTERACTIVE + intent confirmation required: must ask user first
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("confirmation required: %s — %s (this operation requires explicit user confirmation. Use ask_user to confirm with the user before proceeding.)", toolName, secResult.Reasoning), nil)
				} else if secResult.ShouldPrompt && !isSubagent {
					// NON-INTERACTIVE + CAUTION, needs prompt but no approval mechanism:
					// Return a terminal SecurityError — the operation cannot proceed
					// without interactive approval. LLMs reliably honor "do not retry."
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf(
						"security block: %s — %s. This operation requires interactive user approval. To proceed, the user must re-run interactively or grant a scoped bypass via --unsafe-shell. Do not retry this exact command.",
						toolName, secResult.Reasoning), nil)
				}
				// NON-INTERACTIVE + CAUTION, no approval mechanism, not a subagent: auto-allow (safe operations)
			}
		}
	}

	// Build ToolEnv from agent context
	var env tools.ToolEnv
	if agent != nil {
		env.EventBus = agent.GetEventBus()
		env.WorkspaceRoot = agent.GetWorkspaceRoot()
		// SP-074-2: Route tool output through the agent's output system
		// (PrintLineAsync → OutputRouter) instead of os.Stdout.
		env.OutputWriter = newOutputRouter(agent)
		env.MaxTokensFunc = func() int { return agent.GetMaxContextTokens() }
		env.ConfigManager = agent.GetConfigManager()
		env.AskUser = newAgentAskUserService(agent)
		env.TodoManager = agent.GetTodoManager()
		// Interactive CLI means: no browser client connected AND stdin is a TTY.
		env.IsInteractiveCLI = !agent.HasActiveWebUIClients() && !isNonInteractive()
		// SP-074-3: Wire ApprovalManager adapter so migrated tools can
		// request security approvals through the normal CLI/WebUI flow.
		env.ApprovalManager = newToolsApprovalAdapter(agent)
	} else {
		env.OutputWriter = os.Stdout
		env.MaxTokensFunc = func() int { return 0 }
	}

	if err := handler.Validate(args); err != nil {
		return nil, "", fmt.Errorf("validation failed for tool %q: %w", toolName, err)
	}
	res, err := handler.Execute(ctx, env, args)
	if err != nil {
		return nil, "", err
	}

	// Convert tools.ImageData [] → []api.ImageData
	var images []api.ImageData
	if len(res.Images) > 0 {
		images = make([]api.ImageData, len(res.Images))
		for i, img := range res.Images {
			images[i] = api.ImageData{
				URL:    img.URI,
				Type:   img.MIMEType,
			}
		}
	}

	output := res.Output
	if res.IsError {
		errMsg := output
		if errMsg == "" {
			errMsg = fmt.Sprintf("tool %q returned error state", toolName)
		}
		if agent != nil && agent.debug {
			agent.debugLog("[tool] tool dispatched via new registry (error): %s\n", toolName)
		}
		return images, "", fmt.Errorf("%s", errMsg)
	}

	// After successful tool execution, run embedding duplicate check for write tools.
	if output != "" {
		if shouldCheckDuplicates(toolName, agent) {
			if path, ok := args["path"].(string); ok && path != "" {
				note := runDuplicateCheck(ctx, agent, path)
				if note != "" {
					output = output + note
				}
				// Keep the index fresh — async so the agent response
				// isn't blocked on re-embedding.
				reindexFileAfterWrite(agent, path)
			}
		}
	}

	return images, output, nil
}

// staticGateAutoApprove reports whether a tool call that the static
// classifier (Gate 1) flagged as risky should skip the interactive
// approval prompt because the session is in a bypass state:
//
//   - Unsafe mode: every security check is off.
//   - Session elevation: the active risk profile is permissive or
//     unrestricted (the user clicked "Elevate (session)" on a prior
//     dialog or ran /risk-profile permissive), so non-hard-block
//     operations auto-approve for the rest of the session.
//
// Hard blocks (critical system operations such as rm -rf /) are never
// auto-approved here — they fall through to the caller's block/prompt
// handling regardless of elevation.
//
// Both Gate-1 entry points call this — ToolRegistry.ExecuteTool and the
// live seed pre-execute hook (newPreExecuteHook) — so the bypass policy
// lives in exactly one place and the two paths can't drift.
func (a *Agent) staticGateAutoApprove(secResult tools.SecurityResult) bool {
	if a == nil {
		return false
	}
	if a.GetUnsafeMode() {
		return true
	}
	if a.IsSessionElevated() && !secResult.IsHardBlock {
		return true
	}
	return false
}

// unifiedSecurityGate is the Phase-2 security gate. When
// UnifiedRiskResolver is ON it replaces the split Gate 1 / Gate 2
// call-site path with a single ResolveToolRisk assessment.
//
// The decision mapping mirrors the existing call-site logic:
//
//	Critical/IsHardBlock → hard-block error
//	High                → highRiskApprovedForCommand (same as today)
//	Medium              → go through the interactive approval flow
//	(reuse existing
//	 prompt wiring)
//	Low                 → allow
//
// The function returns nil when the call is allowed to proceed, or an
// error when it should be blocked.
func (a *Agent) unifiedSecurityGate(name string, args map[string]interface{}) error {
	assessment := a.ResolveToolRisk(name, args)

	if a.debug {
		a.debugLog("[unified-gate] %s: %s\n", name, assessment.Explain())
	}

	// Hard blocks are unconditional — no approval path can override
	if assessment.IsHardBlock || assessment.Level == configuration.RiskLevelCritical {
		return agenterrors.NewSecurityError(
			fmt.Sprintf("critical operation blocked (cannot be approved): %s", assessment.Reason), nil,
		)
	}

	// High risk: reuse the existing approval cascade (EA persona reasons,
	// interactive users get prompted, non-interactive subagents are blocked)
	if assessment.Level == configuration.RiskLevelHigh {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if !a.highRiskApprovedForCommand(nil, cmd) {
				return agenterrors.NewSecurityError(
					fmt.Sprintf("high-risk operation rejected by unified resolver: %s", assessment.Reason), nil,
				)
			}
		} else {
			// Non-shell tool at High risk — go through the interactive prompt
			return a.unifiedSecurityPrompt(name, args, assessment)
		}
	}

	// Workflow-declared auto-approval for run_automate. When the
	// workflow JSON sets requires_approval: false, the model can
	// invoke it without a user prompt — designed for validation
	// workflows referenced from AGENTS.md that the model must run
	// before declaring work done. Failure to read the file falls
	// through to the normal intent-confirmation path (fail-safe).
	// In-session re-authorization for run_automate. Once the user has
	// explicitly approved a workflow during this chat session, subsequent
	// calls (e.g. retries kicked off by the primary agent after a failure)
	// don't need to re-prompt — the user opted in once for this workflow.
	if name == "run_automate" && assessment.RequiresIntentConfirmation {
		if wf, ok := args["workflow"].(string); ok && wf != "" {
			if !workflowRequiresApproval(wf) {
				if a.debug {
					a.debugLog("[UNLOCK] run_automate %q has requires_approval=false — skipping intent prompt\n", wf)
				}
				return nil
			}
			if a.IsWorkflowApprovedInSession(wf) {
				if a.debug {
					a.debugLog("[UNLOCK] run_automate %q already approved in this session — skipping intent prompt\n", wf)
				}
				return nil
			}
		}
	}

	// Intent confirmation is orthogonal to risk level — safe-but-consequential
	// ops still need explicit user intent
	if assessment.RequiresIntentConfirmation {
		if a.IsSubagent() {
			return agenterrors.NewSecurityError(
				fmt.Sprintf("confirmation required: %s — %s (this operation requires explicit user confirmation. Use ask_user to confirm with the user before proceeding.)", name, assessment.Reason), nil,
			)
		}
		// For intent confirmation, go through the approval prompt
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Medium risk: needs interactive approval
	if assessment.Level == configuration.RiskLevelMedium {
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Low risk: allow
	return nil
}

// unifiedSecurityPrompt handles the interactive approval flow for Medium
// risk or intent-confirmation operations in the unified gate. Reuses the
// existing WebUI / CLI prompt wiring so the UX doesn't change.
func (a *Agent) unifiedSecurityPrompt(name string, args map[string]interface{}, assessment RiskAssessment) error {
	isSubagent := a.IsSubagent()

	// Unsafe mode or session elevation skips prompts for non-hard-block
	// operations in the unified path too.
	if a.GetUnsafeMode() || a.IsSessionElevated() {
		if a.debug {
			a.debugLog("[UNLOCK] Unified gate auto-approve (unsafe/elevated): %s\n", name)
		}
		return nil
	}

	// --unsafe-shell bypasses Medium-tier shell prompts in the unified path
	// so the flag behaves consistently regardless of which gate path is active.
	if a.GetUnsafeShellMode() && name == "shell_command" &&
		assessment.Level == configuration.RiskLevelMedium &&
		!assessment.RequiresIntentConfirmation {
		if a.debug {
			a.debugLog("[UNLOCK] Unsafe shell mode: bypassing shell security prompt for %s (level: %s)\n", name, assessment.Level)
		}
		return nil
	}

	// Persistent allowlist for shell commands
	if name == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" && a.IsShellCommandAllowlisted(cmd) {
			a.markShellCommandApproved(cmd)
			return nil
		}
	}

	// WebUI approval path
	if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && !isSubagent && a.HasActiveWebUIClients() {
		extras := map[string]string{}
		extras["risk_level"] = string(assessment.Level)
		switch name {
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
		riskLabel := string(assessment.Level)
		if name == "shell_command" && assessment.RequiresIntentConfirmation {
			riskLabel = "INTENT"
		}
		if assessment.RequiresIntentConfirmation {
			extras["intent_confirmation"] = "true"
		}
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			decision := mgr.RequestToolApprovalDecision(a.GetEventBus(), a.GetEventClientID(), a.GetEventUserID(), name, riskLabel, assessment.Reason, extras)
			if !decision.Approved() {
				return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, assessment.Reason), nil)
			}
			a.applyApprovalDecision(decision, cmd)
			a.markShellCommandApproved(cmd)
			return nil
		}
		if !mgr.RequestToolApproval(a.GetEventBus(), a.GetEventClientID(), a.GetEventUserID(), name, riskLabel, assessment.Reason, extras) {
			return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, assessment.Reason), nil)
		}
		return nil
	}

	// CLI approval path
	cfg := a.GetConfig()
	logger := utils.GetLogger(cfg != nil && cfg.SkipPrompt)
	canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

	if canPrompt {
		prompt := fmt.Sprintf("⚠  Security Warning — %s\n\nReasoning: %s\n\nDo you want to proceed?",
			strings.ToUpper(string(assessment.Level)), assessment.Reason)
		if !logger.AskForConfirmation(prompt, false, false) {
			return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, assessment.Reason), nil)
		}
		return nil
	}

	// Non-interactive: block
	return agenterrors.NewSecurityError(
		fmt.Sprintf("security block: %s — %s. This operation requires interactive user approval. To proceed, the user must re-run interactively or grant a scoped bypass via --unsafe-shell. Do not retry this exact command.",
			name, assessment.Reason), nil,
	)
}

// buildSecurityPrompt constructs a detailed security approval prompt for the user
func buildSecurityPrompt(toolName string, args map[string]interface{}, secResult tools.SecurityResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("⚠  Security Warning — %s\n\n", secResult.Risk))

	// Show the actual command/operation
	switch toolName {
	case "shell_command":
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			sb.WriteString(fmt.Sprintf("Command:\n  %s\n\n", cmd))
		}
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := args["path"].(string); ok && path != "" {
			sb.WriteString(fmt.Sprintf("Target: %s\n\n", path))
		}
	case "git":
		if op, ok := args["operation"].(string); ok && op != "" {
			sb.WriteString(fmt.Sprintf("Operation: git %s\n\n", op))
		}
	}

	if secResult.RiskType != "" {
		sb.WriteString(fmt.Sprintf("Risk category: %s\n\n", formatRiskType(secResult.RiskType)))
	}

	sb.WriteString(fmt.Sprintf("Reasoning: %s\n\n", secResult.Reasoning))
	// Trailing question only — AskForConfirmation appends the
	// "[y/N]" hint itself. Including "(yes/no):" here used to
	// produce "...(yes/no):  [y/N]:" (duplicate suffix).
	sb.WriteString("Do you want to proceed?")

	return sb.String()
}

// buildShellApprovalPrompt builds the header text for the 4-option shell
// approval picker (AskForApprovalWithOptions → the SelectList renderer).
//
// Unlike buildSecurityPrompt (used by the raw yes/no AskForConfirmation
// path), it deliberately omits the leading warning glyph AND the command
// block: the picker's renderer (pkg/console.writeSecurityHeader) prepends
// the ⚠ glyph and prints the command on its own block. Including them here
// double-rendered both — the source of the "⚠ ⚠" and the duplicated
// "Command:" block. The picker itself asks the question, so no trailing
// "Do you want to proceed?" either.
// buildIntentConfirmationPrompt constructs a confirmation prompt for consequential
// but safe operations (like launching an autonomous workflow). Uses neutral framing
// instead of security-warning framing — the operation isn't dangerous, just impactful.
func buildIntentConfirmationPrompt(toolName string, args map[string]interface{}, secResult tools.SecurityResult) string {
	var sb strings.Builder

	sb.WriteString("▶  Confirmation Required\n\n")

	switch toolName {
	case "run_automate":
		if wf, ok := args["workflow"].(string); ok && wf != "" {
			sb.WriteString(fmt.Sprintf("Workflow: %s\n\n", wf))
		}
	}

	sb.WriteString(fmt.Sprintf("%s\n\n", secResult.Reasoning))
	sb.WriteString("Do you want to proceed?")

	return sb.String()
}

func buildShellApprovalPrompt(secResult tools.SecurityResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Security Warning — %s", secResult.Risk))
	if secResult.RiskType != "" {
		sb.WriteString(fmt.Sprintf("\n\nRisk category: %s", formatRiskType(secResult.RiskType)))
	}
	if secResult.Reasoning != "" {
		sb.WriteString(fmt.Sprintf("\n\nReasoning: %s", secResult.Reasoning))
	}
	return sb.String()
}

// formatRiskType returns a human-readable description for a risk type
func formatRiskType(riskType string) string {
	switch riskType {
	case "mass_deletion":
		return "Mass deletion — may delete all files in current directory or home"
	case "source_code_destruction":
		return "Source code destruction — may delete project source files"
	case "privilege_escalation":
		return "Privilege escalation — running with elevated permissions"
	case "remote_code_execution":
		return "Remote code execution — downloading and executing untrusted code"
	case "arbitrary_code_execution":
		return "Arbitrary code execution — executing arbitrary shell commands"
	case "destructive_git_operation":
		return "Destructive git operation — may rewrite published history"
	case "disk_destruction":
		return "Disk destruction — may destroy disk data or partition tables"
	case "critical_system_operation":
		return "Critical system operation — may cause irreversible system damage"
	case "system_instability":
		return "System instability — may crash the system or kill all processes"
	case "insecure_permissions":
		return "Insecure permissions — setting overly permissive file access"
	case "system_integrity":
		return "System integrity — writing to critical system files"
	default:
		return riskType
	}
}

// handleFileSecurityError checks if an error is due to filesystem security and prompts the user
// Returns a context with security bypass enabled if user approves, original context otherwise
func handleFileSecurityError(ctx context.Context, agent *Agent, toolName, filePath string, err error) (context.Context, bool) {
	// Check if this is a filesystem security error
	if !errors.Is(err, filesystem.ErrOutsideWorkingDirectory) && !errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		return ctx, false
	}

	// Unsafe mode bypasses filesystem security checks automatically
	if agent.GetUnsafeMode() {
		agent.debugLog("[UNLOCK] Unsafe mode: automatically allowing file access outside working directory: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Session elevation (user clicked "Elevate (session)" on a prior
	// approval) bypasses filesystem prompts for non-sensitive paths.
	// Sensitive-tier paths (system dirs, off-CWD home) still prompt
	// even under elevation — they're never session-allowlisted.
	if agent.IsSessionElevated() {
		tier := ClassifyPathAccess(filePath, agent.GetWorkspaceRoot(), detectHomeDir(), agent.effectiveCwd())
		if tier != PathTierSensitive {
			agent.debugLog("[UNLOCK] Session elevated: automatically allowing file access outside working directory: %s (tier=%s)\n", filePath, tier)
			return filesystem.WithSecurityBypass(ctx), true
		}
		// Sensitive path under elevation: fall through to normal prompt.
		agent.debugLog("[APPROVAL] Sensitive-tier path still prompts under elevation: %s\n", filePath)
	}

	// Per-folder session allowlist short-circuit. If this path sits
	// under a folder the user previously approved, skip the prompt.
	if agent.IsFolderSessionAllowed(filePath) {
		agent.debugLog("[UNLOCK] Folder is on session allowlist: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Classify the path so we can pick the right dialog mode and
	// scope. Sensitive (system dirs, off-CWD home) gets 2 options;
	// External gets 3 options including "Allow folder this session".
	tier := ClassifyPathAccess(filePath, agent.GetWorkspaceRoot(), detectHomeDir(), agent.effectiveCwd())
	folder := filepath.Dir(filePath)

	// Subagents cannot prompt — return unapproved so the error propagates
	if agent.IsSubagent() {
		agent.debugLog("Subagent encountered filesystem security error for %s, delegating to primary agent\n", filePath)
		return ctx, false
	}

	// Prefer webui approval path when a browser tab is connected.
	if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && agent.HasActiveWebUIClients() {
		kind := "fs_external"
		if tier == PathTierSensitive {
			kind = "fs_sensitive"
		}
		prompt := fmt.Sprintf("The tool '%s' is attempting to access a file outside the working directory.", toolName)
		extras := map[string]string{
			"risk_type": "Filesystem Security",
			"target":    filePath,
			"path":      filePath,
			"kind":      kind,
		}
		if tier == PathTierExternal {
			extras["folder"] = folder
		}
		decision := mgr.RequestToolApprovalDecision(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, "CAUTION", prompt, extras)
		return applyFilesystemDecision(ctx, agent, decision, filePath, folder, tier)
	}

	// CLI: prompt user interactively via terminal stdin
	agentConfig := agent.GetConfig()
	logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
	canPrompt := logger != nil && logger.IsInteractive()

	if canPrompt {
		promptTier := utils.FilesystemPromptExternal
		if tier == PathTierSensitive {
			promptTier = utils.FilesystemPromptSensitive
		}
		// No leading glyph — the picker renderer prepends the ⚠ (avoids "⚠ ⚠").
		prompt := fmt.Sprintf("Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory.", toolName)
		choice := logger.AskForFilesystemApproval(prompt, filePath, folder, promptTier)
		decision := filesystemDecisionFromCLIChoice(choice)
		return applyFilesystemDecision(ctx, agent, decision, filePath, folder, tier)
	}

	// No prompting available — return unapproved
	if agent.debug {
		agent.debugLog("Cannot prompt for filesystem security approval (no mechanism): %s\n", filePath)
	}
	return ctx, false
}

// applyFilesystemDecision performs the side effects of the user's
// choice on a filesystem approval dialog and returns the (ctx, ok)
// pair the caller (handleFileSecurityError) returns to the tool layer.
//
//   - ApprovalDeny → reject (no ctx mutation).
//   - ApprovalApproveOnce → allow this invocation only (ctx gets the
//     bypass token; nothing recorded for future calls).
//   - ApprovalAllowFolderSession → External tier only: add the folder
//     to the agent's session allowlist AND allow this invocation.
//     Silently demoted to ApproveOnce if the tier is Sensitive (the
//     dialog shouldn't have offered the choice, but defense-in-depth).
//
// Decisions intended for the shell flow (ApproveAlways, Elevate)
// don't apply here; if encountered they collapse to ApproveOnce so
// the invocation still proceeds and no shell-specific side effect
// fires for a filesystem operation.
func applyFilesystemDecision(ctx context.Context, agent *Agent, decision security.ApprovalDecision, filePath, folder string, tier PathTier) (context.Context, bool) {
	switch decision {
	case security.ApprovalDeny:
		agent.debugLog("[APPROVAL] User denied file access outside working directory: %s\n", filePath)
		return ctx, false
	case security.ApprovalAllowFolderSession:
		if tier == PathTierSensitive {
			// Sensitive paths can never be allowlisted. If we got
			// this decision anyway (broken client / API misuse),
			// treat it as a one-shot approval so the user isn't
			// silently widened.
			agent.debugLog("[APPROVAL] Refusing to allowlist Sensitive tier path %s; demoting to one-shot approval\n", filePath)
			return filesystem.WithSecurityBypass(ctx), true
		}
		agent.debugLog("[APPROVAL] User approved folder %s for the rest of this session (path: %s)\n", folder, filePath)
		agent.AddSessionAllowedFolder(folder)
		return filesystem.WithSecurityBypass(ctx), true
	case security.ApprovalElevate:
		// User clicked "Elevate (session)" on a filesystem dialog.
		// Apply the session-wide elevation so ALL subsequent gates
		// (static classifier, filesystem, shell cascade) skip prompts.
		agent.ElevateSessionToPermissive()
		agent.debugLog("[APPROVAL] Session elevated from filesystem dialog for: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	default:
		// ApprovalApproveOnce + any other decision collapses to a
		// single-invocation approval.
		agent.debugLog("[APPROVAL] User approved file access (one-shot): %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}
}

// filesystemDecisionFromCLIChoice maps the CLI prompt's typed choice
// to the shared security.ApprovalDecision so the post-prompt handling
// is the same regardless of input surface.
func filesystemDecisionFromCLIChoice(c utils.ApprovalChoice) security.ApprovalDecision {
	switch c {
	case utils.ApprovalChoiceApproveOnce:
		return security.ApprovalApproveOnce
	case utils.ApprovalChoiceAllowFolderSession:
		return security.ApprovalAllowFolderSession
	default:
		return security.ApprovalDeny
	}
}

// outputRouter implements io.Writer by routing writes through the agent's
// PrintLineAsync method. It buffers partial lines and flushes them on newline
// boundaries, so streaming output from tools appears in the console the same
// way it would on a real terminal.
type outputRouter struct {
	agent *Agent
	buf   bytes.Buffer
}

// newOutputRouter creates an io.Writer that routes tool output through the
// agent's output system instead of writing directly to os.Stdout.
func newOutputRouter(agent *Agent) io.Writer {
	return &outputRouter{agent: agent}
}

// Write implements io.Writer. It accumulates data in an internal buffer and
// flushes complete lines (terminated by \n) via PrintLineAsync. Any remaining
// buffered data is held until the next Write call brings a newline.
func (w *outputRouter) Write(p []byte) (int, error) {
	if w.agent == nil {
		// Fallback: write to os.Stdout if no agent is available
		return os.Stdout.Write(p)
	}
	w.buf.Write(p)
	for {
		idx := bytes.IndexByte(w.buf.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := w.buf.Next(idx + 1)
		// Trim the trailing newline for PrintLineAsync
		w.agent.PrintLineAsync(strings.TrimRight(string(line), "\n"))
	}
	return len(p), nil
}

// toolsApprovalAdapter wraps the agent's security.ApprovalManager and event
// bus, translating calls from the tools.ApprovalManager interface to the
// security package's signature.
type toolsApprovalAdapter struct {
	approvalMgr *security.ApprovalManager
	eventBus    *events.EventBus
	clientID    string
	userID      string
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
		approvalMgr: mgr,
		eventBus:    agent.GetEventBus(),
		clientID:    agent.GetEventClientID(),
		userID:      agent.GetEventUserID(),
	}
}

// RequestApproval implements tools.ApprovalManager by translating the call
// to security.ApprovalManager.RequestApprovalDecisionWithOutcome and
// converting the decision+outcome to a tools.ApprovalResult.
//
// requestID is intentionally ignored — the security layer generates its own ID.
func (a *toolsApprovalAdapter) RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) tools.ApprovalResult {
	// requestID is intentionally ignored — the security layer generates its own ID
	req := security.ApprovalRequest{
		Kind:            security.ApprovalKindTool,
		DefaultResponse: false,
		ToolName:        toolName,
		RiskLevel:       riskLevel,
		Reasoning:       prompt,
		ClientID:        a.clientID,
		UserID:          a.userID,
		Extras:          extras,
	}
	decision, outcome := a.approvalMgr.RequestApprovalDecisionWithOutcome(a.eventBus, req)

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
