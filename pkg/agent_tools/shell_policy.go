// Shell policy — user-configurable safe/dangerous pattern resolution layer.
//
// This package provides a ShellPolicy type that compiles user-defined
// safe/dangerous shell command patterns (prefix or regex) and applies
// them AFTER built-in security classification to tighten or loosen
// the result. The global policy is set via SetShellPolicy and read
// atomically during classification.
//
// Hard invariants:
//   - A user SAFE pattern can NEVER override a built-in DANGEROUS or hard-block.
//   - A user DANGEROUS pattern can NEVER override a built-in hard-block
//     (critical system operations are always blocked).
//   - Within a tier, longest match string wins (most specific overrides broad).
package tools

import (
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ShellPolicy holds compiled user-defined shell patterns used to tighten or
// loosen the built-in security classification. Set globally via SetShellPolicy;
// accessed atomically during classification. nil means no user policy is active
// (built-in classification is used unchanged).
type ShellPolicy struct {
	safePatterns      []compiledPattern
	dangerousPatterns []compiledPattern
}

// compiledPattern holds a single pre-compiled user shell classification pattern.
type compiledPattern struct {
	match  string         // original match string (for "longest match wins" tiebreak + source annotation)
	kind   string         // "prefix" or "regex"
	reason string
	prefix string         // pre-lowercased prefix string (for kind=="prefix")
	regex  *regexp.Regexp // non-nil for kind=="regex"
}

// NewShellPolicy compiles the patterns from a ShellConfig. Returns an error if
// any regex pattern fails to compile. Empty match strings are silently ignored.
func NewShellPolicy(cfg configuration.ShellConfig) (*ShellPolicy, error) {
	p := &ShellPolicy{}

	for _, pat := range cfg.UserSafePatterns {
		cp, err := compilePattern(pat)
		if err != nil {
			return nil, err
		}
		if cp != nil {
			p.safePatterns = append(p.safePatterns, *cp)
		}
	}

	for _, pat := range cfg.UserDangerousPatterns {
		cp, err := compilePattern(pat)
		if err != nil {
			return nil, err
		}
		if cp != nil {
			p.dangerousPatterns = append(p.dangerousPatterns, *cp)
		}
	}

	return p, nil
}

// compilePattern compiles a single ShellPattern into a compiledPattern.
// Returns nil for empty match strings (silently ignored).
func compilePattern(pat configuration.ShellPattern) (*compiledPattern, error) {
	if pat.Match == "" {
		return nil, nil
	}

	cp := &compiledPattern{
		match:  pat.Match,
		kind:   pat.Kind,
		reason: pat.Reason,
	}

	switch pat.Kind {
	case "prefix":
		cp.prefix = strings.ToLower(pat.Match)
	case "regex":
		re, err := regexp.Compile(pat.Match)
		if err != nil {
			return nil, err
		}
		cp.regex = re
	default:
		// Unknown kind treated as prefix (normalized by config.Validate)
		cp.kind = "prefix"
		cp.prefix = strings.ToLower(pat.Match)
	}

	return cp, nil
}

// matchesOne reports whether cp matches the normalized (lowercased, trimmed) command.
func matchesOne(cp *compiledPattern, norm string) bool {
	switch cp.kind {
	case "prefix":
		return strings.HasPrefix(norm, cp.prefix)
	case "regex":
		return cp.regex.MatchString(norm)
	default:
		return false
	}
}

// applyTo checks the command against user patterns and returns the (possibly
// modified) result plus a bool indicating whether a user pattern matched.
//
// Resolution order (most-restrictive wins):
//   1. If the built-in result is already a hard-block, return unchanged.
//   2. User DANGEROUS patterns: escalate to SecurityDangerous with hard-block.
//      Can override built-in CAUTION and SAFE, but NOT a hard-block.
//   3. User SAFE patterns: downgrade to SecuritySafe. Can override built-in
//      CAUTION only — never DANGEROUS or hard-block.
//   4. Within each tier, longest match string wins.
func (p *ShellPolicy) applyTo(result SecurityResult, command string) (SecurityResult, bool) {
	// Hard invariant: no user pattern can override a built-in hard-block.
	if result.IsHardBlock {
		return result, false
	}

	norm := strings.ToLower(strings.TrimSpace(command))
	originalRisk := result.Risk

	// Find best matching dangerous pattern (longest match wins within tier).
	var bestDangerous *compiledPattern
	for i := range p.dangerousPatterns {
		cp := &p.dangerousPatterns[i]
		if matchesOne(cp, norm) {
			if bestDangerous == nil || len(cp.match) > len(bestDangerous.match) {
				bestDangerous = cp
			}
		}
	}

	// Find best matching safe pattern (longest match wins within tier).
	var bestSafe *compiledPattern
	for i := range p.safePatterns {
		cp := &p.safePatterns[i]
		if matchesOne(cp, norm) {
			if bestSafe == nil || len(cp.match) > len(bestSafe.match) {
				bestSafe = cp
			}
		}
	}

	// Apply dangerous pattern first — most-restrictive wins.
	if bestDangerous != nil {
		reason := "User dangerous pattern matched"
		if bestDangerous.reason != "" {
			reason = "User dangerous pattern matched: " + bestDangerous.reason
		}
		result = SecurityResult{
			Risk:         SecurityDangerous,
			Reasoning:    reason,
			ShouldBlock:  true,
			ShouldPrompt: true,
			IsHardBlock:  true,
			RiskType:     "user_dangerous_pattern",
			Category:     RiskCategoryUnknown,
		}
		return result, true
	}

	// Apply safe pattern — only if original built-in result is CAUTION.
	// User SAFE must NEVER override built-in DANGEROUS or a hard-block.
	// (The hard-block case was already returned at the top of applyTo.)
	if bestSafe != nil && originalRisk == SecurityCaution {
		reason := "User safe pattern matched"
		if bestSafe.reason != "" {
			reason = "User safe pattern matched: " + bestSafe.reason
		}
		result = SecurityResult{
			Risk:      SecuritySafe,
			Reasoning: reason,
			Category:  result.Category, // preserve built-in category
		}
		return result, true
	}

	// A built-in DANGEROUS result is authoritative — user SAFE must never
	// downgrade it. (DANGEROUS with IsHardBlock was already returned at the
	// top; this covers DANGEROUS without IsHardBlock, e.g. `git push --force`.)
	if bestSafe != nil && originalRisk == SecurityDangerous {
		return result, false
	}

	return result, false
}

// globalShellPolicy holds the package-level shell policy instance.
// Set via SetShellPolicy; accessed atomically during classification.
var globalShellPolicy atomic.Pointer[ShellPolicy]

// SetShellPolicy sets the package-level shell policy. Called during
// initialization before concurrent classification begins.
func SetShellPolicy(p *ShellPolicy) {
	globalShellPolicy.Store(p)
}

// GetShellPolicy returns the current package-level shell policy, or nil if none is set.
func GetShellPolicy() *ShellPolicy {
	return globalShellPolicy.Load()
}
