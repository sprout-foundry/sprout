package playbooks

import (
	"os"
	"regexp"
	"strings"
)

// FeatureToggleSmallPlaybook adds a small feature or guard behind a flag.
type FeatureToggleSmallPlaybook struct{}

func (p FeatureToggleSmallPlaybook) Name() string { return "feature_toggle_small" }
func (p FeatureToggleSmallPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	return strings.Contains(lo, "small feature") || strings.Contains(lo, "add option") || strings.Contains(lo, "add flag")
}
func (p FeatureToggleSmallPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Add a minimal feature behind a guard or flag to reduce risk."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Add small feature guarded by flag",
			Instructions:       "Implement minimal logic and wire-up. Default behavior must remain unchanged when the flag is off.",
			ScopeJustification: "Limits blast radius while delivering value.",
		})
	}
	return plan
}

// DependencyUpdatePlaybook bumps or pins a dependency.
type DependencyUpdatePlaybook struct{}

func (p DependencyUpdatePlaybook) Name() string { return "dependency_update" }
func (p DependencyUpdatePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"update dependency", "bump", "upgrade package", "go get ", "go mod", "pin version", "upgrade dependency"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	// crude detection of module@version mention
	if regexp.MustCompile(`[a-z0-9\./_-]+@[vV]?\d+\.`).FindString(lo) != "" {
		return true
	}
	return false
}
func (p DependencyUpdatePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Safely update a dependency and adapt minimal necessary code changes."}

	mod, ver := parseGoModuleFromIntent(userIntent)
	candidates := make([]string, 0, len(estimatedFiles)+8)
	candidates = append(candidates, estimatedFiles...)

	// Always include go.mod if present
	if _, err := os.Stat("go.mod"); err == nil {
		candidates = append(candidates, "go.mod")
	}
	if mod != "" {
		candidates = append(candidates, findFilesImportingModule(mod)...)
	}

	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		instr := "Modify imports or small call-site changes required by the update. Avoid broad refactors."
		if f == "go.mod" && mod != "" {
			if ver != "" {
				instr = "Update the required version for " + mod + " to " + ver + "; then run `go mod tidy`."
			} else {
				instr = "Upgrade or pin the required version for " + mod + "; then run `go mod tidy`."
			}
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Adjust code for dependency update if needed",
			Instructions:       instr,
			ScopeJustification: "Keeps the project current with minimal edits.",
		})
	}
	return plan
}

// LintFixPlaybook applies linter-driven fixes.
type LintFixPlaybook struct{}

func (p LintFixPlaybook) Name() string { return "lint_fix" }
func (p LintFixPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"lint", "format", "style fix", "gofmt", "go fmt", "golangci", "staticcheck", "go vet"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p LintFixPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply linter/style fixes while preserving semantics."}
	candidates := make([]string, 0, len(estimatedFiles)+64)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		// Prefer smaller, targeted pass: limit number of files to reduce noise
		candidates = append(candidates, findGoFilesLimited(150)...)
		// Include common config files
		for _, f := range []string{".golangci.yml", ".golangci.yaml"} {
			if _, err := os.Stat(f); err == nil {
				candidates = append(candidates, f)
			}
		}
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		instr := "Perform minimal changes to satisfy linter without behavior changes."
		if strings.HasSuffix(f, ".yml") || strings.HasSuffix(f, ".yaml") {
			instr = "Adjust linter configuration minimally to align with project conventions."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix lint/style issues",
			Instructions:       instr,
			ScopeJustification: "Improves consistency and maintainability.",
		})
	}
	return plan
}

// PerformanceOptimizeHotPathPlaybook targets specific performance issues.
type PerformanceOptimizeHotPathPlaybook struct{}

func (p PerformanceOptimizeHotPathPlaybook) Name() string { return "perf_opt_hotpath" }
func (p PerformanceOptimizeHotPathPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"optimize", "performance", "speed up", "slow", "latency", "cpu", "memory", "pprof", "allocations"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p PerformanceOptimizeHotPathPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Make targeted, measurable optimizations to hot paths without altering external behavior."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findPerfHotCandidates(userIntent)...)
		if len(candidates) == 0 {
			candidates = append(candidates, listGoFilesBySizeDesc(20)...) // heuristic fallback
		}
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply small performance optimization",
			Instructions:       "Reduce allocations and unnecessary work; simplify tight loops; avoid repeated computations; keep behavior identical. Document tradeoffs.",
			ScopeJustification: "Improves performance with localized change.",
		})
	}
	return plan
}

// SecurityAuditFixPlaybook addresses simple insecure patterns.
type SecurityAuditFixPlaybook struct{}

func (p SecurityAuditFixPlaybook) Name() string { return "security_fix" }
func (p SecurityAuditFixPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"security", "sanitize", "credential", "token", "password", "secret", "xss", "injection", "csrf", "insecure"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	// Specific weak crypto / http patterns
	if strings.Contains(lo, "md5") || strings.Contains(lo, "sha1") || strings.Contains(lo, "http://") {
		return true
	}
	return false
}
func (p SecurityAuditFixPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Eliminate common insecure patterns with minimal behavior impact."}
	candidates := make([]string, 0, len(estimatedFiles)+64)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findSecurityRiskCandidates()...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix simple security issue",
			Instructions:       "Replace insecure constructs (e.g., md5/sha1, http://, InsecureSkipVerify) with safer alternatives; avoid logging secrets; add validation. Keep change minimal.",
			ScopeJustification: "Reduces security risk safely.",
		})
	}
	return plan
}

// ConfigChangePlaybook updates configuration defaults and wiring.
type ConfigChangePlaybook struct{}

func (p ConfigChangePlaybook) Name() string { return "config_change" }
func (p ConfigChangePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	return strings.Contains(lo, "config") || strings.Contains(lo, "configuration") || strings.Contains(lo, "default value")
}
func (p ConfigChangePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Modify configuration values or wiring with safe defaults and clear docs."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update configuration usage",
			Instructions:       "Adjust defaults and plumb config through callers carefully. Update related docs/comments.",
			ScopeJustification: "Keeps configuration accurate and clear.",
		})
	}
	return plan
}

// CIUpdatePlaybook updates CI workflows/pipelines.
type CIUpdatePlaybook struct{}

func (p CIUpdatePlaybook) Name() string { return "ci_update" }
func (p CIUpdatePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"ci", "pipeline", "workflow", "github actions", "actions", "gha", "travis", "circleci", "jenkins"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p CIUpdatePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Adjust CI workflows for reliability, speed, or policy changes."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, listCIConfigFiles(40)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update CI workflow step(s)",
			Instructions:       "Apply minimal, well-documented changes to CI steps (caching, setup-go, matrix, permissions). Keep compatibility with the project.",
			ScopeJustification: "Improves delivery pipeline.",
		})
	}
	return plan
}

// LoggingImprovePlaybook improves logging and observability signals.
type LoggingImprovePlaybook struct{}

func (p LoggingImprovePlaybook) Name() string { return "logging_improve" }
func (p LoggingImprovePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	return strings.Contains(lo, "log") || strings.Contains(lo, "logging") || strings.Contains(lo, "observability")
}
func (p LoggingImprovePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Improve logging clarity, levels, and contextual detail without noisy output."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Improve logging in this file",
			Instructions:       "Add or refine logs at meaningful boundaries; choose appropriate levels; avoid sensitive data.",
			ScopeJustification: "Better diagnostics with minimal risk.",
		})
	}
	return plan
}
