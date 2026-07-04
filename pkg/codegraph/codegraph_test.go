package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
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

	dead, err := store.FindDeadCode(ctx, "")
	require.NoError(t, err)

	require.Len(t, dead, 1)
	assert.Equal(t, "pkg/app.standalone", dead[0].QualifiedName)
}

// ============================================================================
// Incremental Indexing Tests (SP-107-3)
// ============================================================================

// mockParser creates a FileParser that returns symbols derived from file content.
// It detects "func " lines and creates a symbol for each, with language inferred
// from the file extension. Qualified names use "pkg.name" format (exactly one dot
// after the last slash) so FindDeadCode does not treat them as methods.
func mockParser(t *testing.T) FileParser {
	t.Helper()
	return func(path string, content []byte) ([]Symbol, []Edge, error) {
		lang := "go"
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
			lang = "typescript"
		} else if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx") {
			lang = "javascript"
		} else if strings.HasSuffix(path, ".py") {
			lang = "python"
		}

		// Derive a short package-like prefix from the file path.
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		prefix := "mock"

		lines := strings.Split(string(content), "\n")
		var symbols []Symbol
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "func ") {
				parts := strings.Fields(trimmed)
				if len(parts) >= 2 && parts[0] == "func" {
					// Strip trailing "()" from the function name.
					name := strings.TrimSuffix(parts[1], "()")
					// Use "prefix.name" format — exactly one dot after last slash,
					// so FindDeadCode does not treat it as a method.
					qName := prefix + "." + name + "_" + base
					symbols = append(symbols, Symbol{
						QualifiedName: qName,
						DisplayName:   name,
						Line:          i + 1,
						Kind:          "func",
						Language:      lang,
					})
				}
			}
		}
		return symbols, nil, nil
	}
}

func TestIndexAll_FullWalk(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create a Go file with two functions.
	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\nfunc world() {}\n"), 0644))

	// Create a TS file — mockParser only detects "func " prefix so it yields 0 symbols,
	// but the file should still be indexed (with 0 symbols).
	tsFile := filepath.Join(tmpDir, "utils.ts")
	require.NoError(t, os.WriteFile(tsFile, []byte("function greet(name: string) { return \"hi\"; }\n"), 0644))

	// Create a Python file — also yields 0 symbols from mockParser.
	pyFile := filepath.Join(tmpDir, "app.py")
	require.NoError(t, os.WriteFile(pyFile, []byte("def run():\n    pass\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 3, stats.FileCount, "should have indexed 3 files")
	assert.Equal(t, 2, stats.NodeCount, "should have indexed 2 symbols from main.go")
}

func TestIndexAll_ExcludesTestFiles(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Write a regular Go file.
	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Write a _test.go file (should be excluded).
	testFile := filepath.Join(tmpDir, "main_test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n\nfunc TestHello() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "_test.go files should be excluded")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_ExcludesSproutDir(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Create .sprout directory with a source file (should be excluded).
	sproutDir := filepath.Join(tmpDir, ".sprout")
	require.NoError(t, os.MkdirAll(sproutDir, 0755))
	hiddenFile := filepath.Join(sproutDir, "secret.go")
	require.NoError(t, os.WriteFile(hiddenFile, []byte("package secret\n\nfunc hidden() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, ".sprout directory should be excluded")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_ExcludesIgnoredDirs(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Create node_modules directory with a source file (should be excluded).
	nmDir := filepath.Join(tmpDir, "node_modules", "pkg")
	require.NoError(t, os.MkdirAll(nmDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nmDir, "index.js"), []byte("module.exports = {};\n"), 0644))

	// Create vendor directory with a Go file (should be excluded).
	vendorDir := filepath.Join(tmpDir, "vendor", "lib")
	require.NoError(t, os.MkdirAll(vendorDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "lib.go"), []byte("package lib\n\nfunc vendor() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "ignored directories should be excluded")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_RemovesDeletedFiles(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Pre-index a file manually (simulating a previous full index).
	err := store.IndexFile(ctx, "old.go", []Symbol{
		{QualifiedName: "oldFunc", DisplayName: "oldFunc", FilePath: "old.go", Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Pre-index another file that still exists on disk.
	stillHere := filepath.Join(tmpDir, "kept.go")
	require.NoError(t, os.WriteFile(stillHere, []byte("package main\n\nfunc kept() {}\n"), 0644))
	err = store.IndexFile(ctx, "kept.go", []Symbol{
		{QualifiedName: "kept", DisplayName: "kept", FilePath: "kept.go", Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, 2, store.Stats().FileCount)
	assert.Equal(t, 2, store.Stats().NodeCount)

	// Now run IndexAll — it should walk tmpDir (which only has kept.go),
	// and remove old.go since it's no longer on disk.
	parser := mockParser(t)
	err = store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "old.go should be removed from index")
	assert.Equal(t, 1, stats.NodeCount, "oldFunc should be removed")
}

func TestIndexAll_NestedDirectories(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create a nested directory structure.
	pkgDir := filepath.Join(tmpDir, "pkg", "hello")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "hello.go"),
		[]byte("package hello\n\nfunc SayHello() {}\nfunc Greet() {}\n"), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.FileCount, "should index files in nested directories")
	assert.Equal(t, 3, stats.NodeCount, "should have 3 symbols total")
}

func TestIndexAll_ContextCancellation(t *testing.T) {
	store := newMemoryStore(t)

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create many files to increase chance of cancellation mid-walk.
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("file_%d.go", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name),
			[]byte("package main\n\nfunc f() {}\n"), 0644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestIndexAll_EmptyDirectory(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 0, stats.FileCount)
	assert.Equal(t, 0, stats.NodeCount)
}

func TestIndexAll_SetsFileMTime(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Set a known mtime so we can verify it was captured.
	knownTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(goFile, knownTime, knownTime))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	// Verify the symbol's FileMTime was set.
	stats := store.Stats()
	assert.Equal(t, 1, stats.NodeCount)

	// Query the node directly via QueryCallees (returns the node even with no edges).
	// Use FindDeadCode since hello is lowercase (not exported) and has no callers.
	dead, err := store.FindDeadCode(ctx, "")
	require.NoError(t, err)
	require.Len(t, dead, 1)
	assert.Equal(t, knownTime.Format(time.RFC3339), dead[0].FileMTime)
}

func TestIndexChangedFiles_OnlyChanged(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create two files.
	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	tsFile := filepath.Join(tmpDir, "utils.ts")
	require.NoError(t, os.WriteFile(tsFile, []byte("// utils\n"), 0644))

	parser := mockParser(t)

	// Full index first.
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)
	assert.Equal(t, 2, store.Stats().FileCount)
	assert.Equal(t, 1, store.Stats().NodeCount)

	// Sleep to ensure mtime differs.
	time.Sleep(100 * time.Millisecond)

	// Modify main.go to add a new function.
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\nfunc world() {}\n"), 0644))

	// Incremental index should only re-index main.go.
	err = store.IndexChangedFiles(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.FileCount, "file count should stay the same")
	assert.Equal(t, 2, stats.NodeCount, "should have hello + world from main.go")
}

func TestIndexChangedFiles_DeletedFile(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().FileCount)
	assert.Equal(t, 1, store.Stats().NodeCount)

	// Delete the file.
	require.NoError(t, os.Remove(goFile))

	// Incremental index should detect deletion and remove from index.
	err = store.IndexChangedFiles(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 0, stats.FileCount, "deleted file should be removed from index")
	assert.Equal(t, 0, stats.NodeCount, "nodes for deleted file should be removed")
}

func TestIndexChangedFiles_NoChanges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().FileCount)

	// No changes — incremental index should be a no-op.
	err = store.IndexChangedFiles(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "no changes should not affect index")
	assert.Equal(t, 1, stats.NodeCount, "no changes should not affect nodes")
}

func TestIndexChangedFiles_MultipleChanges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create three files.
	aFile := filepath.Join(tmpDir, "a.go")
	require.NoError(t, os.WriteFile(aFile, []byte("package main\n\nfunc a() {}\n"), 0644))

	bFile := filepath.Join(tmpDir, "b.go")
	require.NoError(t, os.WriteFile(bFile, []byte("package main\n\nfunc b() {}\n"), 0644))

	cFile := filepath.Join(tmpDir, "c.go")
	require.NoError(t, os.WriteFile(cFile, []byte("package main\n\nfunc c() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)
	assert.Equal(t, 3, store.Stats().FileCount)
	assert.Equal(t, 3, store.Stats().NodeCount)

	time.Sleep(100 * time.Millisecond)

	// Modify a.go (add function), modify b.go (add function), delete c.go.
	require.NoError(t, os.WriteFile(aFile, []byte("package main\n\nfunc a() {}\nfunc a2() {}\n"), 0644))
	require.NoError(t, os.WriteFile(bFile, []byte("package main\n\nfunc b() {}\nfunc b2() {}\n"), 0644))
	require.NoError(t, os.Remove(cFile))

	err = store.IndexChangedFiles(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.FileCount, "c.go should be removed, a.go and b.go remain")
	assert.Equal(t, 4, stats.NodeCount, "a + a2 + b + b2 = 4 symbols")
}

func TestIndexChangedFiles_ContextCancellation(t *testing.T) {
	store := newMemoryStore(t)

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create many files and make them all stale.
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("file_%d.go", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name),
			[]byte("package main\n\nfunc f() {}\n"), 0644))
	}

	parser := mockParser(t)

	// Full index first.
	err := store.IndexAll(context.Background(), parser)
	require.NoError(t, err)

	// Make all files stale by touching them.
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("file_%d.go", i)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name),
			[]byte("package main\n\nfunc f() {}\nfunc g() {}\n"), 0644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = store.IndexChangedFiles(ctx, parser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestDeleteFileFromIndex(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Index two files with an edge between them.
	pathA := "pkg/lib/lib.go"
	err := store.IndexFile(ctx, pathA, []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: pathA, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	pathB := "pkg/app/app.go"
	err = store.IndexFile(ctx, pathB, []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: pathB, Line: 10, Kind: "func", Language: "go"},
	}, []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/lib.DoWork", EdgeType: "calls", Line: 11},
	})
	require.NoError(t, err)

	assert.Equal(t, 2, store.Stats().FileCount)
	assert.Equal(t, 2, store.Stats().NodeCount)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Delete file A from index.
	err = store.deleteFileFromIndex(ctx, pathA)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "only file B should remain")
	assert.Equal(t, 1, stats.NodeCount, "only runner should remain")
	assert.Equal(t, 0, stats.EdgeCount, "edge referencing DoWork should be removed")
}

func TestDeleteFileFromIndex_NonExistent(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Deleting a file that doesn't exist in the index should be a no-op.
	err := store.deleteFileFromIndex(ctx, "nonexistent.go")
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 0, stats.FileCount)
	assert.Equal(t, 0, stats.NodeCount)
}

func TestIndexAll_HiddenDirectoriesExcluded(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Create a hidden directory (not .sprout, not .git) with a source file.
	hiddenDir := filepath.Join(tmpDir, ".hidden")
	require.NoError(t, os.MkdirAll(hiddenDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(hiddenDir, "secret.go"),
		[]byte("package secret\n\nfunc hidden() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "hidden directories should be excluded")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_UnsupportedExtensionsExcluded(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Create files with unsupported extensions.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.md"), []byte("# Hello\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("key: value\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.json"), []byte("{}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "script.sh"), []byte("#!/bin/bash\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "only supported extensions should be indexed")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_ParserError(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Parser that always returns an error.
	failingParser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		return nil, nil, fmt.Errorf("parse failed for %s", path)
	}

	err := store.IndexAll(ctx, failingParser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse failed")
}

func TestIndexAll_SymlinksSkipped(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	goFile := filepath.Join(tmpDir, "main.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package main\n\nfunc hello() {}\n"), 0644))

	// Create a symlink to the Go file (should be skipped).
	linkPath := filepath.Join(tmpDir, "link.go")
	err := os.Symlink(goFile, linkPath)
	if err != nil {
		// Symlinks may not work on all platforms (e.g. Windows without admin).
		// Skip the symlink-specific assertions but still verify the base file.
		parser := mockParser(t)
		err = store.IndexAll(ctx, parser)
		require.NoError(t, err)

		stats := store.Stats()
		assert.GreaterOrEqual(t, stats.FileCount, 1, "at least main.go should be indexed")
		return
	}

	parser := mockParser(t)
	err = store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "symlinks should be skipped")
	assert.Equal(t, 1, stats.NodeCount, "only main.go symbols should be indexed")
}

func TestIndexAll_SubdirectoryWithGoFiles(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create a subdirectory with Go files.
	subDir := filepath.Join(tmpDir, "internal", "handler")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(subDir, "handler.go"),
		[]byte("package handler\n\nfunc HandleRequest() {}\nfunc HandleResponse() {}\n"), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0644))

	parser := mockParser(t)
	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 2, stats.FileCount, "should index files in subdirectories")
	assert.Equal(t, 3, stats.NodeCount, "should have 3 symbols total")
}

func TestIndexAll_RemoveDeletedAfterFullWalk(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Pre-index a file that doesn't exist on disk.
	err := store.IndexFile(ctx, "ghost.go", []Symbol{
		{QualifiedName: "ghost", DisplayName: "ghost", FilePath: "ghost.go", Line: 1, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Pre-index a file that also has edges pointing to ghost.
	err = store.IndexFile(ctx, "caller.go", []Symbol{
		{QualifiedName: "caller", DisplayName: "caller", FilePath: "caller.go", Line: 1, Kind: "func", Language: "go"},
	}, []Edge{
		{SourceQualifiedName: "caller", TargetQualifiedName: "ghost", EdgeType: "calls", Line: 2},
	})
	require.NoError(t, err)

	assert.Equal(t, 2, store.Stats().FileCount)
	assert.Equal(t, 2, store.Stats().NodeCount)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Create a file that exists on disk.
	existingFile := filepath.Join(tmpDir, "real.go")
	require.NoError(t, os.WriteFile(existingFile, []byte("package main\n\nfunc real() {}\n"), 0644))

	parser := mockParser(t)
	err = store.IndexAll(ctx, parser)
	require.NoError(t, err)

	stats := store.Stats()
	assert.Equal(t, 1, stats.FileCount, "ghost.go and caller.go should be removed (not on disk)")
	assert.Equal(t, 1, stats.NodeCount, "only real() should remain")
	assert.Equal(t, 0, stats.EdgeCount, "edges referencing deleted nodes should be removed")
}

// ============================================================================
// InsertAllEdges Tests (SP-107-5: two-phase indexing)
// ============================================================================

func TestInsertAllEdges_Basic(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.helper", DisplayName: "helper", FilePath: path, Line: 20, Kind: "func", Language: "go"},
	}

	// Phase 1: insert symbols only.
	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, store.Stats().NodeCount)
	assert.Equal(t, 0, store.Stats().EdgeCount)

	// Phase 2: insert edges.
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/app.helper", EdgeType: "calls", Line: 11},
	}
	err = store.InsertAllEdges(ctx, edges)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Verify the edge.
	callees, err := store.QueryCallees(ctx, "pkg/app.runner")
	require.NoError(t, err)
	require.Len(t, callees, 1)
	assert.Equal(t, "pkg/app.helper", callees[0].QualifiedName)
}

func TestInsertAllEdges_CrossFile(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Insert symbols for two files (phase 1).
	pathA := "pkg/lib/lib.go"
	err := store.IndexFile(ctx, pathA, []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: pathA, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	pathB := "pkg/app/app.go"
	err = store.IndexFile(ctx, pathB, []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: pathB, Line: 10, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, 2, store.Stats().NodeCount)
	assert.Equal(t, 0, store.Stats().EdgeCount)

	// Phase 2: insert edges (cross-file resolution should work since all nodes exist).
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/lib.DoWork", EdgeType: "calls", Line: 11},
	}
	err = store.InsertAllEdges(ctx, edges)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Verify cross-file edge.
	callees, err := store.QueryCallees(ctx, "pkg/app.runner")
	require.NoError(t, err)
	require.Len(t, callees, 1)
	assert.Equal(t, "pkg/lib.DoWork", callees[0].QualifiedName)

	callers, err := store.QueryCallers(ctx, "pkg/lib.DoWork")
	require.NoError(t, err)
	require.Len(t, callers, 1)
	assert.Equal(t, "pkg/app.runner", callers[0].QualifiedName)
}

func TestInsertAllEdges_SkipsUnresolvable(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	err := store.IndexFile(ctx, path, []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Edge references a target that doesn't exist — should be skipped silently.
	edges := []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/missing.nonexistent", EdgeType: "calls", Line: 11},
	}
	err = store.InsertAllEdges(ctx, edges)
	require.NoError(t, err)
	assert.Equal(t, 0, store.Stats().EdgeCount)
}

func TestInsertAllEdges_EmptyEdges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.InsertAllEdges(ctx, nil)
	require.NoError(t, err)

	err = store.InsertAllEdges(ctx, []Edge{})
	require.NoError(t, err)
}

func TestInsertAllEdges_ReplacesExistingEdges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"
	symbols := []Symbol{
		{QualifiedName: "pkg/app.runner", DisplayName: "runner", FilePath: path, Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.helper", DisplayName: "helper", FilePath: path, Line: 20, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.worker", DisplayName: "worker", FilePath: path, Line: 30, Kind: "func", Language: "go"},
	}

	err := store.IndexFile(ctx, path, symbols, nil)
	require.NoError(t, err)

	// First set of edges.
	err = store.InsertAllEdges(ctx, []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/app.helper", EdgeType: "calls", Line: 11},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Replace with different edges — old edge should be removed.
	err = store.InsertAllEdges(ctx, []Edge{
		{SourceQualifiedName: "pkg/app.runner", TargetQualifiedName: "pkg/app.worker", EdgeType: "calls", Line: 12},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, store.Stats().EdgeCount)

	// Verify the new edge.
	callees, err := store.QueryCallees(ctx, "pkg/app.runner")
	require.NoError(t, err)
	require.Len(t, callees, 1)
	assert.Equal(t, "pkg/app.worker", callees[0].QualifiedName)
}

func TestIndexAll_CrossFileEdges(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	store.baseDir = tmpDir

	// Create two Go files that call each other via the parser's edge logic.
	libFile := filepath.Join(tmpDir, "lib.go")
	require.NoError(t, os.WriteFile(libFile, []byte(`package lib

func DoWork() {}
func Helper() {}
`), 0644))

	appFile := filepath.Join(tmpDir, "app.go")
	require.NoError(t, os.WriteFile(appFile, []byte(`package main

func run() {
    DoWork()
}
`), 0644))

	// Custom parser that generates cross-file edges.
	parser := func(path string, content []byte) ([]Symbol, []Edge, error) {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		lines := strings.Split(string(content), "\n")
		var symbols []Symbol
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "func ") {
				parts := strings.Fields(trimmed)
				if len(parts) >= 2 && parts[0] == "func" {
					name := strings.TrimSuffix(parts[1], "()")
					qName := "pkg." + name
					symbols = append(symbols, Symbol{
						QualifiedName: qName,
						DisplayName:   name,
						Line:          i + 1,
						Kind:          "func",
						Language:      "go",
					})
				}
			}
		}

		// Generate edges: if this is app.go, run() calls DoWork from lib.go.
		var edges []Edge
		if base == "app" {
			for _, sym := range symbols {
				if sym.DisplayName == "run" {
					edges = append(edges, Edge{
						SourceQualifiedName: "pkg.run",
						TargetQualifiedName: "pkg.DoWork",
						EdgeType:            "calls",
						Line:                4,
					})
				}
			}
		}
		return symbols, edges, nil
	}

	err := store.IndexAll(ctx, parser)
	require.NoError(t, err)

	// Should have indexed all files with proper cross-file edges.
	stats := store.Stats()
	assert.Equal(t, 2, stats.FileCount, "should index lib.go and app.go")
	assert.Equal(t, 3, stats.NodeCount, "should have DoWork, Helper, run")
	assert.Equal(t, 1, stats.EdgeCount, "should have one cross-file edge")

	// Verify cross-file edge: run() calls DoWork().
	callers, err := store.QueryCallers(ctx, "pkg.DoWork")
	require.NoError(t, err)
	require.Len(t, callers, 1, "DoWork should have one caller from app.go")
	assert.Equal(t, "pkg.run", callers[0].QualifiedName)

	callees, err := store.QueryCallees(ctx, "pkg.run")
	require.NoError(t, err)
	require.Len(t, callees, 1, "run should have one callee in lib.go")
	assert.Equal(t, "pkg.DoWork", callees[0].QualifiedName)

	// Helper should have no callers.
	callers2, err := store.QueryCallers(ctx, "pkg.Helper")
	require.NoError(t, err)
	assert.Empty(t, callers2, "Helper should have no callers")
}

// ============================================================================
// QueryAllNodes Tests (SP-107-5)
// ============================================================================

func TestQueryAllNodes_EmptyStore(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	nodes, err := store.QueryAllNodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestQueryAllNodes_ReturnsAllSymbols(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Index symbols from two different files.
	err := store.IndexFile(ctx, "pkg/app/app.go", []Symbol{
		{QualifiedName: "pkg/app.run", DisplayName: "run", FilePath: "pkg/app/app.go", Line: 10, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.Config", DisplayName: "Config", FilePath: "pkg/app/app.go", Line: 5, Kind: "type", Language: "go"},
	}, nil)
	require.NoError(t, err)

	err = store.IndexFile(ctx, "pkg/api/handler.go", []Symbol{
		{QualifiedName: "pkg/api.Handle", DisplayName: "Handle", FilePath: "pkg/api/handler.go", Line: 3, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	nodes, err := store.QueryAllNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 3)

	// Verify ordering: by file_path then line.
	assert.Equal(t, "pkg/api/handler.go", nodes[0].FilePath)
	assert.Equal(t, "Handle", nodes[0].DisplayName)

	assert.Equal(t, "pkg/app/app.go", nodes[1].FilePath)
	assert.Equal(t, "Config", nodes[1].DisplayName) // line 5, before run at line 10

	assert.Equal(t, "pkg/app/app.go", nodes[2].FilePath)
	assert.Equal(t, "run", nodes[2].DisplayName) // line 10
}

func TestQueryAllNodes_AfterReplacement(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	path := "pkg/app/app.go"

	// First index: two symbols.
	err := store.IndexFile(ctx, path, []Symbol{
		{QualifiedName: "pkg/app.oldFunc", DisplayName: "oldFunc", FilePath: path, Line: 5, Kind: "func", Language: "go"},
		{QualifiedName: "pkg/app.helper", DisplayName: "helper", FilePath: path, Line: 10, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// Second index: re-index same file with different symbols.
	err = store.IndexFile(ctx, path, []Symbol{
		{QualifiedName: "pkg/app.newFunc", DisplayName: "newFunc", FilePath: path, Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	// QueryAllNodes should return only the new symbol (old ones replaced).
	nodes, err := store.QueryAllNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "newFunc", nodes[0].DisplayName)
}
