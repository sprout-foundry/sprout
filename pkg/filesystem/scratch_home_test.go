package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// scratchHome returns an isolated HOME for tests that need a writable
// location OUTSIDE the OS temp dir (the /tmp exception in isInTmpPath makes
// a t.TempDir workspace vacuous for these tests).
//
// The scratch root lives under $HOME/.cache/sprout-test-homes — a real-home
// subtree, so os.TempDir() checks still classify it as non-tmp — but each
// call gets a unique directory (parallel test binaries and repeated runs
// can never collide on the fixed ~/.sprout-test-* paths these tests used
// before, which raced between CI and local runs and littered the real home
// on failure). t.Setenv auto-restores HOME; the scratch tree is removed on
// cleanup.
func scratchHome(t *testing.T) string {
	t.Helper()

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("scratchHome: no home dir available: %v", err)
	}

	scratchRoot := filepath.Join(realHome, ".cache", "sprout-test-homes")
	if err := os.MkdirAll(scratchRoot, 0755); err != nil {
		t.Fatalf("scratchHome: create scratch root %s: %v", scratchRoot, err)
	}

	dir, err := os.MkdirTemp(scratchRoot, fmt.Sprintf("%s-", t.Name()))
	if err != nil {
		t.Fatalf("scratchHome: create scratch home: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
		pruneScratchRoot(scratchRoot)
	})

	t.Setenv("HOME", dir)
	return dir
}

// pruneScratchRoot removes the scratch root when it is empty, so a
// long-lived machine does not accumulate .cache/sprout-test-homes shells.
// Best-effort: failure to remove (e.g. a parallel test still inside) is
// ignored.
func pruneScratchRoot(root string) {
	if entries, err := os.ReadDir(root); err == nil && len(entries) == 0 {
		os.Remove(root)
	}
}
