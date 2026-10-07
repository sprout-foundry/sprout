// requiretest.go — the require-a-test-for-new-behavior check: when the
// verification run is asked to require a test (opt-in), a turn that added new
// behavior must either declare an active-plan acceptance item of kind test or
// add/change a test file; otherwise the run reports a failing check.
//
// The check is a sibling of the build/test checks: it reuses the Check shape,
// so its outcome flows through the same turn-end reporting and repair loop as
// every other check. It is deliberately mechanical — the classification of a
// changed path as a test file and of a turn as "new behavior" are pure
// predicate functions below, pinned by their own tests.

package verify

import (
	"path"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// RequireTestCheckKind is the check kind for the require-a-test-for-new-
// behavior check. It is not a plancontract acceptance kind (no plan item
// carries it): it is a verification-time requirement the run reports as a
// check, so its result flows through the same reporting and repair loop.
const RequireTestCheckKind plancontract.Kind = "require_test"

// RequireTestInput is the mechanical input the require-a-test-for-new-behavior
// check reads. The caller (the turn-end hook) supplies the turn's changed
// application-code paths and whether the active plan declares an acceptance
// item of kind test; the check never reads the filesystem or the plan file
// itself.
type RequireTestInput struct {
	// Enabled gates the check. Off is the default and a complete no-op: the
	// run's checks are unchanged when it is false.
	Enabled bool
	// PlanHasTestItem reports whether the active plan declares at least one
	// acceptance item of kind test. Such an item satisfies the requirement on
	// its own.
	PlanHasTestItem bool
	// ChangedApplicationPaths are the turn's changed paths that count as
	// application code (already filtered by the caller's documentation /
	// bookkeeping rule). An empty set means the turn changed no application
	// code — no new behavior — and the check is not applied.
	ChangedApplicationPaths []string
}

// RequireTestCheck returns the failing check a require-a-test run reports when
// the turn added new behavior and neither a test acceptance item exists nor a
// test file was added or changed, or nil when the requirement is satisfied,
// disabled, or not applicable (a turn that changed no application code is not
// new behavior and is never flagged).
//
// A satisfied or not-applicable requirement returns nil rather than a passing
// check: the check exists only as a gate, so a run that satisfies it adds no
// check and its pass/fail state is unchanged.
func RequireTestCheck(in RequireTestInput) *Check {
	if !in.Enabled {
		return nil
	}
	if in.PlanHasTestItem {
		return nil
	}
	if len(in.ChangedApplicationPaths) == 0 {
		return nil
	}
	if pathsIncludeTestFile(in.ChangedApplicationPaths) {
		return nil
	}
	return &Check{
		Kind:   RequireTestCheckKind,
		Items:  in.ChangedApplicationPaths,
		Passed: false,
		Reason: "new behavior without a test: add a test or an acceptance item of kind test",
	}
}

// pathsIncludeTestFile reports whether any changed path is a test file.
func pathsIncludeTestFile(paths []string) bool {
	for _, p := range paths {
		if IsTestFilePath(p) {
			return true
		}
	}
	return false
}

// IsTestFilePath reports whether a workspace path is a test file, using
// filename conventions rather than content inspection (so the check stays a
// pure path predicate). It recognizes the common Go, JavaScript/TypeScript,
// Python, Ruby, Rust, and Java conventions:
//
//   - a base name ending in "_test" or "_spec" with a known source
//     extension (`foo_test.go`, `foo_spec.rb`);
//   - a base name ending in ".test" or ".spec" with a known source
//     extension (`Button.test.tsx`, `Button.spec.ts`);
//   - a base named `test_<something>` with a known source extension
//     (`test_foo.py`);
//   - a base name of `test`, `tests`, `spec`, or `specs` with a known source
//     extension (`test.ts`);
//   - any file under a `test`, `tests`, `__tests__`, `spec`, or `specs`
//     directory at any depth (`test/unit/foo.ts`).
//
// The match is case-insensitive. A path that only contains the word test as a
// substring (`contest.go`, `latest.ts`) is not a test file.
func IsTestFilePath(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	slashed := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	segments := strings.Split(path.Clean(slashed), "/")
	if len(segments) == 0 {
		return false
	}
	// A test directory segment at any depth (the final element is the file
	// name, so a file literally named `test` is matched by the base-name rule
	// below, not this one).
	for _, seg := range segments[:len(segments)-1] {
		switch strings.ToLower(seg) {
		case "test", "tests", "__tests__", "spec", "specs":
			return true
		}
	}
	base := strings.ToLower(segments[len(segments)-1])
	if !hasKnownSourceExt(base) {
		return false
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case stem == "test" || stem == "tests" || stem == "spec" || stem == "specs":
		return true
	case strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, "_spec"):
		return true
	case strings.HasPrefix(stem, "test_") || strings.HasPrefix(stem, "spec_"):
		return true
	case strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec"):
		return true
	}
	return false
}

// knownSourceExts are the source extensions a name-convention test file may
// carry. Requiring a known extension keeps a data file that happens to be
// named like a test (`test.json`) from counting as a test.
var knownSourceExts = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true,
	".swift": true, ".c": true, ".cc": true, ".cpp": true, ".h": true,
	".mjs": true, ".cjs": true, ".mts": true, ".cts": true,
}

func hasKnownSourceExt(base string) bool {
	return knownSourceExts[path.Ext(base)]
}
