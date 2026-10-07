package health

import (
	"context"
	"fmt"
	"strings"
)

// Config tunes a health scan. The zero value uses the package defaults.
type Config struct {
	// MaxFileLines is the line count at which a file is reported too long.
	// Zero uses DefaultMaxFileLines.
	MaxFileLines int
	// MaxComplexity is the branching proxy at which a function is reported
	// too complex. Zero uses DefaultMaxComplexity.
	MaxComplexity int
	// MaxFiles bounds how many source files the scan reads. Zero uses
	// DefaultMaxFiles.
	MaxFiles int
	// Checks runs the project's build/test checks. Nil skips the check
	// portion of the report (size/complexity signals only).
	Checks CheckRunner
	// NoChecks suppresses the build/test run even when Checks is set.
	NoChecks bool
	// Duplicates finds near-duplicate code units. Nil skips the duplication
	// portion; NoDuplicates suppresses it even when Duplicates is set.
	Duplicates DuplicateFinder
	// NoDuplicates suppresses the duplication scan even when Duplicates is set.
	NoDuplicates bool
	// DuplicateThreshold is the similarity at which two functions count as
	// near-duplicates. Zero uses DefaultDuplicateThreshold.
	DuplicateThreshold float64
	// Deps resolves the latest version of each dependency. Nil skips the
	// dependency portion; NoDeps suppresses it even when Deps is set.
	Deps DependencyChecker
	// NoDeps suppresses the dependency check even when Deps is set.
	NoDeps bool
}

func (c Config) maxFileLines() int {
	if c.MaxFileLines > 0 {
		return c.MaxFileLines
	}
	return DefaultMaxFileLines
}

func (c Config) maxComplexity() int {
	if c.MaxComplexity > 0 {
		return c.MaxComplexity
	}
	return DefaultMaxComplexity
}

func (c Config) maxFiles() int {
	if c.MaxFiles > 0 {
		return c.MaxFiles
	}
	return DefaultMaxFiles
}

func (c Config) duplicateThreshold() float64 {
	if c.DuplicateThreshold > 0 {
		return c.DuplicateThreshold
	}
	return DefaultDuplicateThreshold
}

// ValidateDuplicateThreshold reports an error when an explicitly supplied
// duplicate threshold is outside (0, 1]. A zero value means "unset" at the
// Config level (like the other thresholds) and is accepted there; the command
// layer rejects a user-supplied 0 because a threshold of 0 would report every
// pair and is almost certainly a mistake.
func ValidateDuplicateThreshold(threshold float64) error {
	if threshold > 1 {
		return fmt.Errorf("health: duplicate threshold must be in (0, 1], got %g", threshold)
	}
	return nil
}

// RejectZeroDuplicateThreshold is the command-layer guard: a threshold of
// exactly 0 that the user asked for would flood the report with every pair.
func RejectZeroDuplicateThreshold(threshold float64) error {
	if threshold == 0 {
		return fmt.Errorf("health: duplicate threshold must be in (0, 1], got 0")
	}
	return nil
}

// Build scans the project rooted at root and returns a health report: size and
// complexity signals, the failing build/test checks, and one proposed,
// separately approvable fix per finding. It runs no model and never applies a
// fix — it only reports. A verification setup error is recorded on the report
// (not returned) so a scan of a project with no build/test commands still
// reports its size/complexity signals.
func Build(ctx context.Context, root string, cfg Config) (*Report, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("health: project root is required")
	}
	if err := ValidateDuplicateThreshold(cfg.DuplicateThreshold); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	report := &Report{Root: root, Findings: []Finding{}}

	scan, err := ScanSources(root, cfg.maxFiles())
	if err != nil {
		return nil, err
	}
	report.FilesScanned = len(scan.Files)
	report.FilesSkipped = scan.Skipped

	report.Findings = append(report.Findings, sizeFindings(scan.Files, cfg.maxFileLines())...)
	report.Findings = append(report.Findings, complexityFindings(scan.Functions, cfg.maxComplexity())...)

	if cfg.Duplicates != nil && !cfg.NoDuplicates {
		matches, dupErr := cfg.Duplicates.FindDuplicates(ctx, root, cfg.duplicateThreshold())
		if dupErr != nil {
			report.Errors = append(report.Errors, "duplicates: "+dupErr.Error())
		} else {
			report.Findings = append(report.Findings, duplicateFindings(matches)...)
		}
	}

	if cfg.Deps != nil && !cfg.NoDeps {
		reqs, reqErr := goModRequires(root)
		if reqErr != nil {
			report.Errors = append(report.Errors, "dependencies: "+reqErr.Error())
		} else {
			outdated, depErr := FindOutdated(ctx, reqs, cfg.Deps, root)
			if depErr != nil {
				report.Errors = append(report.Errors, "dependencies: "+depErr.Error())
			} else {
				report.Findings = append(report.Findings, outdatedFindings(outdated)...)
			}
		}
	}

	if cfg.Checks != nil && !cfg.NoChecks {
		result, runErr := cfg.Checks.Run(ctx, root)
		if runErr != nil {
			report.Errors = append(report.Errors, "verification: "+runErr.Error())
		} else {
			report.Findings = append(report.Findings, checkFindings(result)...)
			report.Checks = result.Summary()
			report.Errors = append(report.Errors, result.Errors...)
		}
	}

	sortFindings(report.Findings)
	return report, nil
}

// SizeThresholdExceeded reports whether a file of n lines is over a max of
// maxLines.
func SizeThresholdExceeded(n, maxLines int) bool { return maxLines > 0 && n > maxLines }

// ComplexityThresholdExceeded reports whether a function of complexity c is
// over a max of maxComplexity.
func ComplexityThresholdExceeded(c, maxComplexity int) bool {
	return maxComplexity > 0 && c > maxComplexity
}

// sizeFindings reports every file over maxLines with a proposed split.
func sizeFindings(files []SourceStats, maxLines int) []Finding {
	var out []Finding
	for _, f := range files {
		if !SizeThresholdExceeded(f.Lines, maxLines) {
			continue
		}
		out = append(out, Finding{
			Kind:     KindFileSize,
			Severity: SeverityFix,
			Target:   f.File,
			Message:  fmt.Sprintf("%s has %d lines (exceeds %d)", f.File, f.Lines, maxLines),
			Fix: &Fix{
				Summary: fmt.Sprintf("split %s: %d lines, exceeds %d", f.File, f.Lines, maxLines),
				Detail:  "Divide the file along its natural seams (types vs. behaviour, or per-feature files) and move each part into its own file, keeping the package's exported surface unchanged.",
			},
		})
	}
	return out
}

// complexityFindings reports every function over maxComplexity with a proposed
// extraction. Functions in the same file that exceed the threshold are grouped
// into one finding so a single file with several hot spots reads as one fix.
func complexityFindings(functions []FunctionComplexity, maxComplexity int) []Finding {
	byFile := map[string][]FunctionComplexity{}
	var order []string
	for _, fn := range functions {
		if !ComplexityThresholdExceeded(fn.Complexity, maxComplexity) {
			continue
		}
		if _, ok := byFile[fn.File]; !ok {
			order = append(order, fn.File)
		}
		byFile[fn.File] = append(byFile[fn.File], fn)
	}

	var out []Finding
	for _, file := range order {
		fns := byFile[file]
		names := make([]string, 0, len(fns))
		for _, fn := range fns {
			names = append(names, fmt.Sprintf("%s:%d (complexity %d)", fn.Name, fn.Line, fn.Complexity))
		}
		message := fmt.Sprintf("%s: %d function(s) exceed complexity %d: %s",
			file, len(fns), maxComplexity, strings.Join(names, ", "))
		target := file
		if len(fns) == 1 {
			target = JoinTarget(file, fns[0].Name)
		}
		out = append(out, Finding{
			Kind:     KindComplexity,
			Severity: SeverityWarn,
			Target:   target,
			Message:  message,
			Fix: &Fix{
				Summary: fmt.Sprintf("extract helper(s) from %s", strings.Join(fnNames(fns), ", ")),
				Detail:  "Pull each long branch chain into a named helper (or a table-driven dispatch) so the top-level function reads as a sequence of steps.",
			},
		})
	}
	return out
}

func fnNames(fns []FunctionComplexity) []string {
	names := make([]string, 0, len(fns))
	for _, fn := range fns {
		names = append(names, fn.Name)
	}
	return names
}
