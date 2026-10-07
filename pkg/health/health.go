// Package health implements the on-demand project health check behind
// `sprout health`: it reports size and complexity signals for a project's
// source tree, near-duplicate functions, dependencies with newer versions
// available, and the project's failing build/test checks, and turns each
// concern into a finding that carries a small, separately approvable fix.
//
// The package is pure data and stdlib: it walks the tree itself, reuses the
// verification runner for the build/test checks, and never runs a model. The
// duplication and dependency checks run behind injectable seams
// (DuplicateFinder, DependencyChecker), so the command uses a live
// implementation while tests inject fixture data with no network. A caller
// (the CLI, or a scheduled job) can present the findings one at a time and
// apply the ones a human approves; nothing here is auto-applied.
package health

import (
	"sort"
	"strings"
)

// Severity ranks a finding so a caller can order or filter them.
type Severity string

const (
	// SeverityWarn is a concern worth addressing but not urgent.
	SeverityWarn Severity = "warn"
	// SeverityFix is a concern the project should address soon (a failing
	// check, an over-long file, an over-complex function).
	SeverityFix Severity = "fix"
)

// FindingKind names what a finding measures.
type FindingKind string

const (
	// KindFileSize is a source file whose length exceeds the threshold.
	KindFileSize FindingKind = "file-size"
	// KindComplexity is a function whose branching complexity proxy exceeds
	// the threshold.
	KindComplexity FindingKind = "complexity"
	// KindCheck is a project build/test check that failed or errored.
	KindCheck FindingKind = "check"
	// KindDuplication is a pair of near-identical code units.
	KindDuplication FindingKind = "duplication"
	// KindOutdatedDep is a dependency with a newer version available.
	KindOutdatedDep FindingKind = "outdated-dependency"
)

// Fix is a small, separately approvable remediation. Applying it is a human
// decision: the health check only ever proposes it.
type Fix struct {
	// Summary is a short imperative description of the change, e.g.
	// "split file pkg/x/big.go (1200 lines, exceeds 1000)".
	Summary string `json:"summary"`
	// Detail elaborates the proposal when the summary is not enough.
	Detail string `json:"detail,omitempty"`
}

// Finding is one health concern plus the fix proposed for it. Findings are
// independent: a caller can approve any subset, in any order.
type Finding struct {
	// Kind names what the finding measures.
	Kind FindingKind `json:"kind"`
	// Severity ranks the finding.
	Severity Severity `json:"severity"`
	// Target identifies what the finding is about: a workspace-relative
	// file path, a "file:function" location, or a check kind.
	Target string `json:"target"`
	// Message is a one-line description of the concern.
	Message string `json:"message"`
	// Fix is the proposed remediation, or nil when none applies.
	Fix *Fix `json:"fix,omitempty"`
}

// Report is the outcome of a health scan.
type Report struct {
	// Root is the absolute path the scan ran against.
	Root string `json:"root"`
	// FilesScanned is how many source files contributed size/complexity
	// signals.
	FilesScanned int `json:"files_scanned"`
	// FilesSkipped counts files visited but not analysed (too large, or
	// unreadable), so a truncated scan is visible rather than silent.
	FilesSkipped int `json:"files_skipped,omitempty"`
	// Findings are the concerns, ordered by severity (fix before warn) and
	// then by target, so the report is deterministic.
	Findings []Finding `json:"findings"`
	// Checks is the verification run's own summary line (build/test
	// pass/fail), empty when no check could be resolved.
	Checks string `json:"checks,omitempty"`
	// Errors are run-level findings (an unreadable tree, a verification
	// setup failure) reported rather than swallowed.
	Errors []string `json:"errors,omitempty"`
}

// Counts returns the number of findings per severity.
func (r *Report) Counts() (fix, warn int) {
	if r == nil {
		return 0, 0
	}
	for _, f := range r.Findings {
		if f.Severity == SeverityFix {
			fix++
		} else {
			warn++
		}
	}
	return fix, warn
}

// sortFindings orders findings deterministically: fix findings first, then by
// kind, target, and message, so two scans of the same tree agree.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Severity != b.Severity {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Message < b.Message
	})
}

func severityRank(s Severity) int {
	if s == SeverityFix {
		return 0
	}
	return 1
}

// JoinTarget joins a file path and a symbol name into a finding target.
func JoinTarget(file, symbol string) string {
	if symbol == "" {
		return file
	}
	return file + ":" + symbol
}

// TrimShort clips s to n runes, appending an ellipsis when it was longer.
// It keeps a finding message/price one-line and bounded.
func TrimShort(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	// Byte slicing can split a rune; back off to a rune boundary.
	cut := s[:n]
	for len(cut) > 0 && !isRuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, " \t\n") + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
