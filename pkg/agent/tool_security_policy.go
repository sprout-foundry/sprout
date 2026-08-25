package agent

import (
	"fmt"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

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
