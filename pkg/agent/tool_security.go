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

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ExecuteTool executes a tool with standardized parameter validation and error handling
func ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent, rawArgsJSON string) ([]api.ImageData, string, error) {
	handler, found := tools.GetNewToolRegistry().Lookup(toolName)
	if !found {
		return nil, "", agenterrors.NewInvalidInputError("unknown tool '"+toolName+"'", nil)
	}

	if agent != nil && agent.debug {
		agent.debugLog("[tool] tool dispatched via new registry: %s\n", toolName)
	}

	// SP-063: computer-use tools are restricted to the computer_user persona.
	// Inert unless computer use is enabled (the restricted-name set is empty).
	if isComputerUseToolBlocked(toolName, agent) {
		return nil, "", agenterrors.NewPermission("tool "+toolName+" is only available to the computer_user persona", map[string]any{"tool": toolName})
	}

	// SP-063: per-session opt-in. Even after the persona-activation gates
	// pass, the FIRST computer-use action in a session must get explicit
	// user consent. Placed AFTER isComputerUseToolBlocked so that only
	// legitimate computer-use calls (correct persona + registered tool)
	// incur the potentially blocking consent dialog. Non-computer-use tools
	// and wrong-persona calls are already filtered above. Once approved,
	// the session flag makes this a fast no-op for the rest of the session.
	if agent != nil && computerUseToolNames[toolName] {
		if err := agent.checkComputerUseSessionOptIn(toolName); err != nil {
			return nil, "", err
		}
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

	// Security validation — classify and block/prompt dangerous operations.
	//
	// SP-068 Phase 2: when UnifiedRiskResolver is enabled, use the single
	// ResolveToolRisk assessment for gating — same path as the seed
	// pre-execute hook. This keeps the public ExecuteTool API consistent
	// with the live dispatch path. When disabled (flag OFF), the legacy
	// dual-gate block below runs unchanged.
	usedUnifiedGate := false
	if agent != nil {
		if cfg := agent.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
			if err := agent.unifiedSecurityGate(toolName, args); err != nil {
				return nil, "", err
			}
			usedUnifiedGate = true
		}
	}
	if !usedUnifiedGate {
		secResult := tools.ClassifyToolCall(toolName, args)
		if secResult.ShouldBlock || secResult.ShouldPrompt || secResult.IntentConfirmation {
			// Workflow-declared auto-approval for run_automate. When the
			// workflow JSON sets requires_approval: false, the model can
			// invoke it without a user prompt — designed for validation
			// workflows referenced from AGENTS.md that the model must run
			// before declaring work done. Failure to read the file falls
			// through to the normal approval path (fail-safe).
			workflowAutoApproved := false
			if agent != nil && toolName == "run_automate" && secResult.IntentConfirmation {
				if wf, ok := args["workflow"].(string); ok && wf != "" {
					if !workflowRequiresApproval(agent, wf) {
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
			} else if agent != nil {
				// SP-127 M1: staticGateAutoApprove now decides both bypass
				// AND path-tier, so file-touching tools benefit from the same
				// allowlist-aware allow that the filesystem gate adapter uses.
				filePath, mode := extractFilePathAndMode(toolName, args)
				if agent.staticGateAutoApprove(secResult, filePath, "", mode) && !secResult.IntentConfirmation {
					// Unsafe mode, session elevation, or path-tier allow —
					// skip the prompt for non-hard-block operations.
					// See staticGateAutoApprove.
					// IntentConfirmation is never auto-approved — it's about
					// explicit user intent, not risk bypass.
					if agent.debug {
						agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated/path-tier): bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
					}
				} else if agent.GetUnsafeShellMode() && toolName == "shell_command" && !secResult.IsHardBlock && secResult.Risk.String() != "DANGEROUS" && !secResult.IntentConfirmation {
					// --unsafe-shell bypasses CAUTION-tier shell prompts across all
					// modes (CLI and WebUI) so the flag behaves consistently regardless
					// of UI. DANGEROUS and hard-block operations still require approval.
					// IntentConfirmation is never auto-approved here either.
					if agent.debug {
						agent.debugLog("[UNLOCK] Unsafe shell mode: bypassing shell security prompt for %s (risk: %s)\n", toolName, secResult.Risk)
					}
				}
			} else if secResult.ShouldBlock || secResult.IntentConfirmation {
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
					// WEBUI: request approval via event bus for the browser dialog.
					//
					// Suspend the CLI spinner and pause the steer reader BEFORE
					// blocking on the webui response. The approval request can
					// block for up to 30 minutes (DefaultTimeout). Without
					// suspending, the steer reader stays in raw mode while the
					// agent is blocked, and any stdout writes from background
					// goroutines (tool output, streaming) corrupt the terminal
					// display. After the block returns (approved, denied, or
					// timed out), ResumeSteer restores the terminal state.
					clihooks.SuspendIndicator()
					clihooks.PauseSteer()
					defer clihooks.ResumeSteer()

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
							return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, secResult.Reasoning), nil)
						}
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
							return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, secResult.Reasoning), nil)
						}
						if toolName == "run_automate" {
							if wf, ok := args["workflow"].(string); ok {
								agent.MarkWorkflowApprovedInSession(wf)
							}
						}
					} else if secResult.ShouldBlock {
						// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", toolName, secResult.Reasoning), nil)
					} else if secResult.IntentConfirmation {
						// NON-INTERACTIVE + intent confirmation required: must ask user first
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm.", toolName, secResult.Reasoning), nil)
					} else if secResult.ShouldPrompt && !isSubagent {
						// NON-INTERACTIVE + CAUTION, needs prompt but no approval mechanism:
						// Return a terminal SecurityError — the operation cannot proceed
						// without interactive approval. LLMs reliably honor "do not retry."
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf(
							"security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm. Do not retry this exact command without changing the risk profile.",
							toolName, secResult.Reasoning), nil)
					} // NON-INTERACTIVE + CAUTION, no approval mechanism, not a subagent: auto-allow (safe operations)
				}
			}
		}
	} // end if !usedUnifiedGate

	// Build ToolEnv from agent context
	var env tools.ToolEnv
	if agent != nil {
		env.EventBus = agent.GetEventBus()
		// Use effectiveCwd so tools honor cd commands during a session.
		env.WorkspaceRoot = agent.effectiveCwd()
		// SP-074-2: Route tool output through the agent's output system
		// (PrintLineAsync → OutputRouter) instead of os.Stdout.
		// Gate on verbose mode: in default/compact, tool output is
		// suppressed — the user doesn't need to see raw read_file
		// contents or full shell stdout dumped to their terminal.
		if cfg := agent.GetConfig(); cfg != nil && cfg.OutputVerbosity == configuration.OutputVerbosityVerbose {
			env.OutputWriter = newOutputRouter(agent)
		}
		env.MaxTokensFunc = func() int { return agent.GetMaxContextTokens() }
		env.ConfigManager = agent.GetConfigManager()
		env.AskUser = newAgentAskUserService(agent)
		env.TodoManager = agent.GetTodoManager()
		// Interactive CLI means: no browser client connected AND stdin is a TTY.
		env.IsInteractiveCLI = !agent.HasActiveWebUIClients() && !isNonInteractive()
		// SP-074-3: Wire ApprovalManager adapter so migrated tools can
		// request security approvals through the normal CLI/WebUI flow.
		env.ApprovalManager = newToolsApprovalAdapter(agent)
		// SP-079-1: Wire new ToolEnv fields for vision, embedding, and
		// the remaining subsystem interfaces.
		env.EmbeddingMgr = agent.GetEmbeddingManager()
		env.VisionProcessor = agent.GetVisionProcessor()
		env.WebBrowser = tools.NewBrowserAdapter()
		env.SkillLoader = newSkillLoaderAdapter(agent)
		env.SearchEngine = newSearchEngineAdapter(agent)
		// SP-082-1: Pass the raw JSON args so handlers can recover key
		// insertion order from the LLM's original tool call.
		env.RawArgsJSON = rawArgsJSON
		env.Notifier = agent
		// Propagate subagent depth for memory gate and other subagent-specific behaviors.
		env.SubagentDepth = agent.subagentDepth
		// Propagate Gate 1's auto-approve decision so handler-level gates
		// (Gate 2) skip their interactive prompt, matching Gate 1. Covers
		// both --unsafe mode and elevated risk profiles; hard blocks are
		// still enforced by the handlers' own IsHardBlock early-returns.
		env.Gate1AutoApproved = agent.GetUnsafeMode() || agent.IsSessionElevated()
		// Wire the filesystem approval gate so file-touching handlers
		// surface the approve / session-allow / elevate dialog on
		// off-workspace paths instead of returning the raw
		// ErrOutsideWorkingDirectory sentinel. See
		// newFilesystemGateAdapter and handleFileSecurityError.
		env.FilesystemGate = newFilesystemGateAdapter(agent)
	} else {
		env.OutputWriter = os.Stdout
		env.MaxTokensFunc = func() int { return 0 }
	}

	if err := handler.Validate(args); err != nil {
		return nil, "", agenterrors.Wrapf(err, "validation failed for tool %q", toolName)
	}

	// SP-104-3: Memory gate for memory-intensive subagent shell commands.
	// Only gate commands that are likely to consume significant memory
	// (test runners, bundlers, compilers) to avoid blocking trivial
	// commands like ls, cat, echo with 30-second sleeps.
	if toolName == "shell_command" && agent.subagentDepth > 0 {
		if cmd, ok := args["command"].(string); ok && IsMemoryIntensiveCommand(cmd) {
			gate := DefaultMemoryGate()
			if err := gate.Check(); err != nil {
				return nil, "", agenterrors.NewPermission(
					fmt.Sprintf("memory gate blocked shell_command: %v", err), nil)
			}
		}
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
				Base64: img.Base64,
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
		return images, "", agenterrors.NewTool(toolName, errMsg, nil)
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
// Both Gate-1 entry points call this — ExecuteTool and the
// live seed pre-execute hook (newPreExecuteHook) — so the bypass policy
// lives in exactly one place and the two paths can't drift.
// fileTouchingTools is the set of tool names that carry a "path" argument.
// Used by extractFilePathAndMode to determine whether to supply path context
// to staticGateAutoApprove.
var fileTouchingTools = map[string]bool{
	"read_file":             true,
	"write_file":            true,
	"edit_file":             true,
	"write_structured_file": true,
	"patch_structured_file": true,
	"list_directory":        true,
}

// extractFilePathAndMode returns the file path and access mode for a tool call,
// or ("", "") for non-file tools. Path is the "path" argument value; mode is
// "write" for write/edit tools, "read" for read tools. Non-file tools and tools
// that don't supply a "path" arg return the zero values so the classifier skips
// the path-tier branch.
//
// Path resolution convention for callers:
//   - filePath is the user-supplied path (may be relative or absolute).
//   - resolvedPath is the symlink-evaluated canonical target (empty if the path
//     does not exist or the caller did not perform resolution).
//   - When resolvedPath is non-empty, classifyFileAccess uses it for workspace
//     containment and sensitive-path checks, falling back to filePath if the
//     resolved target does not exist.
//   - When resolvedPath is empty, the function uses filePath directly for the
//     prefix checks — relative paths that don't exist are evaluated lexically.
func extractFilePathAndMode(toolName string, args map[string]interface{}) (filePath, mode string) {
	if !fileTouchingTools[toolName] {
		return "", ""
	}
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", ""
	}
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return path, "write"
	default:
		return path, "read"
	}
}

// FileAccessDecision describes the resolved verdict for a file-path operation
// from Gate 1's path-tier classifier.
type FileAccessDecision int

const (
	// FileAccessAllow: path is in an allowlisted location (workspace root,
	// session-allowlisted folder, or /tmp).
	FileAccessAllow FileAccessDecision = iota
	// FileAccessPrompt: path is outside the allowlist and not hard-blocked;
	// user must approve.
	FileAccessPrompt
	// FileAccessDeny: path targets a known hard-block location or violates
	// a declared read_only constraint.
	FileAccessDeny
)

// classifyFileAccess inspects the path tier and returns the access verdict.
// Used by both Gate 1 (staticGateAutoApprove) and the filesystem gate adapter
// so they always agree on allow/prompt/deny for a given path.
//
// Inputs:
//   - filePath: the user-supplied path (may be relative, may not exist)
//   - resolvedPath: the symlink-evaluated canonical form (may equal filePath)
//   - mode: "read" or "write" (controls which allowlists apply)
//
// Returns:
//   - FileAccessAllow when the path lands in workspace root, session-allowlisted
//     folder, /tmp, or another gate-bypass-visible location.
//   - FileAccessPrompt when the path is outside the allowlist and not hard-blocked.
//   - FileAccessDeny when the path targets a known hard-block location or when
//     a write is attempted against a read_only declared allowlist entry.
func (a *Agent) classifyFileAccess(filePath, resolvedPath, mode string) FileAccessDecision {
	if a == nil {
		return FileAccessPrompt
	}
	// Use resolvedPath when available; fall back to filePath for lexical checks.
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	// /tmp is universally allowed regardless of mode.
	if filesystem.IsUnderTmpPath(target) {
		return FileAccessAllow
	}
	// Workspace root and subdirectories are always allowed.
	if a.IsUnderWorkspaceRoot(target) {
		return FileAccessAllow
	}
	// Session-allowlisted folders (workflow-declared allowed_paths + user
	// mid-session approvals) are allowed subject to their declared mode.
	if a.IsFolderSessionAllowed(target) {
		if mode == "write" && a.IsReadOnlyAllowedFolder(target) {
			return FileAccessDeny
		}
		return FileAccessAllow
	}
	// Sensitive system paths always prompt rather than hard-deny.
	// The user must confirm access explicitly.
	if filesystem.IsSensitiveSystemPath(target) {
		return FileAccessPrompt
	}
	return FileAccessPrompt
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
//   - Path-tier allow: the resolved path lands in workspace root,
//     /tmp, or a session-allowlisted folder (write attempts against
//     read_only entries are still denied so the classifier has a way
//     to surface the violation).
//
// Hard blocks (critical system operations such as rm -rf /) are never
// auto-approved here — they fall through to the caller's block/prompt
// handling regardless of elevation.
//
// filePath, resolvedPath, and mode narrow the check to file-touching
// tools. Pass "", "", "" for non-file tools; the path-tier branch is
// skipped in that case.
//
// Both Gate-1 entry points call this — ExecuteTool and the
// live seed pre-execute hook (newPreExecuteHook) — so the bypass policy
// lives in exactly one place and the two paths can't drift.
func (a *Agent) staticGateAutoApprove(secResult tools.SecurityResult, filePath, resolvedPath, mode string) bool {
	if a == nil {
		return false
	}
	if a.GetUnsafeMode() {
		return true
	}
	if a.IsSessionElevated() && !secResult.IsHardBlock {
		return true
	}
	// SP-127 M1: path-tier allow. When a file path is supplied, consult
	// the same classifier the filesystem gate adapter uses so Gate 1 and
	// Gate 2 agree on the verdict. FileAccessAllow skips the prompt;
	// FileAccessDeny propagates the hard block so the caller rejects the
	// op; FileAccessPrompt falls through to the existing prompt flow.
	if filePath != "" {
		switch a.classifyFileAccess(filePath, resolvedPath, mode) {
		case FileAccessAllow:
			return true
		case FileAccessDeny:
			return false
		case FileAccessPrompt:
			// fall through to default false
		}
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
		a.logSecurityDecision(name, args, assessment, "blocked")
		return agenterrors.NewSecurityErrorWithAssessment(
			fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", name, assessment.Reason), assessment.Explain(), nil,
		)
	}

	// High risk: reuse the existing approval cascade (EA persona reasons,
	// interactive users get prompted, non-interactive subagents are blocked)
	if assessment.Level == configuration.RiskLevelHigh {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if !a.highRiskApprovedForCommand(nil, cmd) {
				a.logSecurityDecision(name, args, assessment, "blocked")
				return agenterrors.NewSecurityErrorWithAssessment(
					fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", name, assessment.Reason), assessment.Explain(), nil,
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
			if !workflowRequiresApproval(a, wf) {
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
			a.logSecurityDecision(name, args, assessment, "blocked")
			return agenterrors.NewSecurityErrorWithAssessment(
				fmt.Sprintf("security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm.", name, assessment.Reason), assessment.Explain(), nil,
			)
		}
		// For intent confirmation, go through the approval prompt
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Medium risk: needs interactive approval
	if assessment.Level == configuration.RiskLevelMedium {
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Low risk: allow. Skip audit logging here — Low-risk allows are noisy
	// (the vast majority of tool calls) and provide little audit value.
	return nil
}

// unifiedSecurityPrompt handles the interactive approval flow for Medium
// risk or intent-confirmation operations in the unified gate. Delegates to
// RequestApproval which owns surface selection, fallback, and 4-option
// outcome (SP-068 Phase 3).
func (a *Agent) unifiedSecurityPrompt(name string, args map[string]interface{}, assessment RiskAssessment) error {
	_, err := a.RequestApproval(assessment, name, args)
	return err
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

// handleFileSecurityError checks if an error is due to filesystem security and prompts the user.
// Returns a context with security bypass enabled if user approves, original context otherwise.
//
// `resolvedPath` is the canonical target after symlink resolution (the
// actual filesystem object that would be touched). When non-empty, it
// is shown alongside the user-supplied `filePath` in the approval
// dialog so the user can verify the destination is what they expect.
// Pass "" when the caller cannot compute it; display falls back to
// `filePath` alone.
func handleFileSecurityError(ctx context.Context, agent *Agent, toolName, filePath, resolvedPath string, err error) (context.Context, bool) {
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
		// SP-128-1f: read_only declared paths still satisfy
		// IsFolderSessionAllowed (so reads continue to work), but
		// a write tool must NOT be allowed under a read_only grant.
		// We detect "write" via the error sentinel — every write
		// tool surfaces ErrWriteOutsideWorkingDirectory; read tools
		// surface ErrOutsideWorkingDirectory. This is the same
		// signal the rest of the function uses (see the first
		// errors.Is check at the top of this function), so the
		// classification stays consistent. When the path is on the
		// allowlist but the mode says read_only, return
		// (ctx, false) AND attach a denial-reason on the context
		// so the caller (withFilesystemApproval) returns the
		// workflow-specific message instead of the generic
		// off-workspace sentinel. The wording matches the spec:
		// "write blocked: <path> is declared read_only in the
		// active workflow's allowed_paths; the filesystem gate
		// cannot authorize a write under a read_only grant".
		if errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) && !agent.IsFolderSessionWriteAllowed(filePath) {
			agent.debugLog("[APPROVAL] write blocked: %s is declared read_only in the active workflow's allowed_paths; filesystem gate refuses to authorize write\n", filePath)
			// The gate ctx carries the workflow-specific reason; the
			// original filesystem sentinel still wraps via %w inside
			// withFilesystemApproval (see filesystem_gate.go around
			// the denial-reason branch) so errors.Is and the
			// subagent stderr parser (which scans for "outside
			// working directory") keep working.
			reason := fmt.Sprintf("write blocked: %s is declared read_only in the active workflow's allowed_paths; the filesystem gate cannot authorize a write under a read_only grant", filePath)
			return tools.WithFilesystemGateDenialReason(ctx, reason), false
		}
		agent.debugLog("[UNLOCK] Folder is on session allowlist: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Classify the path so we can pick the right dialog mode and
	// scope. Sensitive (system dirs, off-CWD home) gets 2 options;
	// External gets 3 options including "Allow folder this session".
	tier := ClassifyPathAccess(filePath, agent.GetWorkspaceRoot(), detectHomeDir(), agent.effectiveCwd())
	folder := filepath.Dir(filePath)

	// Display target = the user-typed path, with the canonical target
	// appended when it diverges (i.e. when filePath is a symlink).
	// Without this, a workspace symlink to /etc/passwd would prompt
	// "Allow access to workspace/link?" and approval would silently
	// widen access to /etc/passwd.
	displayPath := filePath
	if resolvedPath != "" && resolvedPath != filePath {
		displayPath = filePath + "\n   (resolves to: " + resolvedPath + ")"
	}

	// Subagents cannot prompt — return unapproved so the error propagates
	if agent.IsSubagent() {
		agent.debugLog("Subagent encountered filesystem security error for %s, delegating to primary agent\n", filePath)
		return ctx, false
	}

	// Prefer webui approval path when a browser tab is connected.
	if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && agent.HasActiveWebUIClients() {
		// Suspend CLI spinner and steer reader before blocking on the
		// webui response — same rationale as the tool approval path above.
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		defer clihooks.ResumeSteer()

		kind := "fs_external"
		if tier == PathTierSensitive {
			kind = "fs_sensitive"
		}
		prompt := fmt.Sprintf("The tool '%s' is attempting to access a file outside the working directory.", toolName)
		extras := map[string]string{
			"risk_type": "Filesystem Security",
			"target":    displayPath,
			"path":      displayPath,
			"kind":      kind,
		}
		if resolvedPath != "" && resolvedPath != filePath {
			extras["resolved_path"] = resolvedPath
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
		choice := logger.AskForFilesystemApproval(prompt, displayPath, folder, promptTier)
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

// filesystemGateAdapter implements tools.FilesystemGate by delegating to
// the agent's existing handleFileSecurityError function. It is the
// bridge that brings the file-touching tool handlers (write_file,
// edit_file, read_file, list_directory, write_structured_file, …) onto
// the same approve / session-allow / elevate flow the legacy
// tool_handlers_file.go path already used. Before this adapter
// existed, those handlers hard-errored on off-workspace paths because
// the live seed dispatch path never reached handleFileSecurityError.
type filesystemGateAdapter struct {
	agent *Agent
}

// newFilesystemGateAdapter returns a tools.FilesystemGate backed by the
// agent's filesystem approval flow. Returns nil when agent is nil so
// callers can safely pass the result into ToolEnv without nil checks.
func newFilesystemGateAdapter(agent *Agent) tools.FilesystemGate {
	if agent == nil {
		return nil
	}
	return &filesystemGateAdapter{agent: agent}
}

// RequestPathApproval implements tools.FilesystemGate. It defers
// entirely to handleFileSecurityError, which already handles both
// surfaces (WebUI event-bus dialog when a browser is connected,
// CLI picker otherwise) and persists session-scoped decisions
// (folder allowlist, elevation).
//
// `resolvedPath` is the canonical target after symlink resolution
// (the actual filesystem object that would be touched). When
// non-empty, it is shown alongside the user-supplied `filePath` in
// the approval dialog so the user can verify the destination is
// what they expect — a symlink `workspace/link` pointing to
// `/etc/passwd` would otherwise be approved without the user
// noticing the resolved target.
//
// SP-127 M1: this method now delegates to the Gate 1 path-tier
// classifier so the filesystem gate and Gate 1 always agree on the
// allow/prompt/deny verdict. withFilesystemApproval stays as a
// fallback wrapper around this same adapter, so the two surfaces
// cannot diverge.
func (a *filesystemGateAdapter) RequestPathApproval(ctx context.Context, toolName, filePath, resolvedPath string, err error) (context.Context, bool) {
	if a == nil || a.agent == nil {
		return ctx, false
	}

	// Determine access mode from the error sentinel.
	// Every write tool surfaces ErrWriteOutsideWorkingDirectory;
	// read tools surface ErrOutsideWorkingDirectory.
	mode := "read"
	if errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		mode = "write"
	}

	// SP-127 M1: Gate 1 and Gate 2 consult the same classifier.
	// FileAccessAllow skips the prompt entirely.
	// FileAccessDeny surfaces the read_only violation and rejects.
	// FileAccessPrompt falls through to the interactive prompt flow.
	decision := a.agent.classifyFileAccess(filePath, resolvedPath, mode)
	switch decision {
	case FileAccessAllow:
		return filesystem.WithSecurityBypass(ctx), true
	case FileAccessDeny:
		// read_only violation: attach a denial reason and return false
		// so the original error propagates with a workflow-specific message.
		reason := fmt.Sprintf("write blocked: %s is declared read_only in the active workflow's allowed_paths; the filesystem gate cannot authorize a write under a read_only grant", filePath)
		return tools.WithFilesystemGateDenialReason(ctx, reason), false
	case FileAccessPrompt:
		// fall through to the interactive prompt flow
	}

	return handleFileSecurityError(ctx, a.agent, toolName, filePath, resolvedPath, err)
}
