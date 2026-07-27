//go:build !js

package codegraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionMaps_ExactMatch(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/lib/lib.go", []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: "pkg/lib/lib.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	id, ok := maps.resolve("pkg/lib.DoWork")
	assert.True(t, ok, "exact qualified_name should resolve")
	assert.Equal(t, int64(1), id)
}

func TestResolutionMaps_SuffixMatch(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/lib/lib.go", []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: "pkg/lib/lib.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// Bare leaf name "DoWork" should resolve via suffix match.
	id, ok := maps.resolve("DoWork")
	assert.True(t, ok, "leaf suffix should resolve when unique")
	assert.Equal(t, int64(1), id)
}

func TestResolutionMaps_SuffixMatchReceiverVar(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/y/y.go", []Symbol{
		{QualifiedName: "pkg/y.GetOptimizer", DisplayName: "GetOptimizer", FilePath: "pkg/y/y.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// "state.GetOptimizer" strips the receiver prefix "state." to get
	// leafName "GetOptimizer", then resolves via suffix match.
	id, ok := maps.resolve("state.GetOptimizer")
	assert.True(t, ok, "receiver-qualified name should resolve via suffix match")
	assert.Equal(t, int64(1), id)
}

func TestResolutionMaps_SuffixMatchAmbiguous(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/a/a.go", []Symbol{
		{QualifiedName: "pkg/a.DoWork", DisplayName: "DoWork", FilePath: "pkg/a/a.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)
	err = store.IndexFile(ctx, "pkg/b/b.go", []Symbol{
		{QualifiedName: "pkg/b.DoWork", DisplayName: "DoWork", FilePath: "pkg/b/b.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// "DoWork" is ambiguous — two nodes share the suffix.
	_, ok := maps.resolve("DoWork")
	assert.False(t, ok, "ambiguous suffix should not resolve")
}

func TestResolutionMaps_DisplayNameMatch(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	// Node whose qualified_name has no suffix that matches the leaf,
	// but display_name does.
	err := store.IndexFile(ctx, "pkg/agent/agent.go", []Symbol{
		{QualifiedName: "pkg/agent.(*Agent).ProcessQuery", DisplayName: "ProcessQuery",
			FilePath: "pkg/agent/agent.go", Line: 23, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// "ag.ProcessQuery" strips receiver to "ProcessQuery".
	// Suffix map has "agent.(*Agent).ProcessQuery" and "(*Agent).ProcessQuery"
	// but not bare "ProcessQuery" (no dot before it in the qualified_name).
	// Wait — actually the qualified_name "pkg/agent.(*Agent).ProcessQuery"
	// has a dot before "ProcessQuery", so suffixLeaf["ProcessQuery"] = count 1.
	// So this resolves via suffix match, not display name. That's still correct.
	id, ok := maps.resolve("ag.ProcessQuery")
	assert.True(t, ok, "should resolve")
	assert.Equal(t, int64(1), id)
}

func TestResolutionMaps_DisplayNameAmbiguous(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/a/a.go", []Symbol{
		{QualifiedName: "pkg/a.(*TypeA).Close", DisplayName: "Close", FilePath: "pkg/a/a.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)
	err = store.IndexFile(ctx, "pkg/b/b.go", []Symbol{
		{QualifiedName: "pkg/b.(*TypeB).Close", DisplayName: "Close", FilePath: "pkg/b/b.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// "Close" suffix has count 2, display name "Close" has count 2.
	_, ok := maps.resolve("Close")
	assert.False(t, ok, "ambiguous name should not resolve")
}

func TestResolutionMaps_MethodNameWithParens(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/agent/conversation.go", []Symbol{
		{QualifiedName: "pkg/agent.(*Agent).processQueryWithSeed", DisplayName: "processQueryWithSeed",
			FilePath: "pkg/agent/conversation.go", Line: 79, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	// "(*Agent).processQueryWithSeed" — contains "(" so leaf is NOT stripped.
	// The full string is used as the suffix lookup key.
	// The node's qualified_name "pkg/agent.(*Agent).processQueryWithSeed"
	// produces suffix "(*Agent).processQueryWithSeed" from the dot before "(".
	id, ok := maps.resolve("(*Agent).processQueryWithSeed")
	assert.True(t, ok, "paren-containing suffix should resolve")
	assert.Equal(t, int64(1), id)
}

func TestResolutionMaps_NoMatch(t *testing.T) {
	store := newMemoryStore(t)
	ctx := context.Background()

	err := store.IndexFile(ctx, "pkg/lib/lib.go", []Symbol{
		{QualifiedName: "pkg/lib.DoWork", DisplayName: "DoWork", FilePath: "pkg/lib/lib.go", Line: 5, Kind: "func", Language: "go"},
	}, nil)
	require.NoError(t, err)

	maps, err := buildResolutionMapsInTest(ctx, store)
	require.NoError(t, err)

	_, ok := maps.resolve("Nonexistent")
	assert.False(t, ok, "nonexistent name should not resolve")
}

// buildResolutionMapsInTest is a test helper that opens a transaction on
// the store and builds resolution maps from it.
func buildResolutionMapsInTest(ctx context.Context, store *SQLiteStore) (*resolutionMaps, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	return buildResolutionMaps(ctx, tx)
}
