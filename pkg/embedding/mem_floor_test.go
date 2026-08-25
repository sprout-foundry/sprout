package embedding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// belowFloorAt returns a memAvailableFn that reports plenty of memory until
// the call count exceeds breachAt, then one byte below the floor. checkMemFloor
// runs once before the loop and once per batch, so call counts map
// deterministically onto batches.
func belowFloorAt(breachAt int) func() (uint64, bool) {
	calls := 0
	return func() (uint64, bool) {
		calls++
		if calls > breachAt {
			return memFloorBytes - 1, true
		}
		return memFloorBytes + (1 << 30), true
	}
}

// A build that runs out of memory mid-loop must stop cleanly: partial records
// are flushed (same path as cancellation) and the build returns success with
// fewer than all units embedded — not a hard failure that discards progress.
func TestBuildIndexHaltsBelowMemoryFloorMidBuild(t *testing.T) {
	workspace := t.TempDir()
	const fileCount = 4
	for i := 0; i < fileCount; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("f%d.go", i)),
			fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n\nfunc G%d() string { return \"%d\" }\n", i, i, i, i))
	}

	orig := memAvailableFn
	defer func() { memAvailableFn = orig }()
	memAvailableFn = belowFloorAt(2) // pre-check + first batch pass, second batch halts

	store := newCountingStore()
	idx := NewIndexManager(newMockProvider(8), store, IndexOptions{BatchSize: 2, MaxBodyLen: 2000})

	stats, err := idx.BuildIndex(context.Background(), workspace)
	if err != nil {
		t.Fatalf("floor-halted build returned an error instead of partial progress: %v", err)
	}
	if got := store.Size(); got == 0 {
		t.Fatal("floor-halted build stored nothing; partial flush is broken")
	}
	if got := store.Size(); got >= fileCount*2 {
		t.Errorf("floor-halted build stored %d records, want fewer than %d (build must stop early)", got, fileCount*2)
	}
	// UnitsEmbedded counts units that began embedding, but the store only
	// persists completed files — a mid-file halt can leave UnitsEmbedded
	// ahead of the stored count, so require >= rather than exact equality.
	if stats.UnitsEmbedded < store.Size() {
		t.Errorf("UnitsEmbedded = %d, store size = %d", stats.UnitsEmbedded, store.Size())
	}
	if stats.FilesProcessed != fileCount {
		t.Errorf("FilesProcessed = %d, want %d (extraction is not gated by the floor)", stats.FilesProcessed, fileCount)
	}
}

// Below the floor before any unit embeds, the build must fail loudly rather
// than return an empty, successful index.
func TestBuildIndexFailsBelowMemoryFloorAtStart(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "main.go"), "package main\n\nfunc Alpha() int { return 1 }\n")

	orig := memAvailableFn
	defer func() { memAvailableFn = orig }()
	memAvailableFn = belowFloorAt(0)

	store := newCountingStore()
	idx := NewIndexManager(newMockProvider(8), store, IndexOptions{BatchSize: 2, MaxBodyLen: 2000})

	_, err := idx.BuildIndex(context.Background(), workspace)
	if err == nil {
		t.Fatal("below-floor build returned no error")
	}
	if !errors.Is(err, errMemFloor) {
		t.Fatalf("error = %v, want errMemFloor", err)
	}
	if got := store.Size(); got != 0 {
		t.Errorf("below-floor build stored %d records, want 0", got)
	}
}

// Control: with memory above the floor the same workspace indexes completely.
func TestBuildIndexCompletesAboveMemoryFloor(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "main.go"), "package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() string { return \"b\" }\n")

	orig := memAvailableFn
	defer func() { memAvailableFn = orig }()
	memAvailableFn = func() (uint64, bool) { return memFloorBytes + (1 << 30), true }

	store := newCountingStore()
	idx := NewIndexManager(newMockProvider(8), store, IndexOptions{BatchSize: 2, MaxBodyLen: 2000})

	stats, err := idx.BuildIndex(context.Background(), workspace)
	if err != nil {
		t.Fatalf("above-floor build failed: %v", err)
	}
	if want := 2; store.Size() != want {
		t.Errorf("store size = %d, want %d", store.Size(), want)
	}
	if stats.UnitsEmbedded != 2 {
		t.Errorf("UnitsEmbedded = %d, want 2", stats.UnitsEmbedded)
	}
}

func TestCheckMemFloor(t *testing.T) {
	orig := memAvailableFn
	defer func() { memAvailableFn = orig }()

	memAvailableFn = func() (uint64, bool) { return 0, false }
	if err := checkMemFloor(); err != nil {
		t.Errorf("platform that cannot report memory should not fail: %v", err)
	}

	memAvailableFn = func() (uint64, bool) { return memFloorBytes, true }
	if err := checkMemFloor(); err != nil {
		t.Errorf("memory exactly at the floor should pass: %v", err)
	}

	memAvailableFn = func() (uint64, bool) { return memFloorBytes - 1, true }
	if err := checkMemFloor(); !errors.Is(err, errMemFloor) {
		t.Errorf("memory below the floor should fail with errMemFloor, got %v", err)
	}
}

// A system-wide memory condition must abort the whole git-diff update, not
// just skip the current file: every remaining file would hit the same floor,
// so the loop should not keep calling UpdateFile (or report N per-file
// failures) pointlessly.
func TestUpdateFromGitDiffAbortsBelowMemoryFloor(t *testing.T) {
	workspace := t.TempDir()
	runGitTest(t, workspace, "init")
	runGitTest(t, workspace, "config", "user.email", "test@test.com")
	runGitTest(t, workspace, "config", "user.name", "Test")

	writeFile(t, filepath.Join(workspace, "f1.go"), "package p\n\nfunc F1() int { return 1 }\n")
	writeFile(t, filepath.Join(workspace, "f2.go"), "package p\n\nfunc F2() int { return 2 }\n")
	runGitTest(t, workspace, "add", "f1.go", "f2.go")

	// git reports paths relative to the repo root, and the agent process runs
	// with the workspace as CWD — mirror that so UpdateFile can resolve them.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	orig := memAvailableFn
	t.Cleanup(func() { memAvailableFn = orig })
	memAvailableFn = func() (uint64, bool) { return memFloorBytes - 1, true }

	idx := NewIndexManager(newMockProvider(8), newCountingStore(), IndexOptions{BatchSize: 2, MaxBodyLen: 2000})

	stats, err := idx.UpdateFromGitDiff(context.Background(), workspace)
	if !errors.Is(err, errMemFloor) {
		t.Fatalf("error = %v, want errMemFloor", err)
	}
	if strings.Contains(err.Error(), "failed to update") {
		t.Errorf("error %q looks like per-file skip spam, want a single abort error", err)
	}
	if stats.FilesProcessed != 0 {
		t.Errorf("FilesProcessed = %d, want 0 (loop must abort before processing remaining files)", stats.FilesProcessed)
	}
}

// runGitTest runs git in dir, failing the test on any error. Used to set up
// the hermetic temp repo for git-diff tests.
func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed in %s: %v\noutput: %s", strings.Join(args, " "), dir, err, string(out))
	}
}

// A floor trip mid-file must still escape to UpdateFromGitDiff: the sentinel
// must not be swallowed by embedUnits' partial-results path. If it were, the
// abort would fire one file late (the next file's pre-check) or — when the
// trip lands on the last file — the update would return success silently.
func TestUpdateFromGitDiffAbortsMidFileBelowMemoryFloor(t *testing.T) {
	workspace := t.TempDir()
	runGitTest(t, workspace, "init")
	runGitTest(t, workspace, "config", "user.email", "test@test.com")
	runGitTest(t, workspace, "config", "user.name", "Test")

	// Three functions per file => three units, so at BatchSize 2 each file
	// needs two batches (2 + 1). The first file's second batch trips the
	// floor mid-loop, not the pre-check.
	writeFile(t, filepath.Join(workspace, "f1.go"),
		"package p\n\nfunc F1() int { return 1 }\n\nfunc F2() int { return 2 }\n\nfunc F3() int { return 3 }\n")
	writeFile(t, filepath.Join(workspace, "f2.go"),
		"package p\n\nfunc G1() int { return 1 }\n\nfunc G2() int { return 2 }\n\nfunc G3() int { return 3 }\n")
	runGitTest(t, workspace, "add", "f1.go", "f2.go")

	// git reports paths relative to the repo root, and the agent process runs
	// with the workspace as CWD — mirror that so UpdateFile can resolve them.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	orig := memAvailableFn
	t.Cleanup(func() { memAvailableFn = orig })
	memAvailableFn = belowFloorAt(2) // pre-check + first batch pass, second batch trips

	idx := NewIndexManager(newMockProvider(8), newCountingStore(), IndexOptions{BatchSize: 2, MaxBodyLen: 2000})

	stats, err := idx.UpdateFromGitDiff(context.Background(), workspace)
	if !errors.Is(err, errMemFloor) {
		t.Fatalf("error = %v, want errMemFloor (trip was mid-file, not the pre-check)", err)
	}
	if strings.Contains(err.Error(), "failed to update") {
		t.Errorf("error %q looks like per-file skip spam, want a single abort error", err)
	}
	// Aborting mid-first-file means the first file must not count as
	// processed — with the sentinel swallowed it would (and the abort would
	// land one file late at the second file's pre-check instead).
	if stats.FilesProcessed != 0 {
		t.Errorf("FilesProcessed = %d, want 0 (loop must abort mid-first-file, not one file late)", stats.FilesProcessed)
	}
}
