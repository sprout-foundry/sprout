//go:build !js

// Package cmd provides the `sprout explain` subcommand (SP-068 Phase 3) for
// human-readable risk assessment of commands and tool calls.
package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Git history-rewrite detection (replicated from pkg/agent/tool_handlers.go)
// ---------------------------------------------------------------------------

// stripQuotedContent replaces all single-quoted and double-quoted string
// content with spaces, preserving quote boundaries so token positions stay
// stable.
func stripQuotedContent(s string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			b.WriteByte(ch)
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
		} else if inSingle || inDouble {
			if ch == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// isGitHistoryRewriteCommand checks whether `command` contains a git
// invocation that can lose commit history. Replicated from pkg/agent to
// avoid a circular import.
func isGitHistoryRewriteCommand(command string) bool {
	command = stripQuotedContent(command)
	remaining := command
	for {
		idx := strings.Index(remaining, "git ")
		if idx == -1 {
			return false
		}
		gitCmd := remaining[idx:]
		parts := strings.Fields(gitCmd)
		if len(parts) < 2 {
			remaining = remaining[idx+1:]
			continue
		}
		subcommand := ""
		subIdx := 0
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			subIdx = i
			break
		}
		if subcommand == "" {
			remaining = remaining[idx+1:]
			continue
		}
		rest := parts[subIdx+1:]
		switch subcommand {
		case "rebase":
			// `git rebase --abort` is a recovery op, not a history rewrite.
			// Any other rebase form (including `--abort` with other flags or
			// arguments, or any other rebase variant) is treated as a rewrite.
			// The only permitted rebase invocation is pure `--abort`.
			if len(rest) == 1 && rest[0] == "--abort" {
				return false
			}
			return true
		case "reset":
			hard := false
			for _, a := range rest {
				if a == "--hard" {
					hard = true
				}
			}
			if !hard {
				remaining = remaining[idx+1:]
				continue
			}
			hasCommitIsh := false
			for _, a := range rest {
				if a == "--hard" || strings.HasPrefix(a, "-") || a == "HEAD" {
					continue
				}
				hasCommitIsh = true
				break
			}
			if hasCommitIsh {
				return true
			}
		case "branch":
			for _, a := range rest {
				if a == "-d" || a == "-D" || a == "--delete" {
					return true
				}
			}
		case "tag":
			for _, a := range rest {
				if a == "-d" || a == "--delete" {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// isGitWriteCommand reports whether `command` contains a git invocation
// whose intent requires the orchestrator git-write flow. Replicated from
// pkg/agent to avoid a circular import.
func isGitWriteCommand(command string) bool {
	command = stripQuotedContent(command)
	remaining := command
	for {
		idx := strings.Index(remaining, "git ")
		if idx == -1 {
			return false
		}
		gitCmd := remaining[idx:]
		parts := strings.Fields(gitCmd)
		if len(parts) < 2 {
			remaining = remaining[idx+1:]
			continue
		}
		subcommand := ""
		subcommandIdx := 2
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			subcommandIdx = i
			break
		}
		if subcommand != "" {
			subcommand = strings.TrimPrefix(subcommand, "--")
			subcommand = strings.TrimPrefix(subcommand, "-")
			rest := parts[subcommandIdx+1:]
			switch subcommand {
			case "branch":
				hasDelete := false
				for _, arg := range rest {
					if arg == "-d" || arg == "-D" || arg == "--delete" {
						hasDelete = true
						break
					}
				}
				if hasDelete {
					remaining = remaining[idx+1:]
					continue
				}
				createFlags := map[string]struct{}{
					"-m": {}, "-M": {}, "--move": {},
					"-c": {}, "-C": {}, "--copy": {}, "-f": {}, "--force": {},
					"-u": {}, "--set-upstream-to": {}, "--unset-upstream": {}, "--edit-description": {},
				}
				for _, arg := range rest {
					if _, ok := createFlags[arg]; ok {
						return true
					}
					if !strings.HasPrefix(arg, "-") {
						return true
					}
				}
				remaining = remaining[idx+1:]
				continue
			case "tag":
				hasDelete := false
				for _, arg := range rest {
					if arg == "-d" || arg == "--delete" {
						hasDelete = true
						break
					}
				}
				if hasDelete {
					remaining = remaining[idx+1:]
					continue
				}
				createFlags := map[string]struct{}{
					"-a": {}, "-s": {}, "-u": {}, "-f": {}, "--force": {},
				}
				for _, arg := range rest {
					if _, ok := createFlags[arg]; ok {
						return true
					}
					if !strings.HasPrefix(arg, "-") {
						return true
					}
				}
				remaining = remaining[idx+1:]
				continue
			}
			intentGated := []string{"commit", "push", "merge", "clone", "init", "worktree"}
			for _, writeCmd := range intentGated {
				if subcommand == writeCmd {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// isGitRebaseCommand reports whether `command` contains a `git rebase`
// invocation that rewrites history (i.e. NOT `git rebase --abort`).
// AGENTS.md bans rebase unconditionally; the only permitted rebase is
// `--abort` (recovery from a prior session's interrupted rebase).
func isGitRebaseCommand(command string) bool {
	command = stripQuotedContent(command)
	remaining := command
	for {
		idx := strings.Index(remaining, "git ")
		if idx == -1 {
			return false
		}
		gitCmd := remaining[idx:]
		parts := strings.Fields(gitCmd)
		if len(parts) < 2 {
			remaining = remaining[idx+1:]
			continue
		}
		subcommand := ""
		subIdx := 0
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			subIdx = i
			break
		}
		if subcommand == "rebase" {
			rest := parts[subIdx+1:]
			// Pure `git rebase --abort` is the only permitted rebase
			// invocation (recovery from a prior session's interrupted
			// rebase). Any additional token — even something as benign
			// looking as `--no-verify` — makes the abort intent ambiguous
			// and is treated as a rewrite attempt.
			if len(rest) == 1 && rest[0] == "--abort" {
				return false
			}
			return true
		}
		if subcommand == "pull" {
			// AGENTS.md also bans `git pull --rebase` (and `-r`).
			// Use whole-token matching so `--no-rebase` and
			// `--recurse-submodules -r` don't false-positive.
			// `--rebase-preserve` is a real git flag (rebases + preserves
			// locally committed merges) — also a rebase, also banned.
			for _, a := range parts[subIdx+1:] {
				if a == "--rebase" || a == "-r" || a == "--rebase-preserve" {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// ---------------------------------------------------------------------------
// Contributing-check model
// ---------------------------------------------------------------------------

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
			return fmt.Errorf("unknown or unsupported tool %q (valid: %s)", toolName, explainToolList())
		}

		cliArgs := buildExplainArgs(args, toolName, pathFlag, opFlag)

		if msg := validateExplainInput(toolName, cliArgs); msg != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), msg)
			return fmt.Errorf("no input provided for tool %q", toolName)
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
			return `Usage: sprout explain '<command>'
Provide a command string to classify (e.g. sprout explain 'rm -rf /tmp/foo').`
		}
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if p, _ := args["path"].(string); strings.TrimSpace(p) == "" {
			return fmt.Sprintf(`Usage: sprout explain --tool %s --path <path>
Provide a --path to classify (e.g. sprout explain --tool write_file --path ./foo.txt).`, toolName)
		}
	case "git":
		if op, _ := args["operation"].(string); strings.TrimSpace(op) == "" {
			return `Usage: sprout explain --tool git --operation <op>
Provide a --operation to classify (e.g. sprout explain --tool git --operation push).`
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
			if isGitHistoryRewriteCommand(cmd) {
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

// riskLevelFromSecurityResult converts a tools.SecurityResult risk tier into
// a configuration.RiskLevel. Replaces the deleted tools.RiskLevelFromSecurityResult.
// Mapping: hard-block → critical, safe → low, caution → medium, dangerous → high.
// Unknown risk values default to low (matching the prior behavior).
func riskLevelFromSecurityResult(res tools.SecurityResult) configuration.RiskLevel {
	if res.IsHardBlock {
		return configuration.RiskLevelCritical
	}
	switch res.Risk {
	case tools.SecuritySafe:
		return configuration.RiskLevelLow
	case tools.SecurityCaution:
		return configuration.RiskLevelMedium
	case tools.SecurityDangerous:
		return configuration.RiskLevelHigh
	default:
		return configuration.RiskLevelLow
	}
}

// combinedAssessment folds the git-history-rewrite gate into the classifier
// verdict to produce the final level and hard-block status.
func combinedAssessment(toolName string, secResult tools.SecurityResult, args map[string]interface{}) (configuration.RiskLevel, bool) {
	level := riskLevelFromSecurityResult(secResult)
	hardBlock := secResult.IsHardBlock

	if toolName == "shell_command" {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if isGitHistoryRewriteCommand(cmd) {
				if isGitRebaseCommand(cmd) {
					// AGENTS.md: rebase is unconditionally banned — hard-block.
					level = configuration.RiskLevelCritical
					hardBlock = true
				} else {
					// Other history-rewrite ops: branch -D, tag -d, reset --hard <commit-ish>
					level = configuration.RiskLevelHigh
				}
			}
		}
	}
	return level, hardBlock
}

// levelHeadline renders the top-line summary of the assessment.
func levelHeadline(level configuration.RiskLevel, res tools.SecurityResult) string {
	intent := res.IntentConfirmation
	switch level {
	case configuration.RiskLevelCritical:
		return "CRITICAL — hard-block (cannot be approved)"
	case configuration.RiskLevelHigh:
		if intent {
			return "HIGH — requires explicit confirmation before proceeding"
		}
		return "HIGH — prompts when interactive; blocked when non-interactive"
	case configuration.RiskLevelMedium:
		if intent {
			return "MEDIUM — requires explicit confirmation before proceeding"
		}
		return "MEDIUM — prompts when interactive; auto-approved risk-profile dependent"
	default:
		if intent {
			return "LOW — requires explicit confirmation before proceeding"
		}
		return "LOW — auto-approved (no prompt)"
	}
}

// suppressionHints returns guidance on how to bypass a non-critical prompt.
// Only shown for Medium and High — Low commands are auto-approved and
// Critical cannot be overridden.
func suppressionHints(level configuration.RiskLevel, toolName string) []string {
	if level == configuration.RiskLevelCritical || level == configuration.RiskLevelLow {
		return nil
	}
	var hints []string
	if toolName == "shell_command" {
		hints = append(hints, "--unsafe-shell          (shell commands only)")
	}
	hints = append(hints, "--risk-profile=permissive  (all non-critical operations)")
	hints = append(hints, "Re-run interactively to approve via the webui/CLI dialog")
	return hints
}

// printExplainHuman writes the human-readable assessment to stdout.
func printExplainHuman(cmd *cobra.Command, res tools.SecurityResult, level configuration.RiskLevel, sources []explainSource, toolName string, args map[string]interface{}) {
	out := cmd.OutOrStdout()

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", levelHeadline(level, res))
	fmt.Fprintln(out)

	if c, _ := args["command"].(string); c != "" {
		fmt.Fprintf(out, "  Command:  %s\n", c)
	}
	if p, _ := args["path"].(string); p != "" {
		fmt.Fprintf(out, "  Path:     %s\n", p)
	}
	if op, _ := args["operation"].(string); op != "" {
		fmt.Fprintf(out, "  Operation: %s\n", op)
	}
	fmt.Fprintf(out, "  Tool:     %s\n", toolName)
	fmt.Fprintln(out)

	reason := strings.TrimSpace(res.Reasoning)
	if reason == "" {
		reason = "(no reasoning provided)"
	}
	fmt.Fprintf(out, "  Reason: %s\n", reason)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Contributing checks:")
	for _, s := range sources {
		fmt.Fprintf(out, "    \u2022 %-19s \u2014 %s\n", s.id, s.explain)
	}

	if level == configuration.RiskLevelCritical {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  This operation will be unconditionally blocked. No risk profile,")
		fmt.Fprintln(out, "  flag, or approval can override it.")
	}

	if hints := suppressionHints(level, toolName); len(hints) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  To suppress this prompt:")
		for _, h := range hints {
			fmt.Fprintf(out, "    \u2022 %s\n", h)
		}
	}
	fmt.Fprintln(out)
}

// explainJSONOutput is the structure emitted with --json.
type explainJSONOutput struct {
	Tool      string                 `json:"tool"`
	Args      map[string]interface{} `json:"args"`
	RiskLevel string                 `json:"risk_level"`
	HardBlock bool                   `json:"hard_block"`
	Sources   []explainJSONSource    `json:"sources"`
	Result    tools.SecurityResult   `json:"result"`
}

// explainJSONSource is a single contributing check in JSON output.
type explainJSONSource struct {
	ID      string `json:"id"`
	Explain string `json:"explain"`
	Level   string `json:"level"`
}

// printExplainJSON writes a machine-readable assessment to stdout.
func printExplainJSON(cmd *cobra.Command, res tools.SecurityResult, level configuration.RiskLevel, sources []explainSource, toolName string, args map[string]interface{}) error {
	jsonSources := make([]explainJSONSource, 0, len(sources))
	for _, s := range sources {
		jsonSources = append(jsonSources, explainJSONSource{
			ID:      s.id,
			Explain: s.explain,
			Level:   string(s.level),
		})
	}
	payload := explainJSONOutput{
		Tool:      toolName,
		Args:      args,
		RiskLevel: string(level),
		HardBlock: res.IsHardBlock,
		Sources:   jsonSources,
		Result:    res,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// explainToolList returns a sorted, comma-separated list of supported tools.
func explainToolList() string {
	names := make([]string, 0, len(explainSupportedTools))
	for name := range explainSupportedTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func init() {
	explainCmd.Flags().String("tool", "shell_command", "tool name to classify (shell_command, write_file, edit_file, write_structured_file, patch_structured_file, git, mkdir, fetch_url, web_search)")
	explainCmd.Flags().String("path", "", "file path (for write/edit tools)")
	explainCmd.Flags().String("operation", "", "git operation (for git tool)")
	explainCmd.Flags().Bool("json", false, "machine-readable JSON output")
	rootCmd.AddCommand(explainCmd)
}
