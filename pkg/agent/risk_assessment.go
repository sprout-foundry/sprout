// RiskAssessment provides a unified, single-vocabulary risk assessment for tool calls,
// folding the static classifier and persona cascade onto the Low/Medium/High/Critical scale.
package agent

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// RiskSource identifies which check contributed to an assessment.
type RiskSource string

const (
	RiskSourceClassifier        RiskSource = "classifier"
	RiskSourcePersonaCascade    RiskSource = "persona-cascade"
	RiskSourceCriticalOp        RiskSource = "critical-op"
	RiskSourceGitHistoryRewrite RiskSource = "git-history-rewrite"
	RiskSourceGitRebase         RiskSource = "git-rebase"
	RiskSourceGitWrite          RiskSource = "git-write"
	RiskSourceFSTier            RiskSource = "fs-tier"
	RiskSourceWorkspacePolicy   RiskSource = "workspace-policy"
	RiskSourceHandler           RiskSource = "handler"
	RiskSourcePasswordPrompter  RiskSource = "password-prompter"
)

// RiskAssessment is the canonical, single-vocabulary verdict for a tool call.
type RiskAssessment struct {
	Level configuration.RiskLevel

	// IsHardBlock is true for critical-tier operations that no approval can
	// override (rm -rf /, fork bombs, mkfs).
	IsHardBlock bool

	RequiresIntentConfirmation bool

	Sources []RiskSource

	Reason string

	// PathTier and FileMode are structured fields for file-touching tools.
	PathTier PathTier

	FileMode string
}

// ResolveToolRisk produces the unified risk assessment for a tool call by
// folding all security inputs onto the Low/Medium/High/Critical scale.
func (a *Agent) ResolveToolRisk(toolName string, args map[string]interface{}) RiskAssessment {
	// 1. Static classifier (always)
	secResult := tools.ClassifyToolCall(toolName, args)
	assessment := assessmentFromClassifier(secResult)

	// Downgrade privileged commands when a password prompter is registered.
	if toolName == "shell_command" && a != nil && a.HasPasswordPrompter() {
		if secResult.Category == tools.RiskCategoryPrivileged && assessment.Level.Rank() >= configuration.RiskLevelHigh.Rank() {
			assessment.Level = configuration.RiskLevelMedium
			assessment.IsHardBlock = false
			assessment.Sources = append(assessment.Sources, RiskSourcePasswordPrompter)
			assessment.Reason = "privileged command allowed with password prompter (sudo/passwd will prompt for password)"
		}
	}

	// 2. Persona cascade (shell_command only)
	if toolName == "shell_command" && a != nil {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			level := a.EvaluateOperationRisk(cmd)
			assessment = assessment.combine(
				assessmentFromPersonaCascade(level, fmt.Sprintf("persona/profile risk cascade: %s", level)),
			)

			// 3. Git history-rewrite gate (promptable, not a hard block).
			// Rebase is unconditionally banned; --abort is the only permitted form.
			if isGitHistoryRewriteCommand(cmd) {
				if isGitRebaseCommand(cmd) {
					assessment = assessment.combine(
						RiskAssessment{
							Level:       configuration.RiskLevelCritical,
							IsHardBlock: true,
							Sources:     []RiskSource{RiskSourceGitRebase},
							Reason:      "git rebase is banned by AGENTS.md (all forms: interactive, --continue, --skip, `git pull --rebase`); use `git merge` to integrate upstream. The only permitted invocation is `git rebase --abort` for recovery.",
						},
					)
				} else {
					cfg := a.GetConfig()
					if cfg == nil || !cfg.AllowGitHistoryRewrite {
						assessment = assessment.combine(
							RiskAssessment{
								Level:   configuration.RiskLevelHigh,
								Sources: []RiskSource{RiskSourceGitHistoryRewrite},
								Reason:  "git history-rewrite operation requires approval",
							},
						)
					}
				}
			}

			// 4. Git write gate
			if isGitWriteCommand(cmd) && !a.isGitWriteAllowed() {
				assessment = assessment.combine(
					RiskAssessment{
						Level:   configuration.RiskLevelHigh,
						Sources: []RiskSource{RiskSourceGitWrite},
						Reason:  "git write operation not allowed for current persona",
					},
				)
			}

			// 6. Workspace security policy
			if cfg := a.GetConfig(); cfg != nil && cfg.SecurityPolicy != nil {
				policyAction := cfg.SecurityPolicy.Evaluate(cmd)
				switch policyAction {
				case configuration.PolicyDeny:
					assessment = assessment.combine(
						RiskAssessment{
							Level:       configuration.RiskLevelCritical,
							IsHardBlock: true,
							Sources:     []RiskSource{RiskSourceWorkspacePolicy},
							Reason:      "workspace security policy denies this command",
						},
					)
				case configuration.PolicyPrompt:
					if assessment.Level.Rank() <= configuration.RiskLevelLow.Rank() {
						assessment = assessment.combine(
							RiskAssessment{
								Level:   configuration.RiskLevelMedium,
								Sources: []RiskSource{RiskSourceWorkspacePolicy},
								Reason:  "workspace security policy requires prompt for this command",
							},
						)
					}
				}
			}
		}
	}

	// 5. Filesystem path-tier (file tools). Only write tools contribute risk.
	if (toolName == "write_file" || toolName == "edit_file" ||
		toolName == "write_structured_file" || toolName == "patch_structured_file" ||
		toolName == "read_file") && a != nil {
		if pathRaw, ok := args["path"].(string); ok && pathRaw != "" {
			home := detectHomeDir()
			tier := ClassifyPathAccess(pathRaw, a.GetWorkspaceRoot(), home, a.effectiveCwd())
			assessment.PathTier = tier
			assessment.FileMode = accessModeForTool(toolName)

			isWriteTool := toolName == "write_file" || toolName == "edit_file" ||
				toolName == "write_structured_file" || toolName == "patch_structured_file"

			if isWriteTool {
				switch tier {
				case PathTierSensitive:
					assessment = assessment.combine(
						RiskAssessment{
							Level:   configuration.RiskLevelHigh,
							Sources: []RiskSource{RiskSourceFSTier},
							Reason:  fmt.Sprintf("path %s is in a sensitive filesystem tier", pathRaw),
						},
					)
				case PathTierExternal:
					// Session-scoped folder allowlist: skip if user already approved this folder.
					if a.IsFolderSessionAllowed(pathRaw) {
						if a.debug {
							a.debugLog("[risk] %s path %s is under a session-allowed folder — skipping external-tier Medium contribution\n", toolName, pathRaw)
						}
					} else {
						assessment = assessment.combine(
							RiskAssessment{
								Level:   configuration.RiskLevelMedium,
								Sources: []RiskSource{RiskSourceFSTier},
								Reason:  fmt.Sprintf("path %s is outside the workspace (external tier)", pathRaw),
							},
						)
					}
				}
			}
		}
	}

	return assessment
}

// assessmentFromClassifier maps a static-classifier SecurityResult onto the
// canonical scale: SAFE→Low, CAUTION→Medium, DANGEROUS→High, hard-block→Critical.
func assessmentFromClassifier(res tools.SecurityResult) RiskAssessment {
	level := configuration.RiskLevelLow
	switch res.Risk {
	case tools.SecuritySafe:
		level = configuration.RiskLevelLow
	case tools.SecurityCaution:
		level = configuration.RiskLevelMedium
	case tools.SecurityDangerous:
		level = configuration.RiskLevelHigh
	}
	source := RiskSourceClassifier
	if res.IsHardBlock {
		level = configuration.RiskLevelCritical
		source = RiskSourceCriticalOp
	}
	return RiskAssessment{
		Level:                      level,
		IsHardBlock:                res.IsHardBlock,
		RequiresIntentConfirmation: res.IntentConfirmation,
		Sources:                    []RiskSource{source},
		Reason:                     res.Reasoning,
	}
}

// assessmentFromPersonaCascade builds an assessment from the persona/risk-
// profile cascade's RiskLevel verdict for a command.
func assessmentFromPersonaCascade(level configuration.RiskLevel, reason string) RiskAssessment {
	return RiskAssessment{
		Level:       level,
		IsHardBlock: level == configuration.RiskLevelCritical,
		Sources:     []RiskSource{RiskSourcePersonaCascade},
		Reason:      reason,
	}
}

// combine folds two assessments into one, taking the most restrictive Level.
// Critical always hard-blocks. Preserves PathTier and FileMode from the original.
func (ra RiskAssessment) combine(other RiskAssessment) RiskAssessment {
	winner := ra
	loser := other
	if other.Level.Rank() > ra.Level.Rank() {
		winner = other
		loser = ra
	}

	merged := RiskAssessment{
		Level:                      winner.Level,
		IsHardBlock:                ra.IsHardBlock || other.IsHardBlock,
		RequiresIntentConfirmation: ra.RequiresIntentConfirmation || other.RequiresIntentConfirmation,
		Reason:                     winner.Reason,
		Sources:                    mergeRiskSources(winner.Sources, loser.Sources),
		PathTier:                   ra.PathTier,
		FileMode:                   ra.FileMode,
	}
	if merged.Level == configuration.RiskLevelCritical {
		merged.IsHardBlock = true
	}
	return merged
}

// mergeRiskSources concatenates two source lists, de-duplicating while
// preserving first-seen order so Explain() reads deterministically.
func mergeRiskSources(a, b []RiskSource) []RiskSource {
	seen := make(map[RiskSource]bool, len(a)+len(b))
	out := make([]RiskSource, 0, len(a)+len(b))
	for _, src := range append(append([]RiskSource{}, a...), b...) {
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		out = append(out, src)
	}
	return out
}

// Explain renders a one-line human-readable summary of the assessment for
// diagnostics ("why was this gated?"). Sources are listed alphabetically for
// a stable rendering regardless of combination order.
func (ra RiskAssessment) Explain() string {
	srcs := make([]string, 0, len(ra.Sources))
	for _, s := range ra.Sources {
		srcs = append(srcs, string(s))
	}
	sort.Strings(srcs)

	level := string(ra.Level)
	if level == "" {
		level = "unknown"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "risk=%s", strings.ToUpper(level))
	if ra.IsHardBlock {
		b.WriteString(" (hard-block)")
	}
	if ra.RequiresIntentConfirmation {
		b.WriteString(" (intent-confirmation)")
	}
	if len(srcs) > 0 {
		fmt.Fprintf(&b, " source=%s", strings.Join(srcs, ","))
	}
	if strings.TrimSpace(ra.Reason) != "" {
		fmt.Fprintf(&b, " — %s", ra.Reason)
	}
	return b.String()
}

// resolveOldDecision derives a one-word gating decision from the old
// dual-gate path's SecurityResult for shadow-mode comparison.
func resolveOldDecision(res tools.SecurityResult) string {
	if res.ShouldBlock {
		return "block"
	}
	if res.ShouldPrompt {
		return "prompt"
	}
	return "allow"
}

// resolveUnifiedDecision derives a one-word gating decision from a
// RiskAssessment for shadow-mode comparison with the old path.
func resolveUnifiedDecision(ra RiskAssessment) string {
	if ra.IsHardBlock || ra.Level == configuration.RiskLevelCritical {
		return "block"
	}
	if ra.Level == configuration.RiskLevelHigh || ra.Level == configuration.RiskLevelMedium {
		return "prompt"
	}
	return "allow"
}

// isGitRebaseCommand reports whether `command` contains a `git rebase`
// invocation that rewrites history (i.e. NOT `git rebase --abort`).
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
