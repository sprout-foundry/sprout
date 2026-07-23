package agent

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SP-068 Phase 1 — One risk scale.
//
// Historically a tool call is judged by two independent vocabularies: the
// static classifier (pkg/agent_tools: SAFE/CAUTION/DANGEROUS) and the
// persona risk cascade (pkg/configuration: Low/Medium/High/Critical). This
// file introduces a single canonical representation — RiskAssessment, on
// the Low/Medium/High/Critical scale — and the mapping that folds the
// classifier's three tiers onto it.
//
// Phase 1 is deliberately behavior-preserving: these types and helpers are
// the vocabulary the Phase 2 resolver will consume. Nothing here changes a
// gating decision on its own; the golden tests lock the mapping so Phase 2
// can rewire the call sites without drift.

// RiskSource identifies which check contributed to an assessment, so a
// decision can be explained (Phase 3 `sprout explain`) instead of being an
// opaque "blocked".
type RiskSource string

const (
	// RiskSourceClassifier — the static, string-based classifier
	// (pkg/agent_tools.ClassifyToolCall).
	RiskSourceClassifier RiskSource = "classifier"
	// RiskSourcePersonaCascade — the persona / risk-profile cascade
	// (Agent.EvaluateOperationRisk).
	RiskSourcePersonaCascade RiskSource = "persona-cascade"
	// RiskSourceCriticalOp — the built-in critical-operation hard-block
	// (configuration.IsCriticalOperation).
	RiskSourceCriticalOp RiskSource = "critical-op"
	// RiskSourceGitHistoryRewrite — git commands that can lose commit history
	RiskSourceGitHistoryRewrite RiskSource = "git-history-rewrite"
	// RiskSourceGitRebase — git rebase (AGENTS.md: unconditionally banned)
	RiskSourceGitRebase RiskSource = "git-rebase"
	// RiskSourceGitWrite — git write operations not allowed by persona
	RiskSourceGitWrite RiskSource = "git-write"
	// RiskSourceFSTier — filesystem path-tier classification (Sensitive/External)
	RiskSourceFSTier RiskSource = "fs-tier"
	// RiskSourceWorkspacePolicy — workspace security policy evaluation
	RiskSourceWorkspacePolicy RiskSource = "workspace-policy"
	// RiskSourceHandler — a security error raised by a tool handler at
	// execution time (not by the pre-execute gate). Used when the only
	// signal available is the typed SecurityError returned by the handler.
	RiskSourceHandler RiskSource = "handler"
	// RiskSourcePasswordPrompter — password prompter is registered, so
	// privileged commands (sudo, passwd) are downgraded from block to prompt.
	RiskSourcePasswordPrompter RiskSource = "password-prompter"
)

// RiskAssessment is the canonical, single-vocabulary verdict for a tool
// call. Phase 2 makes it the single output of the unified resolver; Phase 1
// builds and tests it alongside the existing gates.
//
// SP-068 SP-127 synergy: PathTier and FileMode are structured fields that let
// consumers distinguish "elevated due to sensitive system path" from "elevated
// due to destructive shell command" without parsing Reason strings.
type RiskAssessment struct {
	// Level is the canonical risk on the Low/Medium/High/Critical scale.
	Level configuration.RiskLevel

	// IsHardBlock is true for critical-tier operations that no approval can
	// override (rm -rf /, fork bombs, mkfs).
	IsHardBlock bool

	// RequiresIntentConfirmation marks a safe-but-consequential operation
	// (e.g. launching an autonomous workflow) that needs explicit user
	// intent. It is orthogonal to Level — such an op is Low risk but still
	// gated on intent.
	RequiresIntentConfirmation bool

	// Sources lists every check that contributed, in precedence order.
	Sources []RiskSource

	// Reason is a human-readable explanation of the verdict.
	Reason string

	// PathTier is the filesystem path-tier for file-touching tools
	// (PathTierWorkspace, PathTierExternal, PathTierSensitive). Zero value
	// (empty string) means "not a file operation" or "tier not assessed".
	// Consumers can use this to distinguish "elevated due to sensitive
	// system path" from "elevated due to destructive shell command"
	// without parsing Reason strings.
	PathTier PathTier

	// FileMode is "read" or "write" for file operations. Empty for
	// non-file operations.
	FileMode string
}

// ResolveToolRisk produces the unified, single-vocabulary assessment for a
// tool call by folding all available security inputs onto one
// Low/Medium/High/Critical scale. It incorporates:
//
//  1. Static classifier (pkg/agent_tools.ClassifyToolCall)
//  2. Persona / risk-profile cascade (Agent.EvaluateOperationRisk)
//  3. Git history-rewrite gate (isGitHistoryRewriteCommand + AllowGitHistoryRewrite)
//  4. Git write gate (isGitWriteCommand + isGitWriteAllowed)
//  5. Filesystem path-tier (ClassifyPathAccess for file tools)
//  6. Workspace security policy (SecurityPolicy.Evaluate for shell_command)
//
// SP-068 Phase 2: this is the canonical risk view for gating when
// UnifiedRiskResolver is enabled. When the flag is off it still powers
// diagnostics (the gate's debug "[risk]" line and the future `sprout
// explain`) and shadow-mode logging.
func (a *Agent) ResolveToolRisk(toolName string, args map[string]interface{}) RiskAssessment {
	// 1. Static classifier (always)
	secResult := tools.ClassifyToolCall(toolName, args)
	assessment := assessmentFromClassifier(secResult)

	// SP-089-4: when a password prompter is registered, downgrade privileged
	// commands (sudo, passwd, su) from High/Critical block to Medium prompt.
	// This lets the user actually type their password instead of the command
	// being hard-blocked. Destructive commands (rm -rf, dd, mkfs) are NEVER
	// downgraded — only RiskCategoryPrivileged.
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
			// These operations are recoverable via reflog, so they prompt the
			// user rather than being hard-blocked. AllowGitHistoryRewrite=true
			// skips the prompt entirely (config-level opt-in).
			// Exception: rebase is unconditionally banned by AGENTS.md, regardless
			// of AllowGitHistoryRewrite. The only permitted invocation is --abort.
			if isGitHistoryRewriteCommand(cmd) {
				if isGitRebaseCommand(cmd) {
					// AGENTS.md: rebase is unconditionally banned — every
					// form including interactive, --continue, --skip, and
					// `git pull --rebase`. The only permitted invocation is
					// pure `git rebase --abort` (recovery from a prior
					// session's interrupted rebase).
					assessment = assessment.combine(
						RiskAssessment{
							Level:       configuration.RiskLevelCritical,
							IsHardBlock: true,
							Sources:     []RiskSource{RiskSourceGitRebase},
							Reason:      "git rebase is banned by AGENTS.md (all forms: interactive, --continue, --skip, `git pull --rebase`); use `git merge` to integrate upstream. The only permitted invocation is `git rebase --abort` for recovery.",
						},
					)
				} else {
					// Other history-rewrite ops: branch -D, tag -d, reset --hard <commit-ish>
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
					// Only contribute if classifier didn't already flag it
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

	// 5. Filesystem path-tier (file tools)
	// SP-068 SP-127 synergy: populate PathTier and FileMode for all file tools
	// so consumers can distinguish path-tier from risk-tier. Risk contribution
	// only applies to write tools (reads to sensitive paths are less concerning).
	if (toolName == "write_file" || toolName == "edit_file" ||
		toolName == "write_structured_file" || toolName == "patch_structured_file" ||
		toolName == "read_file") && a != nil {
		if pathRaw, ok := args["path"].(string); ok && pathRaw != "" {
			home := detectHomeDir()
			tier := ClassifyPathAccess(pathRaw, a.GetWorkspaceRoot(), home, a.effectiveCwd())
			assessment.PathTier = tier
			assessment.FileMode = accessModeForTool(toolName)

			// Only write tools contribute fs-tier risk; reads are informational
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
					// Session-scoped folder allowlist: if the user already
					// clicked "Allow folder this session" for this path's
					// directory (see handleFileSecurityError /
					// applyFilesystemDecision), skip the Medium contribution.
					// Without this, the unified gate re-prompts on every
					// write to an external folder the user already approved —
					// because ResolveToolRisk runs BEFORE the filesystem
					// layer that owns the allowlist. Sensitive-tier paths
					// never reach this branch (they're caught above) and can
					// never be session-allowlisted.
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
// canonical scale:
//
//	SAFE                 → Low
//	CAUTION              → Medium
//	DANGEROUS            → High
//	(IsHardBlock / crit) → Critical
//
// ShouldBlock/ShouldPrompt are not part of the canonical Level — they are
// downstream policy decisions the resolver derives from Level + context.
// IntentConfirmation is carried through as its own orthogonal flag.
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

// combine folds two assessments into one, taking the most restrictive Level
// (the resolver's "tighten, never silence" rule). The OR of hard-block and
// intent-confirmation flags is kept, and both sets of sources are merged so
// the result can explain every contributing check.
func (ra RiskAssessment) combine(other RiskAssessment) RiskAssessment {
	winner := ra
	loser := other
	// The higher-ranked Level wins; its Reason is the headline. Ties keep
	// ra as the winner (stable).
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
		// Preserve file-operation context from the original assessment.
		// SP-068 SP-127 synergy: these fields are set before any combine() calls
		// and should survive the fold so consumers can distinguish path-tier
		// from risk-tier elevation.
		PathTier: ra.PathTier,
		FileMode: ra.FileMode,
	}
	// A combined Critical Level always hard-blocks even if only one input
	// flagged it, keeping the invariant that Critical is unconditional.
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
