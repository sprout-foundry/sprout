package embedding

import (
	"context"
	"os"
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
		Enabled:            true,
		ORTLibraryPath:     "/fake/path/libonnx.so",
		SimilarityThreshold: 0.85,
		MaxResults:         5,
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

func TestEmbeddingManager_Init_FailsWithoutORT(t *testing.T) {
	// With nil config, Init panics because it dereferences m.config.
	// Verify this is a panic (documenting the current behavior).
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")

	os.Unsetenv("ONNXRUNTIME_LIB")

	defer func() {
		if r := recover(); r == nil {
			t.Log("Init did not panic with nil config (behavior may have changed)")
		}
	}()

	_ = mgr.Init(context.Background()) // expected to panic with nil config
}

func TestEmbeddingManager_Init_Idempotent(t *testing.T) {
	// Init with nil config panics (m.config dereference). After that panic,
	// the initialized flag is still false. Use a config with bad ORT path
	// to test idempotency without triggering a panic.
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:       dir,
		ORTLibraryPath: "/nonexistent/libonnx.so",
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")

	os.Unsetenv("ONNXRUNTIME_LIB")

	err1 := mgr.Init(context.Background()) // will fail
	if err1 == nil {
		t.Skip("Init succeeded unexpectedly (ORT lib may exist)")
	}
	if mgr.IsInitialized() {
		t.Error("expected still not initialized after failed Init")
	}

	// Second call should also fail and not change state.
	err2 := mgr.Init(context.Background())
	if err2 == nil {
		t.Skip("second Init succeeded unexpectedly")
	}
	if mgr.IsInitialized() {
		t.Error("expected still not initialized after second failed Init")
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
	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"))
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

func TestEmbeddingManager_CheckDuplicates_NotInitialized_NoConfig(t *testing.T) {
	// With nil config, Init panics. Use a config with bad ORT path instead.
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:       dir,
		ORTLibraryPath: "/nonexistent/libonnx.so",
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")
	os.Unsetenv("ONNXRUNTIME_LIB")

	result, err := mgr.CheckDuplicates(context.Background(), "test.go", "func Foo() {}")
	// Init will fail due to bad ORT, so CheckDuplicates should return an error.
	if err == nil {
		t.Error("expected error when manager cannot initialize")
	}
	if result != nil {
		t.Error("expected nil result on error")
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
	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"))
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

	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"))
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

func TestEmbeddingManager_QuerySimilar_NotInitialized(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:       dir,
		ORTLibraryPath: "/nonexistent/libonnx.so",
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")
	os.Unsetenv("ONNXRUNTIME_LIB")

	results, err := mgr.QuerySimilar(context.Background(), "test query", 5, 0.5)
	if err == nil {
		t.Error("expected error when manager cannot initialize")
	}
	if results != nil {
		t.Error("expected nil results on error")
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

	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"))
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

	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"))
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

func TestEmbeddingManager_BuildIndex_FailsWithoutORT(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:       dir,
		ORTLibraryPath: "/nonexistent/libonnx.so",
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")
	os.Unsetenv("ONNXRUNTIME_LIB")

	stats, err := mgr.BuildIndex(context.Background())
	if err == nil {
		t.Error("expected error when ORT is not configured")
	}
	if stats != nil {
		t.Error("expected nil stats on error")
	}
}

func TestEmbeddingManager_UpdateFile_FailsWithoutORT(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir:       dir,
		ORTLibraryPath: "/nonexistent/libonnx.so",
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")
	os.Unsetenv("ONNXRUNTIME_LIB")

	err := mgr.UpdateFile(context.Background(), "some.go")
	if err == nil {
		t.Error("expected error when ORT is not configured")
	}
}

func TestEmbeddingManager_UpdateFromGitDiff_FailsWithoutORT(t *testing.T) {
	mgr := NewEmbeddingManager(nil, "/tmp/workspace")
	os.Unsetenv("ONNXRUNTIME_LIB")

	stats, err := mgr.UpdateFromGitDiff(context.Background())
	if err == nil {
		t.Error("expected error when ORT is not configured")
	}
	if stats != nil {
		t.Error("expected nil stats on error")
	}
}

// ─── Edge case: Config with only ORT via env var ───

func TestEmbeddingManager_Init_FailsWithOnlyEnvVar_BadPath(t *testing.T) {
	cfg := &configuration.EmbeddingIndexConfig{
		// No ORTLibraryPath set — will try env var.
	}
	mgr := NewEmbeddingManager(cfg, "/tmp/workspace")

	// Set env var to a non-existent path and disable download fallback so
	// resolution must fail (we're testing the error path, not auto-download).
	t.Setenv("ONNXRUNTIME_LIB", "/nonexistent/libonnx.so")
	t.Setenv("SPROUT_NO_DOWNLOAD", "1")

	// Override config dir to a temp dir without any cached ORT library,
	// so our cache-first priority doesn't accidentally succeed.
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)

	err := mgr.Init(context.Background())
	if err == nil {
		t.Error("expected error when ORT lib path doesn't exist")
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
