package embedding

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// snapshottingStore records the store size after every Store call so a test
// can observe records being flushed in batches during a build rather than all
// at once when the build finishes.
type snapshottingStore struct {
	*countingStore
	snapshots []int
}

func (s *snapshottingStore) Store(records []VectorRecord) error {
	err := s.countingStore.Store(records)
	s.snapshots = append(s.snapshots, s.countingStore.Size())
	return err
}

// TestEmbedUnitsFlushesBatched verifies that records reach the store in
// batched flushes, not in one Store call per file. HNSWStore.Store rewrites
// the whole graph on every call, so per-file writes made a build O(N²); with
// 6 files and the 50-file checkpoint interval, the single end-of-build flush
// must carry every record.
func TestEmbedUnitsFlushesBatched(t *testing.T) {
	workspace := t.TempDir()
	const fileCount = 6
	for i := 0; i < fileCount; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("f%d.go", i)),
			fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n\nfunc G%d() string { return \"%d\" }\n", i, i, i, i))
	}

	store := &snapshottingStore{countingStore: newCountingStore()}
	idx := NewIndexManager(newMockProvider(8), store, IndexOptions{
		BatchSize:  4,
		MaxBodyLen: 2000,
	})

	if _, err := idx.BuildIndex(context.Background(), workspace); err != nil {
		t.Fatalf("build: %v", err)
	}

	if got := store.Size(); got != fileCount*2 {
		t.Fatalf("store holds %d records, want %d", got, fileCount*2)
	}
	// 6 files < manifestCheckpointInterval (50): the batcher accumulates all
	// of them and the end-of-build flush writes everything in one Store call.
	if got := store.writes.Load(); got != 1 {
		t.Errorf("Store called %d times, want 1 — one batched flush, not one per file", got)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("recorded %d Store snapshots, want 1", len(store.snapshots))
	}
	if store.snapshots[len(store.snapshots)-1] != fileCount*2 {
		t.Errorf("final Store call saw %d records, want %d", store.snapshots[len(store.snapshots)-1], fileCount*2)
	}
}

// TestRecordBatcherFlushesOnInterval verifies the batcher flushes to the
// store every flushInterval files and carries the remainder in a final flush.
func TestRecordBatcherFlushesOnInterval(t *testing.T) {
	store := &snapshottingStore{countingStore: newCountingStore()}
	b := newRecordBatcher(store, nil, 2)

	mk := func(id string) []VectorRecord { return []VectorRecord{{ID: id}} }

	if err := b.add("a.go", mk("a")); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := b.add("b.go", mk("b")); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if got := store.writes.Load(); got != 1 {
		t.Fatalf("after crossing the interval, Store called %d times, want 1", got)
	}
	if got := store.Size(); got != 2 {
		t.Errorf("first flush wrote %d records, want 2", got)
	}

	if err := b.add("c.go", mk("c")); err != nil {
		t.Fatalf("add c: %v", err)
	}
	if got := store.writes.Load(); got != 1 {
		t.Fatalf("below the interval again, Store called %d times, want still 1", got)
	}
	if got := store.Size(); got != 2 {
		t.Errorf("records below the interval must stay in memory, store has %d", got)
	}

	if err := b.flush(); err != nil {
		t.Fatalf("final flush: %v", err)
	}
	if got := store.writes.Load(); got != 2 {
		t.Errorf("after final flush, Store called %d times, want 2", got)
	}
	if got := store.Size(); got != 3 {
		t.Errorf("after final flush store holds %d records, want 3", got)
	}
}

// TestInterruptedBuildPersistsRecords is the regression test for the original
// bug: a build cancelled mid-way used to accumulate records internally and
// discard them when the timeout fired, because Store was called exactly once
// after embedUnits returned. With per-file checkpointing the completed files
// must be on disk, reloadable by a fresh store instance.
func TestInterruptedBuildPersistsRecords(t *testing.T) {
	workspace := t.TempDir()
	const fileCount = 6
	for i := 0; i < fileCount; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("f%d.go", i)),
			fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n\nfunc G%d() string { return \"%d\" }\n", i, i, i, i))
	}

	indexDir := t.TempDir()
	indexPath := filepath.Join(indexDir, "index.hnsw")
	manifestPath := filepath.Join(indexDir, "manifest.json")

	// maxBatches=2 interrupts the build after F0-F3 (batch 1) and F4,F5,G0,G1
	// (batch 2), so exactly files f0 and f1 finish embedding.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := &cancellingProvider{
		mockProvider: newMockProvider(8),
		cancel:       cancel,
		maxBatches:   2,
	}
	store, err := NewHNSWStore(indexPath, "mock-model-hash")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	idx := NewIndexManager(interrupted, store, IndexOptions{
		BatchSize:    4,
		MaxBodyLen:   2000,
		ManifestPath: manifestPath,
	})
	if _, err := idx.BuildIndex(ctx, workspace); err != nil {
		t.Fatalf("interrupted build returned an error instead of partial progress: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// A brand-new store over the same path must see the checkpointed records —
	// the exact behavior broken in production, where a cancelled build left the
	// index empty forever.
	reloaded, err := NewHNSWStore(indexPath, "mock-model-hash")
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	defer reloaded.Close()

	all, err := reloaded.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("cancelled build persisted zero records to disk")
	}
	if len(all) == fileCount*2 {
		t.Fatal("build was not actually interrupted; test proves nothing")
	}
	if len(all)%2 != 0 {
		t.Errorf("persisted %d records, want a whole number of 2-unit files", len(all))
	}

	// The manifest must agree with the store: only completed files are marked
	// indexed, so the next build resumes the rest instead of re-embedding
	// everything or, worse, skipping files that were never stored.
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("no manifest written after interrupted build")
	}
	if got := len(manifest.Files); got != len(all)/2 {
		t.Errorf("manifest marks %d files indexed but the store holds records for %d files", got, len(all)/2)
	}
}
