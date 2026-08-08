package embedding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"
)

// TestConcurrentBuilders exercises two independent IndexManagers (each with
// their own mock provider and HNSW store) writing to the same index directory
// simultaneously. The in-process sync.Mutex in HNSWStore cannot serialize two
// separate store instances, so this genuinely exercises the cross-process flock
// on .build.lock. Both calls must complete without error, and the resulting
// index on disk must be consistent (not corrupted).
func TestConcurrentBuilders(t *testing.T) {
	dir := t.TempDir()

	// --- Fixture: two Go files, each with two functions (4 units total) ---
	srcA := `package main

func ReadConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func WriteOutput(data []byte) error {
	return os.WriteFile("out.txt", data, 0644)
}
`
	srcB := `package main

func ParseInput(line string) (string, bool) {
	if len(line) == 0 {
		return "", false
	}
	return line, true
}

func FormatOutput(v string) string {
	return "[out] " + v
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(srcA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte(srcB), 0o644))

	// --- Create TWO independent IndexManagers, each with their own provider & store ---
	opts := IndexOptions{
		BatchSize:  16,
		MaxBodyLen: 500,
		IndexDir:   dir, // enables flock on dir/.build.lock
	}

	storeA, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	require.NoError(t, err)
	idxA := NewIndexManager(newMockProvider(3), storeA, opts)

	storeB, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	require.NoError(t, err)
	idxB := NewIndexManager(newMockProvider(3), storeB, opts)

	ctx := context.Background()

	// --- Launch both builders concurrently with a start barrier ---
	start := make(chan struct{})
	var wg sync.WaitGroup

	var statsA *IndexStats
	var errA error
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		statsA, errA = idxA.BuildIndex(ctx, dir)
	}()

	var statsB *IndexStats
	var errB error
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		statsB, errB = idxB.BuildIndex(ctx, dir)
	}()

	close(start) // release both goroutines simultaneously
	wg.Wait()

	// Both must complete without error (one may have skipped due to lock contention,
	// which is the correct graceful behavior — either way it's nil error).
	require.NoError(t, errA, "builder A should not error")
	require.NoError(t, errB, "builder B should not error")

	// At least one should have actually embedded units (the winner of the lock).
	// When flock is available, exactly one should succeed and the other should
	// skip (returning zero stats). When flock is unavailable, both will succeed
	// and both will write (last writer wins).
	totalEmbedded := statsA.UnitsEmbedded + statsB.UnitsEmbedded
	require.Greater(t, totalEmbedded, 0,
		"at least one builder should have embedded units")

	// --- Close the builder stores so the fresh store can reopen cleanly ---
	require.NoError(t, storeA.Close())
	require.NoError(t, storeB.Close())

	// --- Verify: open a FRESH store and check consistency ---
	fresh, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	require.NoError(t, err, "fresh store must open without error (no disk corruption)")
	defer fresh.Close()

	all, err := fresh.LoadAll()
	require.NoError(t, err, "LoadAll must succeed (records JSON must parse)")

	// The store should have exactly 4 records (2 functions × 2 files).
	// When both builders run (flock unavailable), each does a full build.
	// The second build replaces the first, so we still end up with 4 records.
	// When one builder wins the lock, it produces 4 records.
	require.Equal(t, 4, len(all),
		"index should contain exactly 4 unit records (2 files × 2 functions each)")

	// Verify both fixture files are represented.
	files := make(map[string]int)
	for _, r := range all {
		files[r.File]++
	}
	require.Equal(t, 2, files[filepath.Join(dir, "a.go")], "a.go should have 2 records")
	require.Equal(t, 2, files[filepath.Join(dir, "b.go")], "b.go should have 2 records")

	// Verify the index is queryable — build a query vector and search.
	queryText := "func ReadConfig(path string)"
	vec, err := newMockProvider(3).Embed(ctx, codeQueryPrefix+queryText)
	require.NoError(t, err)

	results, err := fresh.Query(vec, 5, 0.5)
	require.NoError(t, err, "Query should not error on consistent index")
	require.NotEmpty(t, results, "Query should return results for a known function")

	// Verify the raw records JSON on disk is valid (not torn from concurrent writes).
	recordsBytes, err := os.ReadFile(filepath.Join(dir, "index.hnsw.records.json"))
	require.NoError(t, err)
	var rawRecords map[string]VectorRecord
	require.NoError(t, json.Unmarshal(recordsBytes, &rawRecords),
		"records.json must be valid JSON (no torn writes)")

	// Verify the .build.lock file exists in the directory.
	require.FileExists(t, filepath.Join(dir, ".build.lock"),
		"the .build.lock file should exist after concurrent builds")
}

// TestBuildIndexSkipsWhenLockHeld verifies the two facets of lock behavior:
// 1. acquireBuildLock returns errBuildLocked when the flock is held.
// 2. BuildIndex gracefully skips (nil error, zero stats) when the lock is held.
func TestBuildIndexSkipsWhenLockHeld(t *testing.T) {
	t.Run("acquireBuildLock returns errBuildLocked", func(t *testing.T) {
		dir := t.TempDir()

		// Pre-acquire the flock so acquireBuildLock will fail.
		f := flock.New(filepath.Join(dir, ".build.lock"))
		ok, err := f.TryLock()
		require.NoError(t, err)
		require.True(t, ok, "external lock must be acquired")
		t.Cleanup(func() { f.Unlock() })

		// acquireBuildLock should detect the held lock and return errBuildLocked.
		release, err := acquireBuildLock(dir)
		require.Nil(t, release)
		require.ErrorIs(t, err, errBuildLocked,
			"acquireBuildLock should return errBuildLocked when another process holds the flock")
	})

	t.Run("BuildIndex gracefully skips when lock is held", func(t *testing.T) {
		dir := t.TempDir()

		// Create a fixture file so BuildIndex would normally do work.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"),
			[]byte("package main\nfunc Hello() {}\n"), 0o644))

		// Pre-acquire the flock.
		f := flock.New(filepath.Join(dir, ".build.lock"))
		ok, err := f.TryLock()
		require.NoError(t, err)
		require.True(t, ok)
		t.Cleanup(func() { f.Unlock() })

		provider := newMockProvider(3)
		store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
		require.NoError(t, err)
		t.Cleanup(func() { store.Close() })

		idx := NewIndexManager(provider, store, IndexOptions{
			BatchSize:  16,
			MaxBodyLen: 500,
			IndexDir:   dir, // enables locking
		})

		ctx := context.Background()
		stats, err := idx.BuildIndex(ctx, dir)

		// BuildIndex should NOT error — it should skip gracefully.
		require.NoError(t, err, "BuildIndex must not error when the lock is held; it should skip")
		require.NotNil(t, stats)
		// When the lock is held past the timeout, BuildIndex returns
		// an empty stats struct (zero files, zero units).
		require.Equal(t, 0, stats.FilesProcessed,
			"BuildIndex should skip all work when the lock is held")
		require.Equal(t, 0, stats.UnitsExtracted,
			"BuildIndex should skip all work when the lock is held")

		// The store should remain empty — nothing was written.
		require.Equal(t, 0, store.Size(),
			"store should remain empty after a skipped build")
	})

	t.Run("acquireBuildLock succeeds after lock is released", func(t *testing.T) {
		dir := t.TempDir()

		// Acquire then release the flock.
		f := flock.New(filepath.Join(dir, ".build.lock"))
		ok, err := f.TryLock()
		require.NoError(t, err)
		require.True(t, ok)

		f.Unlock()

		// Now acquireBuildLock should succeed.
		release, err := acquireBuildLock(dir)
		require.NoError(t, err)
		if release != nil {
			release()
		}
	})
}
