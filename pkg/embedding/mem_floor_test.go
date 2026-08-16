package embedding

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
