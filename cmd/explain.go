//go:build !js

// Package cmd provides the `sprout explain` subcommand for human-readable
// risk assessment of shell commands.
//
// The explain command runs a command string through the same security
// classification pipeline used at runtime (static classifier, critical-op
// gate, git-history-rewrite gate) and prints a structured breakdown of
// contributing checks and their individual verdicts.
//
// Checks that require Agent runtime context (persona-cascade, git-write,
// workspace-policy, fs-tier) are shown as context-dependent in the output,
// since this CLI command operates without a live Agent instance.
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Risk assessment helpers (replicated from pkg/agent to avoid circular deps)
// ---------------------------------------------------------------------------

// classifyLevel maps a static-classifier SecurityResult onto the canonical
// Low/Medium/High/Critical risk scale, mirroring assessmentFromClassifier()
// in pkg/agent/risk_assessment.go.
func classifyLevel(res tools.SecurityResult) configuration.RiskLevel {
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

// ---------------------------------------------------------------------------
// Git history-rewrite detection (replicated from pkg/agent/tool_handlers.go)
// ---------------------------------------------------------------------------

// stripQuotedContent replaces all single-quoted and double-quoted string
// content with spaces, preserving quote boundaries so token positions stay
// stable. This prevents false-positive git command detection when words
// like "git reset" appear inside quoted arguments.
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
// invocation that can lose commit history (a ref moves backward, a
// branch/tag pointer disappears, a rebase rewrites commits).
//
// Replicated from pkg/agent/tool_handlers.go to avoid importing pkg/agent
// (circular dependency).
//
// Specifically matches:
//   - `git reset --hard <commit-ish>` (backward ref-move, where commit-ish is not HEAD)
//   - `git rebase` (any form — rewrites or drops commits)
//   - `git branch -d`/`-D`/`--delete` (deletes a branch ref)
//   - `git tag -d`/`--delete` (deletes a tag ref)
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
		// Find the subcommand, skipping leading git global flags.
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
			return true
		case "reset":
			// `reset --hard` followed by an explicit commit-ish other than
			// HEAD is a backward ref move. Bare `reset --hard` or
			// `reset --hard HEAD` only mutates the working tree.
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
			// `--hard` with no further args or with `HEAD` as the only
			// other token is working-tree-only. Anything else abandons commits.
			hasCommitIsh := false
			for _, a := range rest {
				if a == "--hard" || strings.HasPrefix(a, "-") {
					continue
				}
				if a == "HEAD" {
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
// whose intent requires the orchestrator git-write flow: `commit`, `push`,
// `merge`, `clone`, `init`, `worktree`, and branch/tag CREATE operations.
//
// Replicated from pkg/agent/tool_handlers.go to avoid importing pkg/agent
// (circular dependency).
//
// Tier A working-tree-mutating ops (`checkout`, `switch`, `restore`, etc.)
// are NOT gated here — the change tracker captures before-content and
// recover_file provides recovery. Tier B history-loss ops (`rebase`,
// `reset --hard <commit-ish>`, `branch -D`, `tag -d`) are routed through
// isGitHistoryRewriteCommand instead.
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
		// Find the actual subcommand (skip "git" and any leading flags).
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

			// branch / tag — only CREATE/UPDATE operations land here.
			// Deletes (`-d`/`-D`/`--delete`) are caught upstream by
			// isGitHistoryRewriteCommand.
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

			// Intent gates: commit, push, merge, clone, init, worktree.
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

// ---------------------------------------------------------------------------
// checkResult holds the outcome of a single contributing check.
// ---------------------------------------------------------------------------

type checkResult struct {
	evaluated    bool
	level        configuration.RiskLevel
	reason       string
	riskType     string
	na           bool   // not applicable for this command
	needsContext bool   // requires agent runtime context
	naReason     string // custom n/a reason
}

func symbolFor(cr checkResult) string {
	if !cr.evaluated || cr.needsContext {
		return "○"
	}
	if cr.na {
		return "✓"
	}
	return "✓"
}

func displayFor(cr checkResult) string {
	if !cr.evaluated && cr.needsContext {
		return "(requires agent runtime context)"
	}
	if cr.needsContext {
		if cr.naReason != "" {
			return fmt.Sprintf("(%s)", cr.naReason)
		}
		return "(requires agent runtime context)"
	}
	if cr.na {
		if cr.naReason != "" {
			return fmt.Sprintf("(%s)", cr.naReason)
		}
		return "(n/a for this command)"
	}
	// Contributed with a level.
	levelStr := strings.ToUpper(string(cr.level))
	if cr.riskType != "" {
		levelStr = fmt.Sprintf("%s (%s)", levelStr, cr.riskType)
	}
	if cr.reason != "" {
		return fmt.Sprintf("%s: %s", levelStr, cr.reason)
	}
	return levelStr
}

// ---------------------------------------------------------------------------
// explainCmd — `sprout explain '<command>'`
// ---------------------------------------------------------------------------

var explainCmd = &cobra.Command{
	Use:   "explain <command>",
	Short: "Explain the risk assessment for a shell command",
	Long: `Run a shell command through the security classification pipeline and print
a human-readable breakdown of every contributing check.

This is the same pipeline used at runtime to decide whether to block,
prompt, or auto-approve a tool call. The explain command shows each
check's individual verdict and the combined final assessment.

Examples:
  sprout explain 'rm -rf /'
  sprout explain 'git reset --hard HEAD~5'
  sprout explain 'ls -la'
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExplain(cmd, args[0])
	},
}

// runExplain executes the risk assessment pipeline and prints the result.
func runExplain(cmd *cobra.Command, command string) error {
	out := cmd.OutOrStdout()

	// ── 1. Static classifier ─────────────────────────────────────────────
	classifierResult := tools.ClassifyToolCall("shell_command", map[string]interface{}{
		"command": command,
	})
	classifierLevel := classifyLevel(classifierResult)

	// ── 2. Critical operation gate ───────────────────────────────────────
	isCritical := configuration.IsCriticalOperation(command)

	// ── 3. Git history-rewrite gate ──────────────────────────────────────
	isGitHistoryRewrite := isGitHistoryRewriteCommand(command)

	// ── Combine assessments ──────────────────────────────────────────────
	// Start with classifier, then tighten with critical-op and git gates.
	level := classifierLevel
	isHardBlock := classifierResult.IsHardBlock
	sources := make([]string, 0, 4)
	reason := classifierResult.Reasoning

	// Track individual check results for the detailed breakdown.
	checks := make(map[string]checkResult)

	// Classifier
	checks["classifier"] = checkResult{
		evaluated: true,
		level:     classifierLevel,
		reason:    classifierResult.Reasoning,
		riskType:  classifierResult.Risk.String(),
	}
	sources = append(sources, "classifier")

	// Critical operation
	if isCritical {
		if level.Rank() < configuration.RiskLevelCritical.Rank() {
			level = configuration.RiskLevelCritical
			reason = "Critical system operation — hard-blocked"
		}
		isHardBlock = true
		sources = append(sources, "critical-op")
		checks["critical-op"] = checkResult{
			evaluated: true,
			level:     configuration.RiskLevelCritical,
			reason:    "Critical system operation — hard-blocked",
		}
	} else {
		checks["critical-op"] = checkResult{evaluated: true, na: true}
	}

	// Git history-rewrite
	if isGitHistoryRewrite {
		if level.Rank() < configuration.RiskLevelCritical.Rank() {
			level = configuration.RiskLevelCritical
			reason = "git history-rewrite operation blocked by default"
		}
		isHardBlock = true
		sources = append(sources, "git-history-rewrite")
		checks["git-history-rewrite"] = checkResult{
			evaluated: true,
			level:     configuration.RiskLevelCritical,
			reason:    "blocked unless allow_git_history_rewrite=true",
		}
	} else {
		checks["git-history-rewrite"] = checkResult{evaluated: true, na: true}
	}

	// Runtime-context checks (shown as placeholders)
	checks["persona-cascade"] = checkResult{evaluated: false, needsContext: true}

	// Git write check
	isGitWrite := isGitWriteCommand(command)
	if isGitWrite {
		checks["git-write"] = checkResult{
			evaluated:    true,
			needsContext: true,
			naReason:     "agent persona determines if allowed",
		}
	} else {
		checks["git-write"] = checkResult{evaluated: true, na: true}
	}

	checks["fs-tier"] = checkResult{evaluated: true, na: true, naReason: "n/a for shell_command"}
	checks["workspace-policy"] = checkResult{evaluated: false, needsContext: true}

	// Sort sources for stable output
	sort.Strings(sources)

	// ── Render ───────────────────────────────────────────────────────────
	fprint := func(format string, args ...interface{}) {
		fmt.Fprintf(out, format+"\n", args...)
	}

	fprint("Risk Assessment")
	fprint("===============")
	fprint("Level:        %s (hard-block: %v)", strings.ToUpper(string(level)), isHardBlock)
	fprint("Sources:      %s", strings.Join(sources, ", "))
	fprint("Reason:       %s", reason)

	fprint("")
	fprint("Contributing checks:")

	// Render each check in a fixed order.
	checkOrder := []string{
		"classifier",
		"critical-op",
		"git-history-rewrite",
		"persona-cascade",
		"git-write",
		"fs-tier",
		"workspace-policy",
	}

	for _, name := range checkOrder {
		cr := checks[name]
		fprint("  %s %-22s → %s", symbolFor(cr), name, displayFor(cr))
	}

	return nil
}

func init() {
	rootCmd.AddCommand(explainCmd)
}
