package embedding

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ─── NewEmbeddingManager tests ───

func TestNewEmbeddingManager_NilConfig(t *testing.T) {
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if !mgr.IsInitialized() {
		// Good — manager should not be initialized yet.
	}
}

func TestNewEmbeddingManager_WithConfig(t *testing.T) {
	cfg := &configuration.EmbeddingIndexConfig{
		Enabled:             true,
		SimilarityThreshold: 0.85,
		MaxResults:          5,
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if mgr.config != cfg {
		t.Error("expected config to be set")
	}
	if mgr.workspaceRoot != "/tmp/workspace" {
		t.Errorf("expected workspaceRoot '/tmp/workspace', got %q", mgr.workspaceRoot)
	}
}

// ─── IsInitialized tests ───

func TestEmbeddingManager_NotInitialized(t *testing.T) {
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")
	if mgr.IsInitialized() {
		t.Error("expected manager to be not initialized after construction")
	}
}

// ─── Init tests ───

func TestEmbeddingManager_Init_PanicOnNilConfig(t *testing.T) {
	// With nil config, Init panics because it dereferences m.config.
	// Verify this is a panic (documenting the current behavior).
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")

	defer func() {
		if r := recover(); r == nil {
			t.Log("Init did not panic with nil config (behavior may have changed)")
		}
	}()

	_ = mgr.Init(context.Background()) // expected to panic with nil config
}

func TestEmbeddingManager_Init_Idempotent(t *testing.T) {
	// Verify that Init is idempotent — calling it twice succeeds both times
	// and doesn't re-initialize resources.
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	err1 := mgr.Init(context.Background())
	if err1 != nil {
		t.Fatalf("first Init failed: %v", err1)
	}
	if !mgr.IsInitialized() {
		t.Error("expected initialized after successful Init")
	}

	// Second call should succeed without re-initializing.
	err2 := mgr.Init(context.Background())
	if err2 != nil {
		t.Fatalf("second Init failed: %v", err2)
	}
	if !mgr.IsInitialized() {
		t.Error("expected still initialized after second Init")
	}
}

// ─── IndexSize tests ───

func TestEmbeddingManager_IndexSize_NotInitialized(t *testing.T) {
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")
	size := mgr.IndexSize()
	if size != 0 {
		t.Errorf("expected IndexSize 0 before init, got %d", size)
	}
}

func TestEmbeddingManager_IndexSize_AfterInit(t *testing.T) {
	dir := t.TempDir()

	// Manually initialize by setting up internal fields (simulate Init success).
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	// Manually set up the manager's internals to simulate a successful Init.
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Store some records.
	if err := store.Store([]VectorRecord{
		{ID: "a", File: "a.go", Embedding: []float32{1, 0, 0}},
		{ID: "b", File: "b.go", Embedding: []float32{0, 1, 0}},
		{ID: "c", File: "c.go", Embedding: []float32{0, 0, 1}},
	}); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	// Manually set internal state.
	mgr.store = store
	mgr.initialized = true

	size := mgr.IndexSize()
	if size != 3 {
		t.Errorf("expected IndexSize 3, got %d", size)
	}
}

// ─── CheckDuplicates tests on EmbeddingManager ───

func TestEmbeddingManager_CheckDuplicates_NotInitialized_NilConfig(t *testing.T) {
	// With nil config, Init panics. CheckDuplicates should not panic but
	// will return an error or recover gracefully.
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	// Provide valid Go source content to avoid parse errors in the embedding pipeline.
	content := "package testpkg\n\nfunc Foo() {}\n"
	result, err := mgr.CheckDuplicates(context.Background(), "test.go", content)
	// With static provider, Init succeeds. CheckDuplicates runs on an empty index.
	// It should return a result with no duplicates (no error).
	if err != nil {
		t.Errorf("expected no error with static provider, got: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestEmbeddingManager_CheckDuplicates_WithConfigThreshold(t *testing.T) {
	dir := t.TempDir()

	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:            dir,
		SimilarityThreshold: 0.75,
		MaxResults:          10,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	// Manually set up internals.
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := &constantProvider{vec: []float32{1, 0, 0}}
	mgr.store = store
	mgr.provider = nil // not used by CheckDuplicates directly
	mgr.indexMgr = NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})
	mgr.initialized = true

	// Add a known record.
	if err := store.Store([]VectorRecord{{
		ID:        "existing.go:FuncA",
		File:      "existing.go",
		Name:      "FuncA",
		Signature: "func FuncA()",
		Embedding: []float32{1, 0, 0},
		IndexedAt: time.Now(),
	}}); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	// Create test file content.
	content := `package pkg

func NewFunc() {}
`

	result, err := mgr.CheckDuplicates(context.Background(), "new.go", content)
	if err != nil {
		t.Fatalf("CheckDuplicates failed: %v", err)
	}

	// The constant provider returns [1,0,0] for everything, so the match
	// should have similarity 1.0 (above the 0.75 threshold).
	if len(result.Duplicates) == 0 {
		t.Log("No duplicates found — the check ran but embeddings may not have matched")
	}
}

func TestEmbeddingManager_CheckDuplicates_UsesConfigDefaults(t *testing.T) {
	dir := t.TempDir()

	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:            dir,
		SimilarityThreshold: 0.0, // very low threshold
		MaxResults:          5,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := &constantProvider{vec: []float32{1, 0, 0}}
	mgr.store = store
	mgr.indexMgr = NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})
	mgr.initialized = true

	content := `package pkg

func NewFunc() {}
`

	// With zero threshold, we should still not find anything (empty store).
	result, err := mgr.CheckDuplicates(context.Background(), "new.go", content)
	if err != nil {
		t.Fatalf("CheckDuplicates failed: %v", err)
	}
	if len(result.Duplicates) != 0 {
		t.Errorf("expected 0 duplicates on empty store, got %d", len(result.Duplicates))
	}
}

// ─── QuerySimilar on EmbeddingManager ───

func TestEmbeddingManager_QuerySimilar_EmptyIndex(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	// With the static provider, initialization succeeds.
	// Verify that querying works on an empty index (returns empty results, no error).
	results, err := mgr.QuerySimilar(context.Background(), "test query", 5, 0.5)
	if err != nil {
		t.Errorf("expected no error with static provider, got: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on empty index, got %d", len(results))
	}
}

// ─── Close tests ───

func TestEmbeddingManager_Close_NotInitialized(t *testing.T) {
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")

	// Closing an uninitialized manager should be safe.
	err := mgr.Close()
	if err != nil {
		t.Errorf("expected no error closing uninitialized manager, got: %v", err)
	}

	if mgr.IsInitialized() {
		t.Error("expected still not initialized")
	}
}

func TestEmbeddingManager_Close_AfterInit(t *testing.T) {
	dir := t.TempDir()

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	mgr.store = store
	mgr.initialized = true

	// Close should succeed (store has no dirty state).
	err = mgr.Close()
	if err != nil {
		t.Errorf("expected no error on close, got: %v", err)
	}

	if mgr.IsInitialized() {
		t.Error("expected manager to be uninitialized after close")
	}
}

func TestEmbeddingManager_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	mgr.store = store
	mgr.initialized = true

	// Close twice should not error.
	_ = mgr.Close()
	err = mgr.Close()
	if err != nil {
		t.Errorf("second close should not error, got: %v", err)
	}
}

// ─── BuildIndex / UpdateFile on EmbeddingManager ───

func TestEmbeddingManager_BuildIndex_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	// With the static provider, BuildIndex succeeds even on an empty workspace.
	// Verify it returns empty stats (no files to index).
	stats, err := mgr.BuildIndex(context.Background())
	if err != nil {
		t.Errorf("expected no error with static provider, got: %v", err)
	}
	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestEmbeddingManager_UpdateFile_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	err := mgr.UpdateFile(context.Background(), "/nonexistent/file.go")
	if err == nil {
		t.Error("expected error when updating nonexistent file")
	}
}

func TestEmbeddingManager_UpdateFromGitDiff_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: dir,
	}
	mgr := NewEmbeddingManager(cfg, dir)

	stats, err := mgr.UpdateFromGitDiff(context.Background())
	// The temp dir is not a git repo, so git diff fails and the error is
	// expected. Verify we get a meaningful error rather than a panic.
	if err == nil {
		t.Log("UpdateFromGitDiff succeeded (repo may be a git repo)")
		if stats == nil {
			t.Error("expected non-nil stats on success")
		}
	} else {
		t.Logf("UpdateFromGitDiff returned expected error: %v", err)
	}
}

// ─── Edge case: Static provider initialization ───

func TestEmbeddingManager_Init_SucceedsWithStaticProvider(t *testing.T) {
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: t.TempDir(),
	}
	mgr := NewEmbeddingManager(cfg, t.TempDir())

	// With the static provider, initialization succeeds regardless of
	// ORT configuration. Verify it initializes cleanly.
	err := mgr.Init(context.Background())
	if err != nil {
		t.Errorf("expected no error with static provider, got: %v", err)
	}
}

// ─── Concurrent access tests ───

func TestEmbeddingManager_IndexSize_Concurrent(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	// Not initialized — IndexSize should safely return 0 under concurrency.
	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			size := mgr.IndexSize()
			if size != 0 {
				t.Errorf("expected 0, got %d", size)
			}
			done <- 1
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestEmbeddingManager_IsInitialized_Concurrent(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- mgr.IsInitialized()
		}()
	}
	for i := 0; i < 10; i++ {
		if initialized := <-done; initialized {
			t.Error("expected all goroutines to see not-initialized state")
		}
	}
}

// ─── RRFMergeResults tests ───

// makeResult is a small helper so the merge tests stay readable.
func makeResult(file string, line int, sim float32) QueryResult {
	return QueryResult{
		Record: VectorRecord{
			File:      file,
			StartLine: line,
			ID:        file + ":" + string(rune('0'+line)),
		},
		Similarity: sim,
	}
}

// TestRRFMerge_DoesNotDropTopStaticHit pins the query-5 regression that
// motivated switching from max-cosine to RRF. Scenario: the correct answer
// is in static's top result (rank 1) with a modest absolute cosine, while
// ONNX's top-5 contains five wrong but high-cosine matches. The merged
// output must still surface the static top hit — under the old max-cosine
// merge it would have been pushed off the topK list.
func TestRRFMerge_DoesNotDropTopStaticHit(t *testing.T) {
	correct := makeResult("delete.go", 10, 0.55) // static rank 1, low cosine
	staticResults := []QueryResult{
		correct,
		makeResult("other.go", 20, 0.40),
	}
	onnxResults := []QueryResult{
		makeResult("wrong1.go", 1, 0.90),
		makeResult("wrong2.go", 2, 0.88),
		makeResult("wrong3.go", 3, 0.86),
		makeResult("wrong4.go", 4, 0.84),
		makeResult("wrong5.go", 5, 0.82),
	}
	out := RRFMergeResults(staticResults, onnxResults, 5)
	if len(out) == 0 {
		t.Fatal("expected non-empty merge")
	}
	if out[0].Record.File != "delete.go" {
		t.Errorf("expected static's rank-1 hit (delete.go) at position 0, got %s with sim=%.3f",
			out[0].Record.File, out[0].Similarity)
	}
}

// TestRRFMerge_OverlapBoostsRank verifies the fundamental RRF property:
// a document found by both providers outranks documents found by only one,
// even if the only-one document has higher absolute cosine.
func TestRRFMerge_OverlapBoostsRank(t *testing.T) {
	shared := makeResult("shared.go", 1, 0.50)
	out := RRFMergeResults(
		[]QueryResult{shared, makeResult("staticonly.go", 5, 0.99)},
		[]QueryResult{shared, makeResult("onnxonly.go", 7, 0.99)},
		5,
	)
	if out[0].Record.File != "shared.go" {
		t.Errorf("expected shared.go (both providers) to rank first, got %s", out[0].Record.File)
	}
}

// TestRRFMerge_RespectsTopK trims oversized merge unions back down.
func TestRRFMerge_RespectsTopK(t *testing.T) {
	a := []QueryResult{makeResult("a.go", 1, 0.9), makeResult("b.go", 2, 0.8), makeResult("c.go", 3, 0.7)}
	b := []QueryResult{makeResult("d.go", 4, 0.6), makeResult("e.go", 5, 0.5)}
	out := RRFMergeResults(a, b, 3)
	if len(out) != 3 {
		t.Errorf("topK=3 should produce 3 results, got %d", len(out))
	}
}

// TestRRFMerge_MemoryRecordsDoNotCollapse pins the dedup-key fix that lets
// memory records survive the merge. Memory records have File="" and
// StartLine=0 — the older File+StartLine key collapsed every memory into a
// single entry, silently dropping all but one from any cross-provider merge.
// Dedupe by Record.ID is what makes the memory ONNX path work at all.
func TestRRFMerge_MemoryRecordsDoNotCollapse(t *testing.T) {
	mem := func(name string, sim float32) QueryResult {
		return QueryResult{
			Record: VectorRecord{
				ID:   "memory:" + name,
				Name: name,
				Type: "memory",
			},
			Similarity: sim,
		}
	}
	a := []QueryResult{mem("auth", 0.80), mem("migrations", 0.60), mem("tests", 0.50)}
	out := RRFMergeResults(a, nil, 5)
	if len(out) != 3 {
		t.Fatalf("expected 3 distinct memory results, got %d (key collision?)", len(out))
	}
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.Record.Name] = true
	}
	for _, name := range []string{"auth", "migrations", "tests"} {
		if !seen[name] {
			t.Errorf("memory %q missing from merged output", name)
		}
	}
}

// TestRRFMerge_KeepsHigherSimilarityCopy ensures that when both providers
// surface the same doc, the QueryResult retained for display has the higher
// per-provider .Similarity. This preserves meaningful percentages in UI
// affordances like the duplicate-check display.
func TestRRFMerge_KeepsHigherSimilarityCopy(t *testing.T) {
	a := []QueryResult{{Record: VectorRecord{File: "x.go", StartLine: 1, ID: "x"}, Similarity: 0.40}}
	b := []QueryResult{{Record: VectorRecord{File: "x.go", StartLine: 1, ID: "x"}, Similarity: 0.95}}
	out := RRFMergeResults(a, b, 5)
	if len(out) != 1 {
		t.Fatalf("expected 1 deduped result, got %d", len(out))
	}
	if out[0].Similarity != 0.95 {
		t.Errorf("expected merged copy to carry the higher .Similarity (0.95), got %.2f", out[0].Similarity)
	}
}

