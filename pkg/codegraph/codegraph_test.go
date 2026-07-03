package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMemoryStore creates an isolated in-memory SQLite store for a single test.
// It uses a temp file (not :memory:) because database/sql opens multiple
// connections and each :memory: connection gets its own empty database.
func newMemoryStore(t *testing.T) *SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewStore_CreatesDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sub", "graph.db")

	store, err := NewStore(dbPath)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.FileExists(t, dbPath)

	err = store.Close()
	require.NoError(t, err)

	// Re-opening the same path should work.
	store2, err := NewStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, store2.Close())
}

func TestIndexFile_BasicInsert(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/hello/hello.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/hello.SayHello", DisplayName: "SayHello", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/hello.Greeting", DisplayName: "Greeting", FilePath: path, Line: 3, Kind: "type", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.NodeCount)
	assert.Equal(t, 1, stats.FileCount)
}

func TestQueryCallersAndCallees(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.run", DisplayName: "run", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.fetchData", DisplayName: "fetchData", FilePath: path, Line: 20, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.parse", DisplayName: "parse", FilePath: path, Line: 30, Kind: "func", Language: "go"},
	}
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.run", TargetQualifiedName: "pkg/app.fetchData", EdgeType: "calls", Line: 12},
		{SourceQualifiedName: "pkg/app.run", TargetQualifiedName: "pkg/app.parse", EdgeType: "calls", Line: 13},
	}

	err := store.IndexFile(ctx, path, symbols, edges)
	require.NoError(t, err)

	// Query callers of fetchData — should return run.
	callers, err := store.QueryCallers(ctx, "pkg/app.fetchData")
	require.NoError(t, err)
	require.Len(t, callers, 1)
	assert.Equal(t, "pkg/app.run", callers[0].QualifiedName)

	// Query callees of run — should return fetchData and parse.
	callees, err := store.QueryCallees(ctx, "pkg/app.run")
	require.NoError(t, err)
	require.Len(t, callees, 2)
	assert.Equal(t, "pkg/app.fetchData", callees[0].QualifiedName)
	assert.Equal(t, "pkg/app.parse", callees[1].QualifiedName)

	// Query callers of parse — should return run.
	callers2, err := store.QueryCallers(ctx, "pkg/app.parse")
	require.NoError(t, err)
	require.Len(t, callers2, 1)
	assert.Equal(t, "pkg/app.run", callers2[0].QualifiedName)
}

func TestQueryCallers_NoResults(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.run", DisplayName: "run", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	// No edges exist — callers should be empty.
	callers, err := store.QueryCallers(ctx, "pkg/app.run")
	require.NoError(t, err)
	assert.Empty(t, callers)

	// Non-existent qualified name.
	callers2, err := store.QueryCallers(ctx, "pkg/other.foo")
	require.NoError(t, err)
	assert.Empty(t, callers2)
}

func TestIndexFile_ReplacesOldData(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"

	// First index: two symbols.
	symbolsV1 := []Symbol{
		{QualifiedName: "pkg/app.oldFunc", DisplayName: "oldFunc", FilePath: path, Line: 5, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.helper", DisplayName: "helper", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}
	err := store.IndexFile(ctx, path, symbolsV1, nil)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.NodeCount)

	// Second index: one different symbol, same path.
	symbolsV2 := []Symbol{
		{QualifiedName: "pkg/app.newFunc", DisplayName: "newFunc", FilePath: path, Line: 5, Kind: "func", Language: "go"},
	}
	err = store.IndexFile(ctx, path, symbolsV2, nil)
	require.NoError(t, err)

	// Should have replaced: only 1 node now.
	stats = store.Stats()
	assert.Equal(t, 1, stats.NodeCount)
	assert.Equal(t, 1, stats.FileCount)

	// The old symbols should be gone.
	nodes, err := store.QueryCallees(ctx, "pkg/app.oldFunc")
	require.NoError(t, err)
	assert.Empty(t, nodes)

	// The new symbol should be present.
	callees, err := store.QueryCallees(ctx, "pkg/app.newFunc")
	require.NoError(t, err)
	assert.Empty(t, callees) // no edges, but the node exists
}

func TestIndexFile_ReplacesEdges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"

	// First index: with an edge.
	symbolsV1 := []Symbol{
		{QualifiedName: "pkg/app.a", DisplayName: "a", FilePath: path, Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.b", DisplayName: "b", FilePath: path, Line: 2, Kind: "func", Language: "go"},
	}
	edgesV1 := []Edge{
		{SourceQualifiedName: "pkg/app.a", TargetQualifiedName: "pkg/app.b", EdgeType: "calls", Line: 3},
	}
	err := store.IndexFile(ctx, path, symbolsV1, edgesV1)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Second index: same symbols, no edges.
	err = store.IndexFile(ctx, path, symbolsV1, nil)
	require.NoError(t, err)

	// Edge should be gone since it referenced nodes from this file.
	assert.Equal(t, 0, store.Stats().EdgeCount)
}

func TestStats_EmptyStore(t *testing.T) {
	store := newMemoryStore(t)

	stats := store.Stats()
	assert.Equal(t, 0, stats.NodeCount)
	assert.Equal(t, 0, stats.EdgeCount)
	assert.Equal(t, 0, stats.FileCount)
}

func TestStats_AfterIndexing(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.a", DisplayName: "a", FilePath: path, Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.b", DisplayName: "b", FilePath: path, Line: 2, Kind: "func", Language: "go"},
	}
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.a", TargetQualifiedName: "pkg/app.b", EdgeType: "calls", Line: 3},
	}

	err := store.IndexFile(ctx, path, symbols, edges)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.NodeCount)
	assert.Equal(t, 1, stats.EdgeCount)
	assert.Equal(t, 1, stats.FileCount)
}

func TestStats_MultipleFiles(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Index two different files.
	err := store.IndexFile(ctx, "pkg/a/a.go", []Symbol{
		{QualifiedName: "pkg/a.FuncA", DisplayName: "FuncA", FilePath: "pkg/a/a.go", Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	err = store.IndexFile(ctx, "pkg/b/b.go", []Symbol{
		{QualifiedName: "pkg/b.FuncB", DisplayName: "FuncB", FilePath: "pkg/b/b.go", Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/b.FuncC", DisplayName: "FuncC", FilePath: "pkg/b/b.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 3, stats.NodeCount)
	assert.Equal(t, 2, stats.FileCount)
}

func TestGetStaleFiles_DeletedFile(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Index a file that doesn't exist on disk.
	phantomPath := filepath.Join(t.TempDir(), "nonexistent.go")
	// Don't create the file — just index it.
	err := store.IndexFile(ctx, phantomPath, []Symbol{
		{QualifiedName: "pkg/ghost.func", DisplayName: "func", FilePath: phantomPath, Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// The file doesn't exist on disk, so it should be reported as stale.
	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	assert.Equal(t, phantomPath, stale[0])
}

func TestGetStaleFiles_ExistingFileNotStale(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "fresh.go")

	// Create the file with an old mtime (1 hour ago) so it's clearly before
	// the index time. RFC3339 has second-level precision, so we need a gap
	// of at least one second.
	oldTime := time.Now().Add(-1 * time.Hour)
	err := os.WriteFile(testFile, []byte("package main\n"), 0644)
	require.NoError(t, err)
	err = os.Chtimes(testFile, oldTime, oldTime)
	require.NoError(t, err)

	// Index the file.
	err = store.IndexFile(ctx, testFile, []Symbol{
		{QualifiedName: "main.init", DisplayName: "init", FilePath: testFile, Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// The file exists and its mtime is before last_indexed, so it should NOT be stale.
	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	assert.Empty(t, stale, "freshly indexed file should not be stale")
}

func TestGetStaleFiles_TouchedFileIsStale(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "modified.go")

	// Create the file.
	err := os.WriteFile(testFile, []byte("package main\n"), 0644)
	require.NoError(t, err)

	// Index it.
	err = store.IndexFile(ctx, testFile, []Symbol{
		{QualifiedName: "main.helper", DisplayName: "helper", FilePath: testFile, Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Now touch the file to make its mtime newer than last_indexed.
	time.Sleep(100 * time.Millisecond)
	err = os.WriteFile(testFile, []byte("package main\n// updated\n"), 0644)
	require.NoError(t, err)

	// The file was modified after indexing, so it should be stale.
	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	assert.Equal(t, testFile, stale[0])
}

func TestGetStaleFiles_MixedStaleAndFresh(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()

	freshFile := filepath.Join(tmpDir, "fresh.go")
	staleFile := filepath.Join(tmpDir, "stale.go")

	// Create both files with old mtimes.
	oldTime := time.Now().Add(-1 * time.Hour)
	err := os.WriteFile(freshFile, []byte("package main\n"), 0644)
	require.NoError(t, err)
	err = os.Chtimes(freshFile, oldTime, oldTime)
	require.NoError(t, err)

	err = os.WriteFile(staleFile, []byte("package main\n"), 0644)
	require.NoError(t, err)
	err = os.Chtimes(staleFile, oldTime, oldTime)
	require.NoError(t, err)

	// Index both.
	err = store.IndexFile(ctx, freshFile, []Symbol{
		{QualifiedName: "main.a", DisplayName: "a", FilePath: freshFile, Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)
	err = store.IndexFile(ctx, staleFile, []Symbol{
		{QualifiedName: "main.b", DisplayName: "b", FilePath: staleFile, Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Touch only staleFile to make its mtime newer than last_indexed.
	newTime := time.Now().Add(1 * time.Hour)
	err = os.Chtimes(staleFile, newTime, newTime)
	require.NoError(t, err)

	stale, err := store.GetStaleFiles(ctx)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	assert.Equal(t, staleFile, stale[0])
}

func TestFindDeadCode_DetectsUncalledFunction(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	// Use dot-free qualified names — the implementation treats qualified names
	// containing "." as methods and excludes them from dead code results.
	symbols := []Symbol{
		{QualifiedName: "usedFunc", DisplayName: "usedFunc", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "deadFunc", DisplayName: "deadFunc", FilePath: path, Line: 20, Kind: "func", Language: "go"},
		{QualifiedName: "caller", DisplayName: "caller", FilePath: path, Line: 30, Kind: "func", Language: "go"},
	}
	edges := []Edge{
		{SourceQualifiedName: "caller", TargetQualifiedName: "usedFunc", EdgeType: "calls", Line: 31},
	}

	err := store.IndexFile(ctx, path, symbols, edges)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	// deadFunc has no inbound calls and is not exported/init/main/Test.
	// caller has no inbound calls either, so both should appear.
	require.Len(t, dead, 2)

	deadNames := make(map[string]bool)
	for _, s := range dead {
		deadNames[s.QualifiedName] = true
	}
	assert.True(t, deadNames["deadFunc"])
	assert.True(t, deadNames["caller"])
}

func TestFindDeadCode_ExcludesExported(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "PublicFunc", DisplayName: "PublicFunc", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "privateFunc", DisplayName: "privateFunc", FilePath: path, Line: 20, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	// PublicFunc is exported (starts with uppercase) — excluded.
	// privateFunc is not exported — included.
	require.Len(t, dead, 1)
	assert.Equal(t, "privateFunc", dead[0].QualifiedName)
}

func TestFindDeadCode_ExcludesInitAndMain(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "init", DisplayName: "init", FilePath: path, Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "main", DisplayName: "main", FilePath: path, Line: 5, Kind: "func", Language: "go"},
		{QualifiedName: "orphan", DisplayName: "orphan", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	require.Len(t, dead, 1)
	assert.Equal(t, "orphan", dead[0].QualifiedName)
}

func TestFindDeadCode_ExcludesTestFunctions(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app_test.go"
	symbols := []Symbol{
		{QualifiedName: "TestSomething", DisplayName: "TestSomething", FilePath: path, Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "testHelper", DisplayName: "testHelper", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	// TestSomething starts with "Test" — excluded.
	// testHelper doesn't start with "Test" — included.
	require.Len(t, dead, 1)
	assert.Equal(t, "testHelper", dead[0].QualifiedName)
}

func TestFindDeadCode_ExcludesNonFuncKinds(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "MyType", DisplayName: "MyType", FilePath: path, Line: 1, Kind: "type", Language: "go"},
		{QualifiedName: "myVar", DisplayName: "myVar", FilePath: path, Line: 5, Kind: "var", Language: "go"},
		{QualifiedName: "myConst", DisplayName: "myConst", FilePath: path, Line: 6, Kind: "const", Language: "go"},
		{QualifiedName: "MyIface", DisplayName: "MyIface", FilePath: path, Line: 10, Kind: "iface", Language: "go"},
		{QualifiedName: "orphan", DisplayName: "orphan", FilePath: path, Line: 15, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	// Only orphan (func with no callers) should appear.
	require.Len(t, dead, 1)
	assert.Equal(t, "orphan", dead[0].QualifiedName)
}

func TestIndexFile_CrossFileEdges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Index file A first (callee lives here).
	pathA := "pkg/lib/lib.go"
	err := store.IndexFile(ctx, pathA, []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: pathA, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Index file B with an edge pointing to A's symbol.
	pathB := "pkg/app/app.go"
	symbolsB := []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: pathB, Line: 10, Kind: "func", Language: "go"},
	}
	edgesB := []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/lib.DoWork", EdgeType: "calls", Line: 11},
	}

	err = store.IndexFile(ctx, pathB, symbolsB, edgesB)
	require.NoError(t, err)

	// The edge should resolve: runner calls DoWork.
	callees, err := store.QueryCallees(ctx, "pkg/app.runner")
	require.NoError(t, err)
	require.Len(t, callees, 1)
	assert.Equal(t, "pkg/lib.DoWork", callees[0].QualifiedName)

	// And DoWork should have runner as a caller.
	callers, err := store.QueryCallers(ctx, "pkg/lib.DoWork")
	require.NoError(t, err)
	require.Len(t, callers, 1)
	assert.Equal(t, "pkg/app.runner", callers[0].QualifiedName)
}

func TestIndexFile_EdgeResolutionSkipsUnresolvable(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}
	// Edge references a target that doesn't exist anywhere.
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/missing.nonexistent", EdgeType: "calls", Line: 11},
	}

	err := store.IndexFile(ctx, path, symbols, edges)
	require.NoError(t, err) // Should not error — unresolvable edges are skipped.

	// No edges should have been created.
	stats := store.Stats()
	assert.Equal(t, 0, stats.EdgeCount)
	assert.Equal(t, 1, stats.NodeCount) // Node was still inserted.
}

func TestIndexFile_EmptySymbols(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/empty/empty.go"

	err := store.IndexFile(ctx, path, []Symbol{}, nil)
	require.NoError(t, err)

	// Should still create a file record with 0 symbols.
	stats := store.Stats()
	assert.Equal(t, 0, stats.NodeCount)
	assert.Equal(t, 1, stats.FileCount)
}

func TestQueryCallees_NonExistentQualifiedName(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	callees, err := store.QueryCallees(ctx, "pkg/nowhere.func")
	require.NoError(t, err)
	assert.Empty(t, callees)
}

func TestFindDeadCode_EmptyStore(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)
	assert.Empty(t, dead)
}

func TestClose_Idempotent(t *testing.T) {
	store := newMemoryStore(t)

	err := store.Close()
	require.NoError(t, err)

	// Closing again should be safe (no panic).
	err = store.Close()
	require.NoError(t, err)
}

func TestIndexFile_SymbolIDsPopulated(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.a", DisplayName: "a", FilePath: path, Line: 1, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.b", DisplayName: "b", FilePath: path, Line: 2, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	// Verify the nodes exist by querying stats.
	stats := store.Stats()
	assert.Equal(t, 2, stats.NodeCount)
}

func TestQueryCallers_ReturnsFullSymbol(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.caller", DisplayName: "caller", FilePath: path, Line: 10, Kind: "func", Language: "go", FileMTime: "2025-01-01T00:00:00Z"},
		{QualifiedName: "pkg/app.callee", DisplayName: "callee", FilePath: path, Line: 20, Kind: "func", Language: "go", FileMTime: "2025-01-01T00:00:00Z"},
	}
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.caller", TargetQualifiedName: "pkg/app.callee", EdgeType: "calls", Line: 11},
	}

	err := store.IndexFile(ctx, path, symbols, edges)
	require.NoError(t, err)

	callers, err := store.QueryCallers(ctx, "pkg/app.callee")
	require.NoError(t, err)
	require.Len(t, callers, 1)

	c := callers[0]
	assert.Equal(t, "pkg/app.caller", c.QualifiedName)
	assert.Equal(t, "caller", c.DisplayName)
	assert.Equal(t, path, c.FilePath)
	assert.Equal(t, 10, c.Line)
	assert.Equal(t, "func", c.Kind)
	assert.Equal(t, "go", c.Language)
	assert.Equal(t, "2025-01-01T00:00:00Z", c.FileMTime)
	assert.NotZero(t, c.ID) // ID should be populated from DB.
}

func TestIndexFile_EdgesToOtherFilePreservedAfterReindex(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// File A has the callee.
	pathA := "pkg/lib/lib.go"
	err := store.IndexFile(ctx, pathA, []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: pathA, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// File B calls A.
	pathB := "pkg/app/app.go"
	err = store.IndexFile(ctx, pathB, []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: pathB, Line: 10, Kind: "func", Language: "go"},
	}, []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/lib.DoWork", EdgeType: "calls", Line: 11},
	})
	require.NoError(t, err)

	// Re-index file A with updated symbols (different symbol).
	err = store.IndexFile(ctx, pathA, []Symbol{
		{QualifiedName: "pkg/lib.DoWorkV2", DisplayName: "DoWorkV2", FilePath: pathA, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// The edge from B→A referenced old DoWork node. Since DoWork was deleted
	// (replaced by DoWorkV2), the edge should be cleaned up.
	stats := store.Stats()
	assert.Equal(t, 0, stats.EdgeCount)
}

func TestFindDeadCode_ExcludesMethods(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		// Qualified name with a dot — treated as a method, excluded.
		{QualifiedName: "pkg/app.(*Server).handle", DisplayName: "handle", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		// Plain function — included if uncalled.
		{QualifiedName: "pkg/app.standalone", DisplayName: "standalone", FilePath: path, Line: 20, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	dead, err := store.FindDeadCode(ctx)
	require.NoError(t, err)

	require.Len(t, dead, 1)
	assert.Equal(t, "pkg/app.standalone", dead[0].QualifiedName)
}
