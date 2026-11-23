//go:build !js

package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncrementalPreservesIncomingEdges verifies that re-indexing a callee's
// file does not destroy the caller's incoming edge (the cross-file edge A→B).
// This is a regression test for the IndexChangedFiles edge-loss bug.
func TestIncrementalPreservesIncomingEdges(t *testing.T) {
	tmpDir := t.TempDir()
	store := newMemoryStore(t)
	store.baseDir = tmpDir
	ctx := context.Background()

	libPath := filepath.Join(tmpDir, "lib.go")
	appPath := filepath.Join(tmpDir, "app.go")

	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.WriteFile(libPath, []byte("package lib\n"), 0644))
	require.NoError(t, os.Chtimes(libPath, old, old))
	require.NoError(t, os.WriteFile(appPath, []byte("package main\n"), 0644))
	require.NoError(t, os.Chtimes(appPath, old, old))

	libHasNewFunc := false

	parser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		switch path {
		case "lib.go":
			syms := []Symbol{
				{QualifiedName: "pkg.DoWork", DisplayName: "DoWork", Line: 5, Kind: "func", Language: "go"},
			}
			if libHasNewFunc {
				syms = append(syms, Symbol{
					QualifiedName: "pkg.NewFunc", DisplayName: "NewFunc", Line: 10, Kind: "func", Language: "go",
				})
			}
			return syms, nil, nil
		case "app.go":
			return []Symbol{
					{QualifiedName: "pkg.run", DisplayName: "run", Line: 5, Kind: "func", Language: "go"},
				}, []Edge{
					{SourceQualifiedName: "pkg.run", TargetQualifiedName: "pkg.DoWork", EdgeType: "calls", Line: 6},
				}, nil
		}
		return nil, nil, nil
	}

	// Full index.
	require.NoError(t, store.IndexAll(ctx, parser))
	require.Equal(t, 1, store.Stats().EdgeCount, "one cross-file edge after full index")

	// Modify lib.go: add NewFunc. Only lib.go becomes stale.
	libHasNewFunc = true
	require.NoError(t, os.WriteFile(libPath, []byte("package lib\n// updated\n"), 0644))
	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, os.Chtimes(libPath, future, future))

	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	sort.Strings(stale)
	require.Equal(t, []string{"lib.go"}, stale, "only lib.go should be stale")

	// Incremental re-index — must NOT destroy the run→DoWork edge.
	require.NoError(t, store.IndexChangedFiles(ctx, parser))

	// Verify: edge survived, new symbol present.
	nodes, err := store.QueryAllNodes(ctx)
	require.NoError(t, err)
	names := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		names[n.QualifiedName] = true
	}
	assert.True(t, names["pkg.NewFunc"], "NewFunc should be indexed")
	assert.True(t, names["pkg.DoWork"], "DoWork should still exist")

	assert.Equal(t, 1, store.Stats().EdgeCount,
		"incoming cross-file edge must survive incremental re-index of callee file")

	callers, err := store.QueryCallers(ctx, "pkg.DoWork")
	require.NoError(t, err)
	require.Len(t, callers, 1, "DoWork should still have run as a caller")
	assert.Equal(t, "pkg.run", callers[0].QualifiedName)
}

// TestIncrementalRemovesDeletedCalleeEdge verifies that deleting a function
// from a callee's file removes the stale incoming edge.
func TestIncrementalRemovesDeletedCalleeEdge(t *testing.T) {
	tmpDir := t.TempDir()
	store := newMemoryStore(t)
	store.baseDir = tmpDir
	ctx := context.Background()

	libPath := filepath.Join(tmpDir, "lib.go")
	appPath := filepath.Join(tmpDir, "app.go")

	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.WriteFile(libPath, []byte("package lib\n"), 0644))
	require.NoError(t, os.Chtimes(libPath, old, old))
	require.NoError(t, os.WriteFile(appPath, []byte("package main\n"), 0644))
	require.NoError(t, os.Chtimes(appPath, old, old))

	doWorkExists := true

	parser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		switch path {
		case "lib.go":
			syms := []Symbol{
				{QualifiedName: "pkg.Helper", DisplayName: "Helper", Line: 5, Kind: "func", Language: "go"},
			}
			if doWorkExists {
				syms = append(syms, Symbol{
					QualifiedName: "pkg.DoWork", DisplayName: "DoWork", Line: 10, Kind: "func", Language: "go",
				})
			}
			return syms, nil, nil
		case "app.go":
			return []Symbol{
					{QualifiedName: "pkg.run", DisplayName: "run", Line: 5, Kind: "func", Language: "go"},
				}, []Edge{
					{SourceQualifiedName: "pkg.run", TargetQualifiedName: "pkg.DoWork", EdgeType: "calls", Line: 6},
				}, nil
		}
		return nil, nil, nil
	}

	require.NoError(t, store.IndexAll(ctx, parser))
	require.Equal(t, 1, store.Stats().EdgeCount)

	// Delete DoWork from lib.go.
	doWorkExists = false
	require.NoError(t, os.WriteFile(libPath, []byte("package lib\n// removed DoWork\n"), 0644))
	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, os.Chtimes(libPath, future, future))

	require.NoError(t, store.IndexChangedFiles(ctx, parser))

	// DoWork node is gone, and the edge run→DoWork is removed.
	nodes, err := store.QueryAllNodes(ctx)
	require.NoError(t, err)
	for _, n := range nodes {
		assert.NotEqual(t, "pkg.DoWork", n.QualifiedName, "DoWork node should be removed")
	}
	assert.Equal(t, 0, store.Stats().EdgeCount, "edge to deleted function should be removed")
}

// TestIncrementalIdempotentWhenNothingStale verifies no data loss when
// IndexChangedFiles is called but nothing is stale.
// TestIncrementalPreservesTransitiveChain verifies the M1 reviewer finding is
// fixed: in a 3-file call chain X→A→B, when only B changes, the edge X→A must
// survive. The earlier version of InsertEdgesForFiles deleted incoming edges to
// referrer files too, progressively destroying edges along call chains.
func TestIncrementalPreservesTransitiveChain(t *testing.T) {
	tmpDir := t.TempDir()
	store := newMemoryStore(t)
	store.baseDir = tmpDir
	ctx := context.Background()

	// Three files: x.go calls a.go calls b.go
	files := map[string]string{
		"x.go": "package main", "a.go": "package main", "b.go": "package main",
	}
	old := time.Now().Add(-2 * time.Hour)
	for path, content := range files {
		fp := filepath.Join(tmpDir, path)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0644))
		require.NoError(t, os.Chtimes(fp, old, old))
	}

	parser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		switch path {
		case "x.go":
			return []Symbol{
					{QualifiedName: "pkg.xFunc", DisplayName: "xFunc", Line: 5, Kind: "func", Language: "go"},
				}, []Edge{
					{SourceQualifiedName: "pkg.xFunc", TargetQualifiedName: "pkg.aFunc", EdgeType: "calls", Line: 6},
				}, nil
		case "a.go":
			return []Symbol{
					{QualifiedName: "pkg.aFunc", DisplayName: "aFunc", Line: 5, Kind: "func", Language: "go"},
				}, []Edge{
					{SourceQualifiedName: "pkg.aFunc", TargetQualifiedName: "pkg.bFunc", EdgeType: "calls", Line: 6},
				}, nil
		case "b.go":
			return []Symbol{
				{QualifiedName: "pkg.bFunc", DisplayName: "bFunc", Line: 5, Kind: "func", Language: "go"},
			}, nil, nil
		}
		return nil, nil, nil
	}

	// Full index establishes the chain xFunc→aFunc→bFunc.
	require.NoError(t, store.IndexAll(ctx, parser))
	require.Equal(t, 2, store.Stats().EdgeCount, "two edges: x→a and a→b")

	// Modify ONLY b.go (the leaf). Touch its mtime.
	bPath := filepath.Join(tmpDir, "b.go")
	require.NoError(t, os.WriteFile(bPath, []byte("package main // updated\n"), 0644))
	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, os.Chtimes(bPath, future, future))

	// Stale: only b.go.
	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	sort.Strings(stale)
	require.Equal(t, []string{"b.go"}, stale)

	require.NoError(t, store.IndexChangedFiles(ctx, parser))

	// After incremental update, BOTH edges must survive.
	// b.go is stale (full delete), a.go is a referrer (outgoing-only delete).
	// x.go is NOT in the closure — its edge to a.go must be untouched.
	assert.Equal(t, 2, store.Stats().EdgeCount,
		"transitive chain edges must survive: x→a (untouched) + a→b (re-resolved)")

	// Verify callers explicitly.
	xCallers, err := store.QueryCallers(ctx, "pkg.xFunc")
	require.NoError(t, err)
	assert.Empty(t, xCallers, "xFunc has no callers")

	aCallers, err := store.QueryCallers(ctx, "pkg.aFunc")
	require.NoError(t, err)
	require.Len(t, aCallers, 1, "aFunc must still have xFunc as caller")
	assert.Equal(t, "pkg.xFunc", aCallers[0].QualifiedName)

	bCallers, err := store.QueryCallers(ctx, "pkg.bFunc")
	require.NoError(t, err)
	require.Len(t, bCallers, 1, "bFunc must still have aFunc as caller")
	assert.Equal(t, "pkg.aFunc", bCallers[0].QualifiedName)
}

func TestIncrementalIdempotentWhenNothingStale(t *testing.T) {
	tmpDir := t.TempDir()
	store := newMemoryStore(t)
	store.baseDir = tmpDir
	ctx := context.Background()

	libPath := filepath.Join(tmpDir, "lib.go")
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.WriteFile(libPath, []byte("package lib\n"), 0644))
	require.NoError(t, os.Chtimes(libPath, old, old))

	parser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		return []Symbol{
			{QualifiedName: "pkg.DoWork", DisplayName: "DoWork", Line: 5, Kind: "func", Language: "go"},
		}, nil, nil
	}

	require.NoError(t, store.IndexAll(ctx, parser))
	before := store.Stats()

	// Nothing changed — call IndexChangedFiles.
	require.NoError(t, store.IndexChangedFiles(ctx, parser))

	after := store.Stats()
	assert.Equal(t, before, after, "no data loss when nothing is stale")
}
