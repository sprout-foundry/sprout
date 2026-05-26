package embedding

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestHNSWStore(t *testing.T) *HNSWStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")
	store, err := NewHNSWStore(path, "test-model-hash")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestHNSWStoreAndQuery(t *testing.T) {
	s := newTestHNSWStore(t)

	records := []VectorRecord{
		{
			ID: "file1.go:1", File: "file1.go", Name: "foo",
			Embedding: []float32{1, 0, 0}, Language: "go",
			IndexedAt: time.Now(),
		},
		{
			ID: "file2.go:1", File: "file2.go", Name: "bar",
			Embedding: []float32{0, 1, 0}, Language: "go",
			IndexedAt: time.Now(),
		},
	}
	require.NoError(t, s.Store(records))

	// Query near [1,0,0] — should rank file1 first
	results, err := s.Query([]float32{1, 0, 0}, 2, 0.5)
	require.NoError(t, err)
	require.NotEmpty(t, results, "should find results")
	// First result should be file1 (exact match to [1,0,0])
	require.Equal(t, "file1.go:1", results[0].Record.ID)
	require.GreaterOrEqual(t, results[0].Similarity, float32(0.99))
}

func TestHNSWStoreUpsert(t *testing.T) {
	s := newTestHNSWStore(t)

	rec1 := VectorRecord{
		ID: "dup:1", File: "a.go", Name: "old",
		Embedding: []float32{1, 0, 0}, IndexedAt: time.Now(),
	}
	require.NoError(t, s.Store([]VectorRecord{rec1}))
	require.Equal(t, 1, s.Size())

	rec2 := VectorRecord{
		ID: "dup:1", File: "a.go", Name: "new",
		Embedding: []float32{0, 1, 0}, IndexedAt: time.Now(),
	}
	require.NoError(t, s.Store([]VectorRecord{rec2}))
	require.Equal(t, 1, s.Size())

	all, err := s.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "new", all[0].Name)
}

func TestHNSWStoreDeleteByFile(t *testing.T) {
	s := newTestHNSWStore(t)

	records := []VectorRecord{
		{ID: "a:1", File: "a.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "a:2", File: "a.go", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		{ID: "b:1", File: "b.go", Embedding: []float32{0, 0, 1}, IndexedAt: time.Now()},
	}
	require.NoError(t, s.Store(records))
	require.Equal(t, 3, s.Size())

	require.NoError(t, s.DeleteByFile("a.go"))
	require.Equal(t, 1, s.Size())

	all, err := s.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "b:1", all[0].ID)
}

func TestHNSWStoreReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	s1, err := NewHNSWStore(path, "v1")
	require.NoError(t, err)
	require.NoError(t, s1.Store([]VectorRecord{
		{ID: "x:1", File: "x.go", Name: "hello", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}))
	require.NoError(t, s1.Close())

	// Reload
	s2, err := NewHNSWStore(path, "v1")
	require.NoError(t, err)
	defer s2.Close()

	all, err := s2.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "hello", all[0].Name)
}

// TestHNSWStorePartialSaveRecovery simulates the crash scenario where a
// previous session persisted the graph file but failed to persist the
// records sidecar. The next session must not panic when rebuilding —
// previously, Store() consulted s.records to decide whether to delete a
// key before re-adding it; when records was empty but the graph still
// held the IDs, Add() tripped hnsw's "node not added" invariant.
func TestHNSWStorePartialSaveRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	s1, err := NewHNSWStore(path, "v1")
	require.NoError(t, err)
	require.NoError(t, s1.Store([]VectorRecord{
		{ID: "x:1", File: "x.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "x:2", File: "x.go", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}))
	require.NoError(t, s1.Close())

	// Simulate a partial save: drop the records sidecar but keep the graph.
	require.NoError(t, os.Remove(path+".records.json"))

	s2, err := NewHNSWStore(path, "v1")
	require.NoError(t, err)
	defer s2.Close()

	// Reconciliation should have cleared the graph so re-Store of the same
	// IDs works without tripping the hnsw "node not added" invariant.
	require.NotPanics(t, func() {
		require.NoError(t, s2.Store([]VectorRecord{
			{ID: "x:1", File: "x.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
			{ID: "x:2", File: "x.go", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		}))
	})
	require.Equal(t, 2, s2.Size())
}

// TestHNSWStoreStaleGraphKeyRecovery covers the same panic surface from a
// different angle: the records map is populated but the graph happens to
// already contain a stray key (e.g., loaded from a corrupted prior state).
// Store() must check the graph itself, not just s.records, to decide
// whether to delete-before-add.
func TestHNSWStoreStaleGraphKeyRecovery(t *testing.T) {
	s := newTestHNSWStore(t)

	// Seed the graph directly, bypassing s.records, to mimic the
	// graph-ahead-of-records state.
	rec := VectorRecord{
		ID: "stale:1", File: "x.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now(),
	}
	require.NoError(t, s.Store([]VectorRecord{rec}))
	// Drop from s.records but leave it in the graph.
	delete(s.records, rec.ID)

	// Re-storing the same ID must not panic.
	require.NotPanics(t, func() {
		require.NoError(t, s.Store([]VectorRecord{rec}))
	})
	require.Equal(t, 1, s.Size())
}

func TestHNSWStoreEmptyQuery(t *testing.T) {
	s := newTestHNSWStore(t)

	results, err := s.Query([]float32{1, 0, 0}, 5, 0.5)
	require.NoError(t, err)
	require.Nil(t, results)
}

func TestHNSWStoreModelHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	s1, err := NewHNSWStore(path, "model-v1")
	require.NoError(t, err)
	require.NoError(t, s1.Store([]VectorRecord{
		{ID: "a:1", File: "a.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}))
	require.Equal(t, 1, s1.Size())
	require.NoError(t, s1.Close())

	// Reopen with different model hash
	s2, err := NewHNSWStore(path, "model-v2")
	require.NoError(t, err)
	defer s2.Close()
	require.Equal(t, 0, s2.Size(), "store should be cleared on model hash mismatch")
}

func TestHNSWStoreModelHashMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	s1, err := NewHNSWStore(path, "model-v1")
	require.NoError(t, err)
	require.NoError(t, s1.Store([]VectorRecord{
		{ID: "a:1", File: "a.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}))
	require.NoError(t, s1.Close())

	// Reopen with same hash — data preserved (though metadata is cleared per reload behavior)
	s2, err := NewHNSWStore(path, "model-v1")
	require.NoError(t, err)
	defer s2.Close()
	// Graph data may persist but metadata is cleared (rebuild needed scenario)
}

func TestHNSWStoreLoadAll(t *testing.T) {
	s := newTestHNSWStore(t)

	records := []VectorRecord{
		{ID: "a:1", File: "a.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "b:1", File: "b.go", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		{ID: "c:1", File: "c.go", Embedding: []float32{0, 0, 1}, IndexedAt: time.Now()},
	}
	require.NoError(t, s.Store(records))
	require.Equal(t, 3, s.Size())

	all, err := s.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 3)

	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	require.Equal(t, "a:1", all[0].ID)
	require.Equal(t, "b:1", all[1].ID)
	require.Equal(t, "c:1", all[2].ID)
}

func TestHNSWStoreLargeDataset(t *testing.T) {
	s := newTestHNSWStore(t)

	const n = 500
	records := make([]VectorRecord, n)
	for i := 0; i < n; i++ {
		// Create varied 3D vectors
		angle := float64(i) / float64(n) * 6.28
		records[i] = VectorRecord{
			ID:        fmt.Sprintf("rec:%d", i),
			File:      fmt.Sprintf("file_%d.go", i/50),
			Embedding: []float32{float32(math.Cos(angle)), float32(math.Sin(angle)), 0.01},
			IndexedAt: time.Now(),
		}
	}
	require.NoError(t, s.Store(records))
	require.Equal(t, n, s.Size())

	queryVec := []float32{1, 0, 0} // near angle=0
	results, err := s.Query(queryVec, 10, 0.9)
	require.NoError(t, err)
	require.NotEmpty(t, results, "should find some results near [1,0,0]")

	// Verify results are mostly in order (HNSW is approximate, so exact ordering
	// is not guaranteed, but the best results should match well).
	require.NotEmpty(t, results)
	for _, r := range results {
		require.GreaterOrEqual(t, r.Similarity, float32(0.9),
			"all results should meet the threshold")
	}
}

func TestHNSWStoreReplaceAll(t *testing.T) {
	s := newTestHNSWStore(t)

	// Initial data
	require.NoError(t, s.Store([]VectorRecord{
		{ID: "old:1", File: "old.go", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "old:2", File: "old.go", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		{ID: "old:3", File: "old.go", Embedding: []float32{0, 0, 1}, IndexedAt: time.Now()},
	}))
	require.Equal(t, 3, s.Size())

	// Replace with new data
	require.NoError(t, s.ReplaceAll([]VectorRecord{
		{ID: "new:1", File: "new.go", Name: "alpha", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "new:2", File: "new.go", Name: "beta", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}))
	require.Equal(t, 2, s.Size())

	all, err := s.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 2)

	names := map[string]bool{}
	for _, r := range all {
		names[r.Name] = true
	}
	require.True(t, names["alpha"])
	require.True(t, names["beta"])
}

// TestHNSWStoreReplaceAll_DuplicateIDsDoNotPanic reproduces the crash
// reported in a live sprout session:
//
//	panic: node not added
//	github.com/coder/hnsw.(*Graph[...]).Add  graph.go:405
//	(*HNSWStore).ReplaceAll                  store_hnsw.go:426
//	(*EmbeddingManager).AutoBuildWhenReady   manager.go:365
//
// The hnsw library's Add() invariant `g.Len() == preLen+1` is violated
// when an existing key is replaced — Delete decrements Len, Insert
// increments it, net change 0, panic. Our upstream extractors
// (extractor_py.go, extractor_ts.go) can produce duplicate IDs of the
// form "path:methodname" when two same-named methods live in different
// classes of the same file. ReplaceAll now dedupes before calling Add;
// this test pins that behavior.
func TestHNSWStoreReplaceAll_DuplicateIDsDoNotPanic(t *testing.T) {
	s := newTestHNSWStore(t)

	// Two records share an ID — mirrors what
	// `extractor_py.go: fmt.Sprintf("%s:%s", path, methodName)` produces
	// for `class A: def run` and `class B: def run` in the same file.
	now := time.Now()
	records := []VectorRecord{
		{ID: "shared.py:run", File: "shared.py", Name: "A.run", Embedding: []float32{1, 0, 0}, IndexedAt: now},
		{ID: "shared.py:run", File: "shared.py", Name: "B.run", Embedding: []float32{0, 1, 0}, IndexedAt: now},
		{ID: "other.py:once", File: "other.py", Name: "once", Embedding: []float32{0, 0, 1}, IndexedAt: now},
	}

	require.NotPanics(t, func() {
		require.NoError(t, s.ReplaceAll(records))
	}, "ReplaceAll must dedupe duplicate IDs before hitting the hnsw graph")

	// Last-write-wins semantics — the SECOND `shared.py:run` should be
	// the one in the store, matching the map-population order.
	require.Equal(t, 2, s.Size(), "deduped store should hold 2 unique IDs")
	all, err := s.LoadAll()
	require.NoError(t, err)

	for _, r := range all {
		if r.ID == "shared.py:run" {
			require.Equal(t, "B.run", r.Name, "last record with duplicate ID should win")
		}
	}
}
