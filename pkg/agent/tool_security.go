package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
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
	if secResult := tools.ClassifyToolCall(toolName, args); secResult.ShouldBlock || secResult.ShouldPrompt {
		if agent != nil && agent.GetUnsafeMode() {
			// Unsafe mode: bypass all security checks
			if agent.debug {
				agent.debugLog("[UNLOCK] Unsafe mode: bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
			}
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
				}
				if !mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, secResult.Risk.String(), secResult.Reasoning, extras) {
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", toolName, secResult.Reasoning), nil)
				}
			} else {
				// CLI: prompt user interactively via terminal stdin
				agentConfig := agent.GetConfig()
				logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
				canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

				if canPrompt {
					prompt := buildSecurityPrompt(toolName, args, secResult)
					if !logger.AskForConfirmation(prompt, false, false) {
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", toolName, secResult.Reasoning), nil)
					}
				} else if secResult.ShouldBlock {
					// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block
					return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security block: %s — %s", toolName, secResult.Reasoning), nil)
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
			}
		}
	}

	return images, output, nil
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
	sb.WriteString("Do you want to proceed? (yes/no): ")

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

	// If user already approved filesystem access this session, skip re-prompting
	if agent.IsSecurityBypassApproved() {
		agent.debugLog("[UNLOCK] Session-level security bypass: allowing file access outside working directory: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Subagents cannot prompt — return unapproved so the error propagates
	if agent.IsSubagent() {
		agent.debugLog("Subagent encountered filesystem security error for %s, delegating to primary agent\n", filePath)
		return ctx, false
	}

	// Prefer webui approval path when a browser tab is connected.
	// Same pattern as the pre-execution security classification above.
	if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && agent.HasActiveWebUIClients() {
		// WEBUI: request approval via event bus for the browser dialog
		prompt := fmt.Sprintf("The tool '%s' is attempting to access a file outside the working directory: %s", toolName, filePath)
		extras := map[string]string{
			"risk_type": "Filesystem Security",
			"target":    filePath,
		}
		if mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, "CAUTION", prompt, extras) {
			agent.debugLog("[APPROVAL] User approved file access outside working directory: %s\n", filePath)
			agent.SetSecurityBypassApproved()
			return filesystem.WithSecurityBypass(ctx), true
		}
		agent.debugLog("[APPROVAL] User rejected file access outside working directory: %s\n", filePath)
		return ctx, false
	}

	// CLI: prompt user interactively via terminal stdin
	agentConfig := agent.GetConfig()
	logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
	canPrompt := logger != nil && logger.IsInteractive()

	if canPrompt {
		prompt := fmt.Sprintf("[WARN] Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory:\n  %s\n\nDo you want to allow this? (yes/no): ", toolName, filePath)
		if logger.AskForConfirmation(prompt, false, false) {
			agent.debugLog("[APPROVAL] User approved file access outside working directory: %s\n", filePath)
			agent.SetSecurityBypassApproved()
			return filesystem.WithSecurityBypass(ctx), true
		}
		agent.debugLog("[APPROVAL] User rejected file access outside working directory: %s\n", filePath)
		return ctx, false
	}

	// No prompting available — return unapproved
	if agent.debug {
		agent.debugLog("Cannot prompt for filesystem security approval (no mechanism): %s\n", filePath)
	}
	return ctx, false
}
