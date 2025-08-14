package playbooks

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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

// helpers for this file
func findBuildErrorCandidateFiles(userIntent string) []string {
	var results []string
	lo := userIntent

	// 1) Direct file references like path/to/file.go:123
	goFileRe := regexp.MustCompile(`[A-Za-z0-9_./\\\-]+\.go`)
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

func findFailingTestCandidateFiles(userIntent string) []string {
	var results []string

	// Direct file references
	goFileRe := regexp.MustCompile(`[A-Za-z0-9_./\\\-]+(_test)?\.go`)
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
