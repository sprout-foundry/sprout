// turn_application_code.go — application-code classification for the
// turn-end verification gate. The verification run gates on the turn's
// application-code changes: a turn that changed only documentation or
// plan bookkeeping must not start a build (editing a README kicking off
// a build+test run is both wrong and surprising). TurnChangedPaths
// stays the raw, unfiltered window for its other consumers; the gate
// reads the filtered window below.

package agent

import (
	"path/filepath"
	"strings"
)

// IsApplicationCodePath reports whether a changed workspace path counts
// as application code for the verification gate. It is a pure
// path-string predicate: it never touches the filesystem, and it
// classifies whatever form the caller passes (relative or absolute;
// separators normalized for the platform it runs on).
//
// A path is NOT application code when either of these holds:
//
//   - Documentation: a markdown file (a `.md` extension, any case), or
//     any file under a `docs` directory segment at any depth (`docs/`,
//     `foo/docs/`; the directory name compares case-insensitively). That
//     covers the obvious README.md, CHANGELOG.md, and docs/** cases
//     wherever they live. The rule matches the directory, not a file of
//     that name: a file literally named `docs` (a final path element
//     with no extension, e.g. `pkg/docs`) is application code. Other
//     prose extensions (.txt, .rst) are deliberately not filtered — they
//     may be fixtures or data.
//
//   - Sprout bookkeeping: a path with a `.sprout` path segment (the
//     plan/starter-manifest/session directory: `.sprout/plan.json`,
//     `.sprout/starter.json`, ...), at any depth — including a final
//     element named `.sprout` (a file of that name is bookkeeping too,
//     unlike the docs rule above, which matches directories only).
//     Segment equality only — `.sprout` as a substring of a segment
//     (`mysprout/`, `x.sprout/`) is an ordinary name and stays
//     application code.
//
// Everything else is application code: sources (.go, .ts, .py, .rs, ...),
// markup and styles (.html, .css), build configuration (go.mod,
// package.json, Makefile), data, and unknown extensions. The gate exists
// to skip obvious no-build turns, so an unfamiliar path must fail open
// (count as application code and run verification), never silently skip
// it.
func IsApplicationCodePath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	last := len(parts) - 1

	// A `.sprout` path segment — the bookkeeping directory, at any
	// depth.
	for _, seg := range parts {
		if seg == ".sprout" {
			return false
		}
	}

	// Anything under a `docs` directory segment. The final element is
	// the file name, so a file literally named `docs` is not matched.
	for i := 0; i < last; i++ {
		if strings.EqualFold(parts[i], "docs") {
			return false
		}
	}

	// Markdown anywhere is documentation.
	if strings.EqualFold(filepath.Ext(parts[last]), ".md") {
		return false
	}
	return true
}

// TurnChangedApplicationPaths returns the distinct workspace paths the
// current turn changed that count as application code
// (IsApplicationCodePath), in TurnChangedPaths' first-seen order — or
// nil when change tracking is off, the turn changed no files, or every
// changed path is documentation or .sprout bookkeeping. The turn-end
// verification gate and the not-verified reason read this filtered
// window, not the raw one: a docs-only or bookkeeping-only turn reports
// "no code changes" and never starts a build.
func (a *Agent) TurnChangedApplicationPaths() []string {
	paths := a.TurnChangedPaths()
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if IsApplicationCodePath(p) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
