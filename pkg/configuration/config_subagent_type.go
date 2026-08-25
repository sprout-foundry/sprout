// Package configuration: SubagentType struct, IsCriticalOperation, and risk evaluation.
// (split from config_risk_subagent.go)
package configuration

import (
	"strings"
)

// IsCriticalOperation reports whether a command matches a pattern that
// is NEVER allowed regardless of profile, persona, or interactive
// approval. Reserved for operations that can permanently destroy the
// system or leave it in an unrecoverable state.
//
// This is the single source of truth for "critical" across every
// security gate: the static classifier (agent_tools.isCriticalSystemOperation
// delegates here) and the persona risk cascade (EvaluateOperationRisk)
// both consult it, so the two gates can't disagree on what's critical.
//
// Covers:
//   - rm -rf targeting root or the current dir (`/`, `/*`, `~`, `$HOME`, `.`, `*`)
//   - Classic fork-bomb pattern `:(){:|:&};:`
//   - Filesystem creation / raw disk overwrite (mkfs, dd to a block device)
//   - Mass process kills (killall -9 / -KILL)
//   - chmod 000 on a system root path
//   - Overwriting critical auth/system files (/etc/shadow, /etc/passwd, …)
//
// Tokenized matching matches invokesCommand semantics so a benign
// substring inside a path or argument can't trigger a false positive.
func IsCriticalOperation(command string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(command))

	// Fork bomb — the literal `:()` shell-function-named-colon is a
	// reliable signal. The token is unusual enough that false
	// positives are vanishingly rare.
	if strings.Contains(cmdLower, ":()") && strings.Contains(cmdLower, ":|:") {
		return true
	}

	// Strip heredoc bodies and quoted content before token-based matching
	// so DATA content inside a heredoc or string literal doesn't trigger
	// false-positive critical-pattern matches (e.g. a script that mentions
	// "rm -rf /" in a comment or string).
	cmdLower = stripHeredocAndQuotes(cmdLower)

	fields := strings.Fields(cmdLower)

	// rm -rf <root-equivalent or cwd wildcard>. We need rm invoked as a
	// command, with a recursive flag, AND a destructive target.
	if invokesCommand(fields, "rm") {
		hasRecursive := false
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				hasRecursive = true
				break
			}
			if len(f) > 2 && f[0] == '-' && f[1] != '-' && strings.ContainsAny(f, "rR") {
				hasRecursive = true
				break
			}
		}
		if hasRecursive {
			for _, f := range fields {
				switch f {
				case "/", "/*", "~", "~/", "$home", "${home}", "${home}/", "$home/", ".", "*":
					return true
				}
			}
		}
	}

	// mkfs / mkfs.* — formatting a filesystem destroys everything on the target.
	// Handle sudo prefix: "sudo mkfs.ext4 /dev/sda1" → fields[0] is "sudo".
	if len(fields) > 0 {
		checkIdx := 0
		if fields[0] == "sudo" && len(fields) > 1 {
			checkIdx = 1
		}
		if fields[checkIdx] == "mkfs" || strings.HasPrefix(fields[checkIdx], "mkfs.") {
			return true
		}
	}

	// dd reading from / writing to a primary block device.
	if invokesCommand(fields, "dd") {
		for _, disk := range []string{"/dev/sda", "/dev/sdb", "/dev/nvme", "/dev/vda"} {
			if strings.Contains(cmdLower, "of="+disk) || strings.Contains(cmdLower, "if="+disk) {
				return true
			}
		}
	}

	// killall -9 / -KILL — mass process termination.
	// Handle sudo prefix: "sudo killall -9" must also be critical.
	if strings.HasPrefix(cmdLower, "killall -9") || strings.HasPrefix(cmdLower, "killall -kill") ||
		strings.HasPrefix(cmdLower, "sudo killall -9") || strings.HasPrefix(cmdLower, "sudo killall -kill") {
		return true
	}

	// chmod 000 on a system root path (locks everyone out).
	if strings.Contains(cmdLower, "chmod 000 /") {
		return true
	}

	// Overwriting critical auth/system files.
	for _, file := range []string{"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/root/.ssh/authorized_keys"} {
		if strings.Contains(cmdLower, "> "+file) || strings.Contains(cmdLower, ">> "+file) || strings.Contains(cmdLower, "echo "+file) {
			return true
		}
	}

	return false
}

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID                 string            `json:"id"`                             // Unique identifier (e.g., "coder", "tester", "debugger")
	Name               string            `json:"name"`                           // Human-readable name (e.g., "Coder", "Tester")
	Description        string            `json:"description"`                    // What this subagent specializes in
	Provider           string            `json:"provider"`                       // Provider for this subagent type (optional, falls back to SubagentProvider)
	Model              string            `json:"model"`                          // Model for this subagent type (optional, falls back to SubagentModel)
	SystemPrompt       string            `json:"system_prompt"`                  // Relative path to system prompt file (e.g., "subagent_prompts/coder.md")
	SystemPromptText   string            `json:"system_prompt_text,omitempty"`   // Optional inline system prompt text (replaces base prompt entirely)
	SystemPromptAppend string            `json:"system_prompt_append,omitempty"` // Optional inline text appended to the base or loaded system prompt (for composition)
	AllowedTools       []string          `json:"allowed_tools,omitempty"`        // Optional explicit tool allowlist for focused persona behavior
	Aliases            []string          `json:"aliases,omitempty"`              // Optional aliases (e.g., "web-scraper")
	Enabled            bool              `json:"enabled"`                        // Catalog-only: every shipped persona sets this true. Runtime "is this persona usable?" is determined by Config.DisabledPersonas (user) + LocalOnly (env). Kept for catalog hygiene + defense-in-depth in case a future variant ships with a deliberately-disabled entry.
	LocalOnly          bool              `json:"local_only,omitempty"`           // Only available in local mode (not cloud)
	Delegatable        bool              `json:"delegatable,omitempty"`          // Whether this persona can be used as a subagent (default: true for worker personas, false for orchestrator personas)
	AutoApproveRules   *AutoApproveRules `json:"auto_approve_rules,omitempty"`   // Risk cascade rules for the runtime auto-approve check
	// Capabilities is an explicit list of agency grants this persona holds
	// (e.g. "git_write"). Replaces sniffing AutoApproveRules to infer what a
	// persona is allowed to do. Use HasCapability to query.
	Capabilities []string `json:"capabilities,omitempty"`
	// CanSpawnNonDelegatable lists otherwise-undelegatable persona IDs that
	// this persona may spawn. Replaces the hardcoded EA-spawn-authority
	// special-case. The coordinator carries ["orchestrator"] to enable the
	// canonical coordinator→orchestrator→specialist chain.
	CanSpawnNonDelegatable []string `json:"can_spawn_non_delegatable,omitempty"`
}

// HasCapability reports whether the persona declares the given capability
// name. Comparison is case-insensitive and whitespace-tolerant.
func (st *SubagentType) HasCapability(name string) bool {
	if st == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return false
	}
	for _, c := range st.Capabilities {
		if strings.ToLower(strings.TrimSpace(c)) == target {
			return true
		}
	}
	return false
}

// GetAutoApproveRules returns the auto-approve rules for this persona,
// falling back to defaults if none are configured.
// Callers MUST NOT modify the returned struct's slice fields,
// as they may share backing arrays with the original config.
func (st *SubagentType) GetAutoApproveRules() AutoApproveRules {
	if st.AutoApproveRules != nil {
		return *st.AutoApproveRules
	}
	return DefaultAutoApproveRules()
}

// EvaluateOperationRisk determines the risk level of a shell operation
// based on the persona's auto-approve rules.
// Returns RiskLevelCritical for absolute-block patterns, otherwise
// RiskLevelLow, RiskLevelMedium, or RiskLevelHigh per the rules.
func (st *SubagentType) EvaluateOperationRisk(command string) RiskLevel {
	// Critical patterns are ALWAYS blocked, regardless of persona
	// rules or profile. Checked first so no rule lookup can shadow
	// them.
	if IsCriticalOperation(command) {
		return RiskLevelCritical
	}

	rules := st.GetAutoApproveRules()

	// Strip heredoc bodies and quoted strings before pattern matching.
	// Without this, a heredoc or string literal containing "git checkout"
	// or "rm -rf" would falsely match risk patterns — the classic case
	// is a script that embeds a command example as DATA (e.g. writing a
	// Go test file whose source code mentions "git checkout").
	cmdLower := strings.ToLower(stripHeredocAndQuotes(command))

	// HighRiskNever patterns are gated. "force_flag" is one such
	// pattern that lives in the list for all gated profiles; the
	// Unrestricted profile has it removed so -f / --force passes
	// through. (Prior to SP-058 there was a hardcoded
	// containsForceFlag short-circuit before the loop; that's been
	// folded into the data-driven check so profiles can fully
	// control gating.)
	for _, pattern := range rules.HighRiskNever {
		if matchesRiskPattern(cmdLower, pattern) {
			return RiskLevelHigh
		}
	}

	// Determine the operation category for classification
	opCategory := categorizeCommand(cmdLower)

	// Check if the operation is explicitly in the low-risk list
	for _, pattern := range rules.LowRiskOps {
		if opCategory == pattern {
			return RiskLevelLow
		}
	}

	// Check if the operation is in the medium-risk list
	for _, pattern := range rules.MediumRiskOps {
		if opCategory == pattern {
			return RiskLevelMedium
		}
	}

	// Fall back to the profile's declared DefaultRisk for unknown
	// operations. Empty / unspecified default behaves as Medium for
	// backward compatibility with personas configured before SP-058.
	// DefaultRisk = Critical is legitimate for the readonly profile
	// (blocks all non-read ops outright).
	switch rules.DefaultRisk {
	case RiskLevelLow:
		return RiskLevelLow
	case RiskLevelHigh:
		return RiskLevelHigh
	case RiskLevelCritical:
		return RiskLevelCritical
	default:
		return RiskLevelMedium
	}
}
