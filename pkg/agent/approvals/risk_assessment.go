package approvals

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
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

// AssessmentFromClassifier maps a static-classifier SecurityResult onto the
// canonical scale: SAFE→Low, CAUTION→Medium, DANGEROUS→High, hard-block→Critical.
func AssessmentFromClassifier(res tools.SecurityResult) RiskAssessment {
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

// AssessmentFromPersonaCascade builds an assessment from the persona/risk-
// profile cascade's RiskLevel verdict for a command.
func AssessmentFromPersonaCascade(level configuration.RiskLevel, reason string) RiskAssessment {
	return RiskAssessment{
		Level:       level,
		IsHardBlock: level == configuration.RiskLevelCritical,
		Sources:     []RiskSource{RiskSourcePersonaCascade},
		Reason:      reason,
	}
}

// Combine folds two assessments into one, taking the most restrictive Level.
// Critical always hard-blocks. Preserves PathTier and FileMode from the original.
func (ra RiskAssessment) Combine(other RiskAssessment) RiskAssessment {
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
		Sources:                    MergeRiskSources(winner.Sources, loser.Sources),
		PathTier:                   ra.PathTier,
		FileMode:                   ra.FileMode,
	}
	if merged.Level == configuration.RiskLevelCritical {
		merged.IsHardBlock = true
	}
	return merged
}

// MergeRiskSources concatenates two source lists, de-duplicating while
// preserving first-seen order so Explain() reads deterministically.
func MergeRiskSources(a, b []RiskSource) []RiskSource {
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

// ResolveOldDecision derives a one-word gating decision from the old
// dual-gate path's SecurityResult for shadow-mode comparison.
func ResolveOldDecision(res tools.SecurityResult) string {
	if res.ShouldBlock {
		return "block"
	}
	if res.ShouldPrompt {
		return "prompt"
	}
	return "allow"
}

// ResolveUnifiedDecision derives a one-word gating decision from a
// RiskAssessment for shadow-mode comparison with the old path.
func ResolveUnifiedDecision(ra RiskAssessment) string {
	if ra.IsHardBlock || ra.Level == configuration.RiskLevelCritical {
		return "block"
	}
	if ra.Level == configuration.RiskLevelHigh || ra.Level == configuration.RiskLevelMedium {
		return "prompt"
	}
	return "allow"
}

// IsGitRebaseCommand reports whether `command` contains a `git rebase`
// invocation that rewrites history (i.e. NOT `git rebase --abort`).
func IsGitRebaseCommand(command string) bool {
	command = shelltext.StripQuotedContent(command)
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
