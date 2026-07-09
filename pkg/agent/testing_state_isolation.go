package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/search"
)

// SetTestStateDirHook overrides the session state dir to the given
// path for the lifetime of the returned restore func. Lower-level
// primitive than NewTestStateDir(t) — useful from TestMain in test
// packages that don't yet have a *testing.T. Prefer NewTestStateDir
// inside individual tests; this is for package-wide isolation hooks.
//
// Returns a restore func that puts getStateDirFunc back to its prior
// value. Idempotent — calling restore more than once is a no-op.
func SetTestStateDirHook(dir string) func() {
	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return dir, nil }
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		getStateDirFunc = orig
	}
}

// SnapshotRealStateDir captures the current file set under the user's
// real ~/.sprout/sessions/ for later leak-detection. Use this from
// TestMain before installing SetTestStateDirHook; pair it with
// AssertNoStateLeak at the end of TestMain to fail loudly if any
// test bypassed the isolation hook and wrote to the real dir.
//
// Returns ("", nil) when the real dir can't be resolved (e.g. no HOME
// env in CI) — the post-snapshot call then degrades to a no-op rather
// than fabricating a false negative.
func SnapshotRealStateDir() (realDir string, before map[string]time.Time) {
	d, err := defaultGetStateDir()
	if err != nil {
		return "", nil
	}
	return d, snapshotStateDir(d)
}

// AssertNoStateLeak is the TestMain counterpart of the Layer-5 check
// in NewTestStateDir(t). Compares the current file set under realDir
// against the snapshot from SnapshotRealStateDir; if any new file
// appeared, it writes a noisy stderr warning AND returns a non-zero
// suggested exit code so TestMain can fail the run.
//
// Why warning + exit-code instead of t.Errorf: TestMain has no
// *testing.T to attach an error to. We could panic, but tests that
// raced through to completion would already be marked PASS by
// `go test`; a panic in TestMain then prints a misleading "test
// passed but cleanup failed" message. Returning a code lets the
// caller `os.Exit(testCode | leakCode)` so CI fails on real leaks
// while preserving the underlying test-failure signal.
//
// Returns 0 when nothing leaked, 1 when something did.
//
// Detection model: only flag files whose mtime is *newer than the
// snapshot start time*. Pre-existing files in the developer's real
// state dir (e.g. sessions from prior CLI runs) have mtimes from
// before TestMain started; if their content is re-read in-place the
// read access doesn't update mtime, so they don't trigger a false
// positive. Only files that were created or rewritten during this
// test run are reported.
func AssertNoStateLeak(realDir string, before map[string]time.Time) int {
	if realDir == "" {
		return 0
	}
	cutoff := time.Now()
	after := snapshotStateDir(realDir)
	var leaked []string
	for path, mt := range after {
		if mt.After(cutoff) || mt.Equal(cutoff) {
			leaked = append(leaked, path)
			continue
		}
		// For files already on disk before the run, only flag them if
		// their mtime changed AND the change happened during the run.
		// We approximate "during the run" as "newer than 1 minute before
		// the cutoff" — anything older was touched by some prior CLI
		// run, not this test.
		if prev, ok := before[path]; !ok || !prev.Equal(mt) {
			if mt.After(cutoff.Add(-1 * time.Minute)) {
				leaked = append(leaked, path)
			}
		}
	}
	if len(leaked) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr,
		"\n[state-leak] %d file(s) leaked into real state dir %q during the test run.\n"+
			"  A test built an Agent without installing SetTestStateDirHook (or NewTestStateDir).\n"+
			"  First leaked path: %s\n",
		len(leaked), realDir, leaked[0])
	return 1
}

// NewTestStateDir redirects pkg/agent's session-persistence path AND the
// global search-index updater to an isolated t.TempDir so that tests
// creating real Agents don't leak state JSONs or search-index.json into
// the caller's ~/.sprout/sessions/.
//
// The search-index redirect is load-bearing: SaveStateScoped triggers
// search.MarkSessionDirty, which schedules a debounced BuildIndex. Without
// isolation that BuildIndex walks the entire real sessions corpus (~250 MB
// including 93 MB session JSONs), building an HNSW index with 30+ GB peak
// allocation.
//
// Backstory: tests in cmd/ build real Agent instances to exercise the
// chat/plan loop. Each Agent runs autoSaveState() on a timer, which
// writes to whatever GetStateDir() returns. Without this helper that's
// the developer's real ~/.sprout/sessions/, and ~90 mock-provider
// session JSONs accumulated there before we caught it on 2026-06-08.
// See the `cleanup` body below for the Layer-5 detector that fails any
// future test that bypasses this isolation.
//
// Returns a cleanup func that:
//
//  1. Restores the original getStateDirFunc (mirrors t.Setenv unwind
//     semantics but for our package-level function var).
//  2. Snapshots the real ~/.sprout/sessions/ contents at test start
//     and re-checks at cleanup. Any new file under that tree fails the
//     test with a clear pointer at this helper — the same pattern
//     pkg/configuration/testing_isolation.go uses for the config file.
//
// Usage:
//
//	func TestMyCmdThingy(t *testing.T) {
//	    defer agent.NewTestStateDir(t)()
//	    // ... build and use a real Agent without leaking state.
//	}
//
// The helper lives in a non-_test.go file so it can be imported from
// cmd/ tests (Go forbids importing from _test.go across packages).
func NewTestStateDir(t *testing.T) func() {
	t.Helper()

	// Snapshot the REAL state dir contents BEFORE the override so the
	// detector compares against the developer's actual home. Capturing
	// after the override would compare a temp dir to itself and never
	// detect a leak.
	realDir, realDirErr := defaultGetStateDir()
	realBefore := snapshotStateDir(realDir)

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".sprout", "sessions")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("NewTestStateDir: create temp sessions dir: %v", err)
	}

	orig := getStateDirFunc
	getStateDirFunc = func() (string, error) { return stateDir, nil }

	// Redirect the search index updater into the same temp dir so
	// SaveStateScoped → search.MarkSessionDirty writes to the temp
	// dir instead of the developer's real ~/.sprout/sessions/.
	// Without this, the debounced BuildIndex would walk the entire
	// real sessions corpus (~250 MB), building an HNSW index with
	// 30+ GB peak allocation.
	oldUpdater := search.ResetGlobalUpdaterForTest()
	indexPath := filepath.Join(stateDir, "search-index.json")
	search.GlobalUpdater = search.NewIndexUpdater(indexPath, stateDir)

	return func() {
		search.RestoreGlobalUpdater(oldUpdater)
		getStateDirFunc = orig

		// Layer 5: did anything new appear under the real
		// ~/.sprout/sessions/ during the test? Tests that bypass this
		// helper (e.g. by reaching for `defaultGetStateDir()` directly,
		// or by writing through a path that doesn't consult
		// getStateDirFunc) surface here instead of silently polluting
		// the developer's session corpus.
		if realDirErr != nil {
			return
		}
		realAfter := snapshotStateDir(realDir)
		var leaked []string
		for path, mt := range realAfter {
			prev, ok := realBefore[path]
			if !ok || !prev.Equal(mt) {
				leaked = append(leaked, path)
			}
		}
		if len(leaked) > 0 {
			head := leaked[0]
			if len(leaked) > 1 {
				t.Errorf("test leaked %d files into real state dir %q "+
					"(first: %s). A code path bypassed getStateDirFunc — "+
					"every test that builds an Agent must `defer "+
					"agent.NewTestStateDir(t)()` up front.",
					len(leaked), realDir, head)
			} else {
				t.Errorf("test leaked file into real state dir: %s. "+
					"A code path bypassed getStateDirFunc — every test "+
					"that builds an Agent must `defer "+
					"agent.NewTestStateDir(t)()` up front.", head)
			}
		}
	}
}

// snapshotStateDir returns a path → mtime map for every regular file
// under dir. Used by the Layer-5 detector to compare pre/post test
// state without trying to diff file contents (which would be slow on
// large state corpuses and would miss files that get re-written
// in-place at byte-identical length).
//
// Returns an empty map if the dir doesn't exist yet — that's the
// common case on a fresh CI machine, and treating it as "no files"
// gives the detector the right baseline.
func snapshotStateDir(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	if dir == "" {
		return out
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Permission errors, missing-dir errors, etc. don't fail
			// the snapshot — we just record what we could read. The
			// detector compares post-snapshot against the same partial
			// view, so the asymmetry doesn't produce false positives.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out[path] = info.ModTime()
		return nil
	})
	return out
}
