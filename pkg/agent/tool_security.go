package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
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
		if agent != nil && agent.staticGateAutoApprove(secResult) && !secResult.IntentConfirmation {
			// Unsafe mode or session elevation — skip the prompt for
			// non-hard-block operations. See staticGateAutoApprove.
			// IntentConfirmation is never auto-approved — it's about
			// explicit user intent, not risk bypass.
			if agent.debug {
				agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated): bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
			}
		} else if agent == nil && (secResult.ShouldBlock || secResult.IntentConfirmation) {
			// Defense-in-depth: no agent context available for approval,
			// so reject operations that require it.
			return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security: %s — %s (no agent context for approval)", toolName, secResult.Reasoning), nil)
		} else if agent != nil {
			// Check if we're running as a subagent — subagents cannot prompt
			isSubagent := agent.IsSubagent()

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
				if !mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, secResult.Risk.String(), secResult.Reasoning, extras) {
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", toolName, secResult.Reasoning), nil)
				}
				// Signal Gate 2 (persona cascade) that this command
				// already passed an interactive approval so it doesn't
				// re-prompt for the same execution (SP-058 follow-up).
				ctx = WithUserApproved(ctx)
			} else {
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
					ctx = WithUserApproved(ctx)
				} else if secResult.ShouldBlock {
					// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security block: %s — %s", toolName, secResult.Reasoning), nil)
				} else if secResult.IntentConfirmation {
					// NON-INTERACTIVE + intent confirmation required: must ask user first
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("confirmation required: %s — %s (this operation requires explicit user confirmation. Use ask_user to confirm with the user before proceeding.)", toolName, secResult.Reasoning), nil)
				} else if secResult.ShouldPrompt && !isSubagent {
					// NON-INTERACTIVE + CAUTION, needs prompt but no approval mechanism:
					// Return a special error that tells the LLM to re-assert safety before proceeding
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security caution: %s — %s (requires LLM verification: confirm this action is safe, expected, and aligned with user goals before proceeding)", toolName, secResult.Reasoning), nil)
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
		// TODO(SP-038): Agent has no Stdout/Writer accessor; it routes output
		// via PrintLine/PrintLineAsync → OutputRouter. For now, use os.Stdout
		// so tools that stream output still produce visible results.
		env.OutputWriter = os.Stdout
		env.MaxTokensFunc = func() int { return agent.GetMaxContextTokens() }
		env.ConfigManager = agent.GetConfigManager()
		env.AskUser = newAgentAskUserService(agent)
		// TODO(SP-038): Wire ApprovalManager adapter when tools are migrated
		// ApprovalManager: security.ApprovalManager does not implement
		// tools.ApprovalManager (different method signatures), pass nil
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
