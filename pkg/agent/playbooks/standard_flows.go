package playbooks

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	return category == "test" || strings.Contains(lo, "add test") || strings.Contains(lo, "write test") || strings.Contains(lo, "increase coverage")
}
func (p AddUnitTestsPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Create concise, reliable unit tests that capture expected behavior."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Add or extend unit tests",
			Instructions:       "Add tests for critical paths and edge cases. Keep them deterministic and minimal.",
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
	return strings.Contains(lo, "refactor") || strings.Contains(lo, "rename") || strings.Contains(lo, "extract function")
}
func (p RefactorSmallPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply small, safe refactors without changing behavior."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Perform small mechanical refactor",
			Instructions:       "Apply renames, extractions, or reformatting that do not alter behavior. Keep diffs clear and localized.",
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
	return strings.Contains(lo, "update dependency") || strings.Contains(lo, "bump") || strings.Contains(lo, "upgrade package")
}
func (p DependencyUpdatePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Safely update a dependency and adapt minimal necessary code changes."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Adjust code for dependency update if needed",
			Instructions:       "Modify imports or small call-site changes required by the update. Avoid broad refactors.",
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
	return strings.Contains(lo, "lint") || strings.Contains(lo, "format") || strings.Contains(lo, "style fix")
}
func (p LintFixPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply linter/style fixes while preserving semantics."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix lint/style issues",
			Instructions:       "Perform minimal changes to satisfy linter without behavior changes.",
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
	return strings.Contains(lo, "optimize") || strings.Contains(lo, "performance") || strings.Contains(lo, "speed up")
}
func (p PerformanceOptimizeHotPathPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Make targeted, measurable optimizations to hot paths without altering external behavior."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply small performance optimization",
			Instructions:       "Use simpler algorithms, reduce allocations, or short-circuit conditions. Add comments describing the tradeoff.",
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
	return strings.Contains(lo, "security") || strings.Contains(lo, "sanitize") || strings.Contains(lo, "credential")
}
func (p SecurityAuditFixPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Eliminate common insecure patterns with minimal behavior impact."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Fix simple security issue",
			Instructions:       "Add input validation, avoid hardcoded secrets, and prefer safe APIs. Keep change minimal.",
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
	return strings.Contains(lo, "api change") || strings.Contains(lo, "signature change") || strings.Contains(lo, "rename function")
}
func (p APIChangePropagationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Propagate API/signature changes across call sites with mechanical edits."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Adjust call sites for API change",
			Instructions:       "Update imports and call sites to match the new API shape. No behavioral changes.",
			ScopeJustification: "Keeps code compiling after API updates.",
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
	return strings.Contains(lo, "ci") || strings.Contains(lo, "pipeline") || strings.Contains(lo, "workflow")
}
func (p CIUpdatePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Adjust CI workflows for reliability, speed, or policy changes."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update CI workflow step(s)",
			Instructions:       "Keep changes minimal and documented. Ensure compatibility with current project setup.",
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
	return strings.Contains(lo, "comment only") || strings.Contains(lo, "update comments") || strings.Contains(lo, "comment clarity")
}
func (p CodeCommentSyncPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Align code comments with actual behavior; no logic changes."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update comments for accuracy",
			Instructions:       "Review code and ensure comments are accurate and helpful. Avoid editing logic.",
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
	return strings.Contains(lo, "rename file") || strings.Contains(lo, "move file") || strings.Contains(lo, "relocate")
}
func (p FileRenamePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Perform file rename/move and adjust minimal references accordingly."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Rename or move file and update references",
			Instructions:       "Move/rename file as requested and update imports/references. Keep changes minimal and compiling.",
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
	return strings.Contains(lo, "multiple files") || strings.Contains(lo, "several files") || strings.Contains(lo, "across files")
}
func (p MultiFileEditPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply coordinated edits across related files while keeping each change minimal."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
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
	return strings.Contains(lo, "migrate type") || strings.Contains(lo, "replace type") || strings.Contains(lo, "interface change")
}
func (p TypesMigrationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Mechanically migrate to new types or interfaces across the codebase."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update references for new types/interfaces",
			Instructions:       "Adjust type names/usages and imports. Keep behavior unchanged.",
			ScopeJustification: "Ensures consistency after type migration.",
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
