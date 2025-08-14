package playbooks

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// HeaderSummaryPlaybook inserts or updates concise header summaries in source files.
type HeaderSummaryPlaybook struct{}

func (p HeaderSummaryPlaybook) Name() string { return "header_summary" }
func (p HeaderSummaryPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "docs" {
		return strings.Contains(lo, "header") || strings.Contains(lo, "summary") || strings.Contains(lo, "comment")
	}
	return strings.Contains(lo, "header summary") || strings.Contains(lo, "file header") || strings.Contains(lo, "top comment")
}
func (p HeaderSummaryPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Insert or refresh concise header summaries at the top of relevant files."}
	candidates := make([]string, 0, len(estimatedFiles))
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findFilesNeedingHeaderComments()...)
	}
	plan.Files = append(plan.Files, candidates...)
	for _, f := range candidates {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Add or update a short header summary comment",
			Instructions:       "If the file lacks a header comment, insert one summarizing the file's purpose in 1-2 sentences. If present, refresh for accuracy. Keep it concise.",
			ScopeJustification: "Improves readability with minimal change.",
		})
	}
	return plan
}

// SimpleReplacePlaybook handles prompts like: change 'old' to 'new'.
type SimpleReplacePlaybook struct{}

func (p SimpleReplacePlaybook) Name() string { return "simple_replace" }
func (p SimpleReplacePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	return strings.Contains(lo, "change '") || strings.Contains(lo, "replace '") || strings.Contains(lo, "s/")
}
func (p SimpleReplacePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply deterministic small textual replacements across target files."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply requested small textual replacement",
			Instructions:       "Perform the exact replacement requested, ensuring only intended occurrences are changed. Keep diffs minimal.",
			ScopeJustification: "Small deterministic edit.",
		})
	}
	return plan
}

// BugFixCompilationPlaybook targets compilation/build errors.
type BugFixCompilationPlaybook struct{}

func (p BugFixCompilationPlaybook) Name() string { return "fix_build" }
func (p BugFixCompilationPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "fix" {
		return true
	}
	// Common compiler/build error phrases across ecosystems
	phrases := []string{
		"compile error", "build error", "does not compile", "undefined:", "cannot find",
		"type mismatch", "no field", "missing return", "unexpected ", "syntax error",
		"import cycle", "duplicate symbol", "linker error", "cannot use ", "unresolved",
	}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p BugFixCompilationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Identify and correct compilation failures by adjusting imports, types, or simple logic errors."}
	candidates := make([]string, 0, len(estimatedFiles))
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findBuildErrorCandidateFiles(userIntent)...)
	}
	plan.Files = append(plan.Files, candidates...)
	for _, f := range candidates {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix compile issues in this file",
			Instructions:       "Resolve missing imports, type mismatches, or trivial syntax issues causing build failures. Do not refactor broadly.",
			ScopeJustification: "Unblocks build with minimal edits.",
		})
	}
	return plan
}

// TestFailureFixPlaybook targets failing unit/integration tests.
type TestFailureFixPlaybook struct{}

func (p TestFailureFixPlaybook) Name() string { return "fix_tests" }
func (p TestFailureFixPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "test" {
		return true
	}
	phrases := []string{
		"failing test", "fix tests", "test failing", "assertion failed", "expected", "got:",
		"--- fail:", "--- FAIL:", "panic: ", "race detected", "timeout in test",
	}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p TestFailureFixPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Identify failing tests and implement minimal code changes to make them pass."}
	candidates := make([]string, 0, len(estimatedFiles))
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findFailingTestCandidateFiles(userIntent)...)
	}
	plan.Files = append(plan.Files, candidates...)
	for _, f := range candidates {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix behavior to satisfy tests",
			Instructions:       "Understand expected behavior from tests and minimally adjust code to pass. Avoid broad design changes.",
			ScopeJustification: "Restores test health with focused changes.",
		})
	}
	return plan
}

// AddUnitTestsPlaybook adds or extends tests where missing or insufficient.
type AddUnitTestsPlaybook struct{}

func (p AddUnitTestsPlaybook) Name() string { return "add_tests" }
func (p AddUnitTestsPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "test" {
		return true
	}
	phrases := []string{"add test", "write test", "increase coverage", "tests for", "unit test", "integration test"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	// e.g., mentions of TestFoo or _test.go
	if regexp.MustCompile(`\bTest[A-Za-z0-9_]+\b`).FindString(lo) != "" || strings.Contains(lo, "_test.go") {
		return true
	}
	return false
}
func (p AddUnitTestsPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Create concise, reliable unit tests that capture expected behavior."}
	candidates := make([]string, 0, len(estimatedFiles)+64)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		// Prefer adding tests where none exist yet
		candidates = append(candidates, findSourceFilesWithoutTests(60)...)
		// Also include files explicitly mentioned by path or symbol
		explicit := extractFilePathsFromIntent(userIntent, 10)
		candidates = append(candidates, explicit...)
		if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
			candidates = append(candidates, findFilesWithSymbol(sym)...)
		}
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Add or extend unit tests",
			Instructions:       "Add tests for critical and edge cases. Keep tests deterministic and focused. Prefer table-driven tests where suitable.",
			ScopeJustification: "Improves robustness via testing.",
		})
	}
	return plan
}

// RefactorSmallPlaybook covers small, mechanical refactors.
type RefactorSmallPlaybook struct{}

func (p RefactorSmallPlaybook) Name() string { return "refactor_small" }
func (p RefactorSmallPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"refactor", "rename", "extract function", "extract method", "inline variable", "simplify", "dead code"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p RefactorSmallPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply small, safe refactors without changing behavior."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)

	// Try to target by symbol if mentioned
	if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
		candidates = append(candidates, findFilesWithSymbol(sym)...)
	}
	if len(candidates) == 0 {
		// Fallback: consider larger source files first, where refactors usually help readability
		candidates = append(candidates, listGoFilesBySizeDesc(30)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Perform small mechanical refactor",
			Instructions:       "Apply renames, extractions, or formatting that do not alter behavior. Keep diffs clear and localized.",
			ScopeJustification: "Improves clarity with minimal risk.",
		})
	}
	return plan
}

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

// APIChangePropagationPlaybook updates call sites after API/signature changes.
type APIChangePropagationPlaybook struct{}

func (p APIChangePropagationPlaybook) Name() string { return "api_change_propagation" }
func (p APIChangePropagationPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"api change", "signature change", "rename function", "rename method", "breaking change", "rename type"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	// Detect pattern "rename Old to New" or "Old -> New"
	if regexp.MustCompile(`\brename\s+[A-Za-z0-9_]+\s+to\s+[A-Za-z0-9_]+`).MatchString(lo) ||
		regexp.MustCompile(`\b[A-Za-z0-9_]+\s*->\s*[A-Za-z0-9_]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p APIChangePropagationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Propagate API/signature changes across call sites with mechanical edits."}

	oldName, newName := parseAPIRenameFromIntent(userIntent)
	candidates := make([]string, 0, len(estimatedFiles)+16)
	candidates = append(candidates, estimatedFiles...)
	if oldName != "" {
		candidates = append(candidates, findFilesWithSymbol(oldName)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)

	for _, f := range plan.Files {
		instr := "Update imports and call sites to match the new API shape. No behavioral changes."
		if oldName != "" && newName != "" {
			instr = "Rename symbol '" + oldName + "' to '" + newName + "' in references and definitions as needed. Ensure consistency and successful build."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Adjust call sites for API change",
			Instructions:       instr,
			ScopeJustification: "Keeps code compiling after API updates.",
		})
	}
	return plan
}

// Dependency/API helpers

func parseGoModuleFromIntent(intent string) (module string, version string) {
	// Try to extract patterns like: module@v1.2.3 or mention in quotes
	lo := intent
	re := regexp.MustCompile(`([a-zA-Z0-9_./\-]+)@([vV]?\d+\.[\d]+(\.[\d]+)?)`)
	if m := re.FindStringSubmatch(lo); len(m) >= 3 {
		return m[1], m[2]
	}
	// Try go get module or upgrade module mentions
	re2 := regexp.MustCompile(`go\s+get\s+([a-zA-Z0-9_./\-]+)(?:@([vV]?\d+\.[\d]+(\.[\d]+)?))?`)
	if m := re2.FindStringSubmatch(lo); len(m) >= 2 {
		mod := m[1]
		ver := ""
		if len(m) >= 3 {
			ver = m[2]
		}
		return mod, ver
	}
	return "", ""
}

func findFilesImportingModule(module string) []string {
	var results []string
	if module == "" {
		return results
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), module) {
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
}

func parseAPIRenameFromIntent(intent string) (oldName, newName string) {
	// Handle "rename Old to New"
	re := regexp.MustCompile(`(?i)rename\s+([A-Za-z0-9_]+)\s+to\s+([A-Za-z0-9_]+)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	// Handle "Old -> New"
	re2 := regexp.MustCompile(`\b([A-Za-z0-9_]+)\s*->\s*([A-Za-z0-9_]+)\b`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func findFilesWithSymbol(symbol string) []string {
	var results []string
	if symbol == "" {
		return results
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(b)
		if strings.Contains(content, symbol) {
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
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

// CodeCommentSyncPlaybook focuses on comment/documentation within code.
type CodeCommentSyncPlaybook struct{}

func (p CodeCommentSyncPlaybook) Name() string { return "code_comment_sync" }
func (p CodeCommentSyncPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"comment only", "update comments", "comment clarity", "docstring", "docs in code", "outdated comments", "clarify comments"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p CodeCommentSyncPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Align code comments with actual behavior; no logic changes."}
	candidates := make([]string, 0, len(estimatedFiles)+50)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findCommentHeavyFiles(40)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update comments for accuracy",
			Instructions:       "Review code and ensure comments accurately reflect behavior and constraints. Do not change logic. Prefer concise, high-signal comments.",
			ScopeJustification: "Improves clarity with zero behavior change.",
		})
	}
	return plan
}

// FileRenamePlaybook handles file moves/renames with import/call-site updates.
type FileRenamePlaybook struct{}

func (p FileRenamePlaybook) Name() string { return "file_rename" }
func (p FileRenamePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	if strings.Contains(lo, "rename file") || strings.Contains(lo, "move file") || strings.Contains(lo, "relocate") || strings.Contains(lo, "mv ") {
		return true
	}
	// Generic rename with path-like tokens
	if regexp.MustCompile(`(?i)rename\s+[A-Za-z0-9_./\-]+\s+(to|->)\s+[A-Za-z0-9_./\-]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p FileRenamePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Perform file rename/move and adjust minimal references accordingly."}
	from, to := parseFileRenameFromIntent(userIntent)
	candidates := make([]string, 0, len(estimatedFiles)+16)
	candidates = append(candidates, estimatedFiles...)
	if from != "" {
		candidates = append(candidates, from)
		// Find files that mention the old file path or base name
		base := filepath.Base(from)
		mentionRefs := findFilesMentioningString(base, 50)
		candidates = append(candidates, mentionRefs...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		instr := "Move/rename file as requested and update imports/references. Keep changes minimal and compiling."
		if f == from && to != "" {
			instr = "Rename/move this file to '" + to + "' and update any build tags or package references as needed."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Rename or move file and update references",
			Instructions:       instr,
			ScopeJustification: "Maintains integrity after file relocation.",
		})
	}
	return plan
}

// MultiFileEditPlaybook coordinates multiple related edits.
type MultiFileEditPlaybook struct{}

func (p MultiFileEditPlaybook) Name() string { return "multi_file_edit" }
func (p MultiFileEditPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	if strings.Contains(lo, "multiple files") || strings.Contains(lo, "several files") || strings.Contains(lo, "across files") {
		return true
	}
	// If the intent includes more than one file path, treat as multi-file
	fps := extractFilePathsFromIntent(userIntent, 3)
	return len(fps) >= 2
}
func (p MultiFileEditPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply coordinated edits across related files while keeping each change minimal."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	// Add files explicitly mentioned in the intent
	explicit := extractFilePathsFromIntent(userIntent, 20)
	candidates = append(candidates, explicit...)
	// If still narrow, add symbol-targeted files
	if len(candidates) < 2 {
		if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
			candidates = append(candidates, findFilesWithSymbol(sym)...)
		}
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply coordinated change",
			Instructions:       "Implement the requested change in this file, ensuring consistency with other touched files.",
			ScopeJustification: "Keeps cross-file changes aligned.",
		})
	}
	return plan
}

// TypesMigrationPlaybook migrates to new types/interfaces with mechanical changes.
type TypesMigrationPlaybook struct{}

func (p TypesMigrationPlaybook) Name() string { return "types_migration" }
func (p TypesMigrationPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"migrate type", "replace type", "interface change", "rename struct", "rename type", "rename interface", "field rename"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	if regexp.MustCompile(`(?i)rename\s+[A-Za-z0-9_]+\s+to\s+[A-Za-z0-9_]+`).MatchString(lo) ||
		regexp.MustCompile(`\b[A-Za-z0-9_]+\s*->\s*[A-Za-z0-9_]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p TypesMigrationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Mechanically migrate to new types or interfaces across the codebase."}
	oldType, newType := parseTypeRenameFromIntent(userIntent)
	oldField, newField := parseFieldRenameFromIntent(userIntent)

	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	if oldType != "" {
		candidates = append(candidates, findFilesWithSymbol(oldType)...)
	}
	if oldField != "" {
		candidates = append(candidates, findFilesWithSymbol(oldField)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)

	for _, f := range plan.Files {
		instr := "Adjust type names/usages and imports. Keep behavior unchanged."
		if oldType != "" && newType != "" {
			instr = "Rename type '" + oldType + "' to '" + newType + "' and update references, constructors, and method receivers if applicable."
		}
		if oldField != "" && newField != "" {
			instr += " Also rename field '" + oldField + "' to '" + newField + "' in struct literals and selectors."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update references for new types/interfaces",
			Instructions:       instr,
			ScopeJustification: "Ensures consistency after type or field migration.",
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

// Helpers

// findBuildErrorCandidateFiles attempts to extract file paths and symbols from the error text and locate likely files.
func findBuildErrorCandidateFiles(userIntent string) []string {
	var results []string
	lo := userIntent

	// 1) Direct file references like path/to/file.go:123
	goFileRe := regexp.MustCompile(`[A-Za-z0-9_./\\-]+\.go`)
	for _, m := range goFileRe.FindAllString(lo, -1) {
		results = append(results, filepath.ToSlash(m))
	}

	// 2) Symbols after "undefined:" or similar
	undefRe := regexp.MustCompile(`undefined:\s*([A-Za-z0-9_]+)`) // undefined: Symbol
	symbols := map[string]struct{}{}
	for _, m := range undefRe.FindAllStringSubmatch(lo, -1) {
		if len(m) > 1 {
			symbols[m[1]] = struct{}{}
		}
	}

	if len(symbols) > 0 {
		_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// Skip vendor and hidden dirs for speed
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(b)
			for sym := range symbols {
				// look for uses or definitions
				if strings.Contains(content, " "+sym+"(") || strings.Contains(content, "type "+sym+" ") || strings.Contains(content, "var "+sym+" ") || strings.Contains(content, "const "+sym+" ") {
					results = append(results, filepath.ToSlash(path))
					break
				}
			}
			return nil
		})
	}

	// 3) If nothing found, include common entry points
	if len(results) == 0 {
		// Prioritize Go module and main entry
		for _, f := range []string{"go.mod", "main.go"} {
			if _, err := os.Stat(f); err == nil {
				results = append(results, f)
			}
		}
	}

	return dedupeStrings(results)
}

// findFailingTestCandidateFiles tries to identify failing test files and related sources from the error output or intent.
func findFailingTestCandidateFiles(userIntent string) []string {
	var results []string

	// Direct file references
	goFileRe := regexp.MustCompile(`[A-Za-z0-9_./\\-]+(_test)?\.go`)
	for _, m := range goFileRe.FindAllString(userIntent, -1) {
		results = append(results, filepath.ToSlash(m))
		if strings.HasSuffix(m, "_test.go") {
			src := strings.TrimSuffix(m, "_test.go") + ".go"
			if _, err := os.Stat(src); err == nil {
				results = append(results, filepath.ToSlash(src))
			}
		}
	}

	// Test name like TestSomething
	testNameRe := regexp.MustCompile(`\bTest[A-Za-z0-9_]+\b`)
	names := testNameRe.FindAllString(userIntent, -1)
	if len(names) > 0 {
		_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(b)
			for _, tn := range names {
				if strings.Contains(content, "func "+tn+"(") {
					results = append(results, filepath.ToSlash(path))
					src := strings.TrimSuffix(path, "_test.go") + ".go"
					if _, err := os.Stat(src); err == nil {
						results = append(results, filepath.ToSlash(src))
					}
					break
				}
			}
			return nil
		})
	}

	// If still empty, include all *_test.go at top level dirs for triage
	if len(results) == 0 {
		_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				results = append(results, filepath.ToSlash(path))
			}
			return nil
		})
	}

	return dedupeStrings(results)
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// Utility helpers for discovery
func findGoFilesLimited(limit int) []string {
	var files []string
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, filepath.ToSlash(path))
			if len(files) >= limit {
				return errors.New("limit reached")
			}
		}
		return nil
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

func listGoFilesBySizeDesc(limit int) []string {
	type fsz struct {
		p string
		s int64
	}
	var arr []fsz
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			if info, err := os.Stat(path); err == nil {
				arr = append(arr, fsz{p: filepath.ToSlash(path), s: info.Size()})
			}
		}
		return nil
	})
	sort.Slice(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	if len(arr) > limit {
		arr = arr[:limit]
	}
	res := make([]string, 0, len(arr))
	for _, a := range arr {
		res = append(res, a.p)
	}
	return res
}

func parseSymbolNameFromIntent(intent string) string {
	// Look for simple symbol name patterns after common verbs
	re := regexp.MustCompile(`(?i)(rename|refactor|extract)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[2]
	}
	return ""
}

// File path helpers
func extractFilePathsFromIntent(intent string, limit int) []string {
	var res []string
	// Match path-like tokens with extensions
	re := regexp.MustCompile(`([A-Za-z0-9_./\\\-]+\.[A-Za-z0-9_]+)`) // simple
	for _, m := range re.FindAllString(intent, -1) {
		res = append(res, filepath.ToSlash(m))
		if len(res) >= limit {
			break
		}
	}
	return dedupeStrings(res)
}

func parseFileRenameFromIntent(intent string) (from string, to string) {
	// Patterns: rename a/b.go to c/d.go  | a/b.go -> c/d.go  | mv a/b.go c/d.go
	re1 := regexp.MustCompile(`(?i)rename\s+([A-Za-z0-9_./\\\-]+)\s+to\s+([A-Za-z0-9_./\\\-]+)`) // rename X to Y
	if m := re1.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	re2 := regexp.MustCompile(`([A-Za-z0-9_./\\\-]+)\s*->\s*([A-Za-z0-9_./\\\-]+)`) // X -> Y
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	re3 := regexp.MustCompile(`(?i)\bmv\s+([A-Za-z0-9_./\\\-]+)\s+([A-Za-z0-9_./\\\-]+)`) // mv X Y
	if m := re3.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	return "", ""
}

func findFilesMentioningString(substr string, limit int) []string {
	var results []string
	if substr == "" {
		return results
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), substr) {
			results = append(results, filepath.ToSlash(path))
			if len(results) >= limit {
				return errors.New("limit")
			}
		}
		return nil
	})
	return dedupeStrings(results)
}

// findFilesNeedingHeaderComments scans for Go source files that do not start with a comment block.
func findFilesNeedingHeaderComments() []string {
	var results []string
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if startsWithGoComment(path) {
			return nil
		}
		results = append(results, filepath.ToSlash(path))
		return nil
	})
	return dedupeStrings(results)
}

func startsWithGoComment(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(b)
	lines := strings.Split(content, "\n")
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
			return true
		}
		return false
	}
	return false
}

// perf/security candidate discovery
func findPerfHotCandidates(userIntent string) []string {
	var results []string
	// If the intent mentions a symbol, try finding it
	if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
		results = append(results, findFilesWithSymbol(sym)...)
	}
	// Heuristics: look for hotspots (many appends/allocations/tight loops)
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(b)
		score := 0
		// simple scoring
		score += strings.Count(s, "append(")
		score += strings.Count(s, "make(")
		score += strings.Count(s, "for ")
		if strings.Contains(s, "pprof") {
			score += 5
		}
		if score >= 8 { // heuristic threshold
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
}

func findSecurityRiskCandidates() []string {
	var results []string
	weakRe := []*regexp.Regexp{
		regexp.MustCompile(`\bmd5\.`),
		regexp.MustCompile(`\bsha1\.`),
		regexp.MustCompile(`http://`),
		regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),
		regexp.MustCompile(`(?i)password`),
		regexp.MustCompile(`(?i)secret`),
		regexp.MustCompile(`(?i)token`),
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(b)
		for _, re := range weakRe {
			if re.FindStringIndex(s) != nil {
				results = append(results, filepath.ToSlash(path))
				break
			}
		}
		return nil
	})
	return dedupeStrings(results)
}

// Test/CI helpers
func findSourceFilesWithoutTests(limit int) []string {
	var results []string
	hasTest := map[string]bool{}
	// First record test files
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			base := strings.TrimSuffix(filepath.ToSlash(path), "_test.go") + ".go"
			hasTest[base] = true
		}
		return nil
	})
	// Now find source files without tests
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			p := filepath.ToSlash(path)
			if !hasTest[p] {
				results = append(results, p)
				if len(results) >= limit {
					return errors.New("limit")
				}
			}
		}
		return nil
	})
	return dedupeStrings(results)
}

func listCIConfigFiles(limit int) []string {
	var res []string
	candidates := []string{
		".github/workflows", ".github/workflows/ci.yml", ".github/workflows/test.yml",
		".travis.yml", ".circleci/config.yml", "Jenkinsfile", "azure-pipelines.yml",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil {
			if st.IsDir() {
				// List yml files inside
				_ = filepath.WalkDir(c, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
						res = append(res, filepath.ToSlash(path))
						if len(res) >= limit {
							return errors.New("limit")
						}
					}
					return nil
				})
			} else {
				res = append(res, filepath.ToSlash(c))
			}
		}
		if len(res) >= limit {
			break
		}
	}
	return dedupeStrings(res)
}

// Type/field rename parsing
func parseTypeRenameFromIntent(intent string) (oldType, newType string) {
	re := regexp.MustCompile(`(?i)(?:type|struct|interface)?\s*rename\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	re2 := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*->\s*([A-Za-z_][A-Za-z0-9_]*)\b`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func parseFieldRenameFromIntent(intent string) (oldField, newField string) {
	re := regexp.MustCompile(`(?i)field\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	re2 := regexp.MustCompile(`(?i)rename\s+field\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

// Find files with dense comments to sync
func findCommentHeavyFiles(limit int) []string {
	type entry struct {
		p     string
		ratio float64
	}
	var arr []entry
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) == 0 {
			return nil
		}
		comment := 0
		code := 0
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "* ") {
				comment++
			} else {
				code++
			}
		}
		total := comment + code
		if total == 0 {
			return nil
		}
		ratio := float64(comment) / float64(total)
		if ratio >= 0.20 { // at least 20% comments
			arr = append(arr, entry{p: filepath.ToSlash(path), ratio: ratio})
		}
		return nil
	})
	sort.Slice(arr, func(i, j int) bool { return arr[i].ratio > arr[j].ratio })
	if len(arr) > limit {
		arr = arr[:limit]
	}
	res := make([]string, 0, len(arr))
	for _, e := range arr {
		res = append(res, e.p)
	}
	return res
}
