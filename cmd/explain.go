//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

// formatTypedError formats a typed error with category, message, and metadata
// for human-readable display. Returns the formatted string.
func formatTypedError(err error) string {
	if err == nil {
		return ""
	}

	// Check for TypedError first (SP-094 hierarchy)
	if te := agenterrors.AsTypedError(err); te != nil {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Error [%s] (%s): %s", te.Code, te.Severity, te.Message))
		if te.Component != "" {
			b.WriteString(fmt.Sprintf("\n  Component: %s", te.Component))
		}
		if len(te.Details) > 0 {
			b.WriteString("\n  Details:")
			for k, v := range te.Details {
				b.WriteString(fmt.Sprintf("\n    %s: %v", k, v))
			}
		}
		return b.String()
	}

	// Check for AgentError (legacy category-based)
	var agentErr *agenterrors.AgentError
	if errors.As(err, &agentErr) {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Category: %s\n", agentErr.Category))
		b.WriteString(fmt.Sprintf("Message: %s\n", agentErr.Message))
		if len(agentErr.Metadata) > 0 {
			b.WriteString("Metadata:\n")
			for k, v := range agentErr.Metadata {
				b.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
			}
		}
		if agentErr.Cause != nil {
			b.WriteString(fmt.Sprintf("Cause: %v\n", agentErr.Cause))
		}
		return strings.TrimSpace(b.String())
	}

	// Fallback: just the error message
	return err.Error()
}

// explainSource labels a single contributing check in the risk assessment.
type explainSource struct {
	id      string
	explain string
	level   configuration.RiskLevel
}

// explainSupportedTools is the allowlist of tool names that --tool accepts.
var explainSupportedTools = map[string]bool{
	"shell_command":         true,
	"write_file":            true,
	"edit_file":             true,
	"write_structured_file": true,
	"patch_structured_file": true,
	"git":                   true,
	"mkdir":                 true,
	"fetch_url":             true,
	"web_search":            true,
}

// ---------------------------------------------------------------------------
// explainCmd — `sprout explain '<command>'`
// ---------------------------------------------------------------------------

var explainCmd = &cobra.Command{
	Use:   "explain [flags] '<command>'",
	Short: "Show the security risk assessment for a command",
	Long: `Explain the security risk assessment for a command or tool call.

This diagnostic shows how Sprout classifies an operation on the canonical
Low/Medium/High/Critical risk scale, along with the reasoning and the
checks that contributed to the verdict.

By default the positional argument is treated as a shell command. Use
--tool to classify another tool (e.g. write_file with --path, git with
--operation).

It uses the static classifier only — no LLM, provider, or workspace context
is required. Runtime-gated checks (persona risk profile, workspace security
policy) are noted as context-dependent.

Examples:
  sprout explain 'rm -rf /'
  sprout explain 'git push'
  sprout explain 'ls -la'
  sprout explain --tool write_file --path ~/.ssh/config
  sprout explain --tool git --operation push
  sprout explain 'ls' --json`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName, _ := cmd.Flags().GetString("tool")
		pathFlag, _ := cmd.Flags().GetString("path")
		opFlag, _ := cmd.Flags().GetString("operation")
		asJSON, _ := cmd.Flags().GetBool("json")

		if !explainSupportedTools[toolName] {
			return usageErrorf(cmd, "unsupported --tool %q (valid: %s)", toolName, explainToolList())
		}

		cliArgs := buildExplainArgs(args, toolName, pathFlag, opFlag)

		if usage := validateExplainInput(toolName, cliArgs); usage != "" {
			return usageErrorWithHint(cmd, usage, "nothing to classify")
		}

		secResult := tools.ClassifyToolCall(toolName, cliArgs)
		level, hardBlock := combinedAssessment(toolName, secResult, cliArgs)
		if hardBlock {
			secResult.IsHardBlock = true
		}
		sources := explainSourcesFor(toolName, secResult, cliArgs)

		if asJSON {
			return printExplainJSON(cmd, secResult, level, sources, toolName, cliArgs)
		}

		printExplainHuman(cmd, secResult, level, sources, toolName, cliArgs)
		return nil
	},
}

// runExplain runs the risk assessment pipeline for a shell command and
// prints the human-readable breakdown. It is the testable core of the
// explain command.
func runExplain(cmd *cobra.Command, command string) error {
	args := map[string]interface{}{"command": command}
	secResult := tools.ClassifyToolCall("shell_command", args)
	level, hardBlock := combinedAssessment("shell_command", secResult, args)
	if hardBlock {
		secResult.IsHardBlock = true
	}
	sources := explainSourcesFor("shell_command", secResult, args)
	printExplainHuman(cmd, secResult, level, sources, "shell_command", args)
	return nil
}

// buildExplainArgs constructs the args map that ClassifyToolCall expects.
func buildExplainArgs(args []string, toolName, pathFlag, opFlag string) map[string]interface{} {
	out := map[string]interface{}{}
	if len(args) > 0 {
		out["command"] = strings.Join(args, " ")
	}
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if pathFlag != "" {
			out["path"] = pathFlag
		}
	case "git":
		if opFlag != "" {
			out["operation"] = opFlag
		}
	}
	return out
}

// validateExplainInput returns a non-empty user-facing message when the
// tool call is missing the input required to classify it.
func validateExplainInput(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "shell_command":
		if c, _ := args["command"].(string); strings.TrimSpace(c) == "" {
			return "Usage: sprout explain '<command>'   e.g. sprout explain 'rm -rf /tmp/foo'"
		}
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if p, _ := args["path"].(string); strings.TrimSpace(p) == "" {
			return fmt.Sprintf("Usage: sprout explain --tool %s --path <path>", toolName)
		}
	case "git":
		if op, _ := args["operation"].(string); strings.TrimSpace(op) == "" {
			return "Usage: sprout explain --tool git --operation <op>   e.g. --operation push"
		}
	}
	return ""
}

// explainSourcesFor derives the contributing-check list from a SecurityResult.
func explainSourcesFor(toolName string, res tools.SecurityResult, args map[string]interface{}) []explainSource {
	var sources []explainSource

	if res.IsHardBlock {
		sources = append(sources, explainSource{
			id:      "critical-op",
			explain: "built-in critical-operation hard-block",
			level:   configuration.RiskLevelCritical,
		})
	}

	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if shelltext.IsGitHistoryRewriteCommand(cmd) {
				if isGitRebaseCommand(cmd) {
					// AGENTS.md: rebase is unconditionally banned — every
					// form including interactive, --continue, --skip, and
					// `git pull --rebase`. The only permitted invocation is
					// pure `git rebase --abort` (recovery from a prior
					// session's interrupted rebase).
					sources = append(sources, explainSource{
						id:      "git-rebase",
						explain: "AGENTS.md: rebase is unconditionally banned — interactive, --continue, --skip, and `git pull --rebase` are all blocked. The only permitted invocation is `git rebase --abort` for recovery. Use `git merge` to integrate upstream.",
						level:   configuration.RiskLevelCritical,
					})
				} else {
					// Other history-rewrite ops: branch -D, tag -d, reset --hard <commit-ish>
					sources = append(sources, explainSource{
						id:      "git-history-rewrite",
						explain: "git history-rewrite — promptable; auto-approved when allow_git_history_rewrite=true",
						level:   configuration.RiskLevelHigh,
					})
				}
			}
		}
	}

	sources = append(sources, explainSource{
		id:      "classifier",
		explain: "static string-based classifier",
		level:   riskLevelFromSecurityResult(res),
	})

	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if isGitWriteCommand(cmd) {
				sources = append(sources, explainSource{
					id:      "git-write",
					explain: "git write op — gated by persona (requires agent runtime context)",
					level:   configuration.RiskLevelHigh,
				})
			}
			sources = append(sources, explainSource{
				id:      "persona-cascade",
				explain: "persona/profile risk cascade (requires agent runtime context)",
				level:   configuration.RiskLevelLow,
			})
		}
	}

	return sources
}

func init() {
	explainCmd.Flags().String("tool", "shell_command", "tool name to classify (shell_command, write_file, edit_file, write_structured_file, patch_structured_file, git, mkdir, fetch_url, web_search)")
	explainCmd.Flags().String("path", "", "file path (for write/edit tools)")
	explainCmd.Flags().String("operation", "", "git operation (for git tool)")
	explainCmd.Flags().Bool("json", false, "machine-readable JSON output")
	rootCmd.AddCommand(explainCmd)
}
