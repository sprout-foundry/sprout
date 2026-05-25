package embedding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockProvider produces deterministic embeddings for testing.
type mockProvider struct {
	dims int
}

func (m *mockProvider) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dims)
	for i := range vec {
		vec[i] = float32(len(text)+i) / 1000.0
	}
	return vec, nil
}

func (m *mockProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := m.Embed(nil, t)
		results[i] = v
	}
	return results, nil
}

func (m *mockProvider) Dimensions() int    { return m.dims }
func (m *mockProvider) Name() string       { return "mock" }
func (m *mockProvider) ModelHash() string  { return "mock-model-hash" }
func (m *mockProvider) Close() error       { return nil }

func newMockProvider(dims int) *mockProvider {
	return &mockProvider{dims: dims}
}

func TestBuildIndex(t *testing.T) {
	dir := t.TempDir()

	goSrc := `package main

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
	filePath := filepath.Join(dir, "config.go")
	if err := os.WriteFile(filePath, []byte(goSrc), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	opts := IndexOptions{
		IncludeTests: false,
		BatchSize:    16,
		MaxBodyLen:   500,
	}
	idx := NewIndexManager(provider, store, opts)

	ctx := context.Background()
	stats, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if stats.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", stats.FilesProcessed)
	}

	if stats.UnitsExtracted < 2 {
		t.Errorf("expected at least 2 units extracted, got %d", stats.UnitsExtracted)
	}

	if stats.UnitsEmbedded != stats.UnitsExtracted {
		t.Errorf("expected %d units embedded, got %d", stats.UnitsExtracted, stats.UnitsEmbedded)
	}

	if store.Size() != stats.UnitsEmbedded {
		t.Errorf("expected store size %d, got %d", stats.UnitsEmbedded, store.Size())
	}

	if stats.Duration <= 0 {
		t.Error("expected non-zero duration")
	}
}

func TestUpdateFile(t *testing.T) {
	dir := t.TempDir()

	goSrc := `package main

func ReadConfig(path string) string {
	return ""
}
`
	filePath := filepath.Join(dir, "config.go")
	if err := os.WriteFile(filePath, []byte(goSrc), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	ctx := context.Background()

	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("initial BuildIndex failed: %v", err)
	}

	initialSize := store.Size()
	if initialSize == 0 {
		t.Fatal("initial index should have records")
	}

	newSrc := `package main

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
	if err := os.WriteFile(filePath, []byte(newSrc), 0o644); err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	if err := idx.UpdateFile(ctx, filePath); err != nil {
		t.Fatalf("UpdateFile failed: %v", err)
	}

	if store.Size() != 2 {
		t.Errorf("expected store size 2 after update, got %d", store.Size())
	}
}

func TestQuerySimilar(t *testing.T) {
	dir := t.TempDir()

	goSrc := `package main

func ReadConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
`
	filePath := filepath.Join(dir, "config.go")
	if err := os.WriteFile(filePath, []byte(goSrc), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	ctx := context.Background()
	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	results, err := idx.QuerySimilar(ctx, "ReadConfig", 5, 0.5)
	if err != nil {
		t.Fatalf("QuerySimilar failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least 1 result from QuerySimilar")
	}

	for _, r := range results {
		if r.Similarity < 0.5 {
			t.Errorf("result below threshold: %.4f", r.Similarity)
		}
	}
}

func TestCheckDuplicates(t *testing.T) {
	dir := t.TempDir()

	goSrc := `package main

func ReadConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
`
	filePath := filepath.Join(dir, "config.go")
	if err := os.WriteFile(filePath, []byte(goSrc), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	ctx := context.Background()
	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	results, err := idx.CheckDuplicates(ctx, "ReadConfig", 5, 0)
	if err != nil {
		t.Fatalf("CheckDuplicates failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least 1 result from CheckDuplicates with matching query")
	}

	for _, r := range results {
		if r.Similarity < 0.90 {
			t.Errorf("result below default duplicate threshold: %.4f", r.Similarity)
		}
	}
}

func TestBuildIndexEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{})

	ctx := context.Background()
	stats, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("BuildIndex failed on empty dir: %v", err)
	}

	if stats.FilesProcessed != 0 {
		t.Errorf("expected 0 files processed, got %d", stats.FilesProcessed)
	}
	if stats.UnitsExtracted != 0 {
		t.Errorf("expected 0 units extracted, got %d", stats.UnitsExtracted)
	}
}

// TestBuildIndexCleansUpStaleRecords verifies that BuildIndex removes
// records for files that were deleted from the workspace and for symbols
// that disappear from a re-walked file. Both used to leak because
// Store() is upsert-only and no explicit deletion was being issued.
func TestBuildIndexCleansUpStaleRecords(t *testing.T) {
	dir := t.TempDir()

	// Two files initially; later we delete fileB.go entirely and remove
	// one function from fileA.go.
	srcAInitial := `package main

func Keep() string { return "keep" }

func Remove() string { return "remove" }
`
	srcB := `package main

func InB() string { return "b" }
`
	pathA := filepath.Join(dir, "fileA.go")
	pathB := filepath.Join(dir, "fileB.go")
	if err := os.WriteFile(pathA, []byte(srcAInitial), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := os.WriteFile(pathB, []byte(srcB), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})
	ctx := context.Background()

	// First build: 3 records (Keep, Remove, InB).
	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("first BuildIndex: %v", err)
	}
	if got := store.Size(); got != 3 {
		t.Fatalf("after initial build: got %d records, want 3", got)
	}

	// Mutate the workspace: remove fileB.go entirely, drop Remove() from fileA.go.
	srcAModified := `package main

func Keep() string { return "keep" }
`
	if err := os.WriteFile(pathA, []byte(srcAModified), 0o644); err != nil {
		t.Fatalf("rewrite A: %v", err)
	}
	if err := os.Remove(pathB); err != nil {
		t.Fatalf("remove B: %v", err)
	}

	// Second build: only Keep() should remain.
	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("second BuildIndex: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 1 {
		ids := make([]string, len(all))
		for i, r := range all {
			ids[i] = r.ID
		}
		t.Fatalf("after cleanup: got %d records %v, want 1 (Keep only)", len(all), ids)
	}
	if all[0].Name != "Keep" {
		t.Errorf("remaining record name = %q, want Keep", all[0].Name)
	}
}

// TestBuildIndexManifestDeletedFileCleanup verifies that when the manifest
// optimization skips re-walking all files (because none changed mtime),
// records for files that were deleted from the workspace are still
// cleaned up. Previously the early-return on "all unchanged" skipped this.
func TestBuildIndexManifestDeletedFileCleanup(t *testing.T) {
	dir := t.TempDir()

	pathA := filepath.Join(dir, "fileA.go")
	pathB := filepath.Join(dir, "fileB.go")
	if err := os.WriteFile(pathA, []byte("package main\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("package main\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	manifestPath := filepath.Join(dir, ".manifest.json")
	idx := NewIndexManager(provider, store, IndexOptions{
		BatchSize:    16,
		MaxBodyLen:   500,
		ManifestPath: manifestPath,
	})
	ctx := context.Background()

	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("first BuildIndex: %v", err)
	}
	if got := store.Size(); got != 2 {
		t.Fatalf("after initial build: got %d, want 2", got)
	}

	// Delete fileB.go but don't touch fileA.go's mtime. The manifest will
	// report fileA as unchanged, fileB as deleted, no changed files.
	if err := os.Remove(pathB); err != nil {
		t.Fatalf("remove B: %v", err)
	}

	if _, err := idx.BuildIndex(ctx, dir); err != nil {
		t.Fatalf("second BuildIndex: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 1 || all[0].File != pathA {
		t.Fatalf("after manifest-skipped cleanup: got %d records (%+v), want 1 from fileA", len(all), all)
	}
}

func TestBuildIndexSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestXxx(t *testing.T) {}\n"), 0o644)

	provider := newMockProvider(3)
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	idx := NewIndexManager(provider, store, IndexOptions{
		IncludeTests: false,
		BatchSize:    16,
		MaxBodyLen:   500,
	})

	ctx := context.Background()
	stats, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if stats.FilesProcessed != 2 {
		t.Errorf("expected 2 files processed, got %d", stats.FilesProcessed)
	}

	if stats.UnitsExtracted != 1 {
		t.Errorf("expected 1 unit extracted (Main only), got %d", stats.UnitsExtracted)
	}
}
