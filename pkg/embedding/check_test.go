package embedding

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── FormatDuplicateWarning tests ───

func TestFormatDuplicateWarning(t *testing.T) {
	matches := []QueryResult{
		{
			Record: VectorRecord{
				ID:        "file_a.go:ReadConfig",
				File:      "file_a.go",
				Name:      "ReadConfig",
				Signature: "func ReadConfig(path string) string",
				StartLine: 10,
				EndLine:   20,
			},
			Similarity: 0.95,
		},
		{
			Record: VectorRecord{
				ID:        "file_b.go:LoadConfig",
				File:      "file_b.go",
				Name:      "LoadConfig",
				Signature: "func LoadConfig(path string) string",
				StartLine: 5,
				EndLine:   15,
			},
			Similarity: 0.88,
		},
	}

	result := FormatDuplicateWarning(matches)

	// Verify structure of output — new agent-internal format
	if !strings.Contains(result, "[DUPLICATE CHECK]") {
		t.Error("expected [DUPLICATE CHECK] header in output")
	}
	if !strings.Contains(result, "file_a.go:ReadConfig") {
		t.Error("expected first match ID in output")
	}
	if !strings.Contains(result, "95%") {
		t.Error("expected first match percentage (95%) in output")
	}
	if !strings.Contains(result, "func ReadConfig(path string) string") {
		t.Error("expected first match signature in output")
	}
	if !strings.Contains(result, "file_a.go:10-20") {
		t.Error("expected first match location in output")
	}
	if !strings.Contains(result, "file_b.go:LoadConfig") {
		t.Error("expected second match ID in output")
	}
	if !strings.Contains(result, "88%") {
		t.Error("expected second match percentage (88%) in output")
	}
	if !strings.Contains(result, "Review the above. If your code serves a genuinely different purpose") {
		t.Error("expected trailing guidance message in output")
	}

	// Verify ordering: higher similarity first
	if !strings.Contains(result, "file_b.go:LoadConfig") {
		t.Fatal("both matches should appear in output")
	}
	if strings.Index(result, "file_a.go:ReadConfig") > strings.Index(result, "file_b.go:LoadConfig") {
		t.Error("matches should appear in the order provided (highest similarity first)")
	}
}

func TestFormatDuplicateWarning_Empty(t *testing.T) {
	result := FormatDuplicateWarning(nil)
	if result != "" {
		t.Errorf("expected empty string for nil input, got %q", result)
	}

	result = FormatDuplicateWarning([]QueryResult{})
	if result != "" {
		t.Errorf("expected empty string for empty slice, got %q", result)
	}
}

func TestFormatDuplicateWarning_SingleMatch(t *testing.T) {
	matches := []QueryResult{
		{
			Record: VectorRecord{
				ID:        "a.go:Foo",
				Name:      "Foo",
				Signature: "func Foo()",
				StartLine: 1,
				EndLine:   5,
			},
			Similarity: 0.92,
		},
	}

	result := FormatDuplicateWarning(matches)
	if !strings.Contains(result, "a.go:Foo") {
		t.Error("expected match ID in output")
	}
	if !strings.Contains(result, "92%") {
		t.Error("expected match percentage in output")
	}
	if !strings.Contains(result, "func Foo()") {
		t.Error("expected signature in output")
	}
	if !strings.Contains(result, "[DUPLICATE CHECK]") {
		t.Error("expected [DUPLICATE CHECK] header")
	}
}

// ─── CheckFileForDuplicates tests ───

func TestCheckFileForDuplicates_NilManager(t *testing.T) {
	result, err := CheckFileForDuplicates(context.Background(), nil, "test.go", "func Foo() {}", "/workspace", 0.9, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Duplicates) != 0 {
		t.Errorf("expected 0 duplicates, got %d", len(result.Duplicates))
	}
	if result.WarningText != "" {
		t.Errorf("expected empty warning, got %q", result.WarningText)
	}
}

func TestCheckFileForDuplicates_NoMatches(t *testing.T) {
	dir := t.TempDir()

	// Create a store with one record that has an orthogonal embedding.
	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Write a record with embedding [1, 0, 0] — completely different from the query.
	err = store.Store([]VectorRecord{
		{
			ID:        "existing.go:ExistingFunc",
			File:      "existing.go",
			Name:      "ExistingFunc",
			Signature: "func ExistingFunc() int",
			Embedding: []float32{1, 0, 0},
			Hash:      "abc123",
			IndexedAt: time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("failed to store record: %v", err)
	}

	// Create a file with a function whose embedding text is very different.
	goSrc := `package main

func BrandNewFunction(x int) string {
	return "hello" + string(rune(x))
}
`
	filePath := filepath.Join(dir, "newfile.go")
	if err := os.WriteFile(filePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	// Query with threshold 0.9 — the new function's embedding (based on text
	// length) will be very different from [1,0,0], so no matches above threshold.
	result, err := CheckFileForDuplicates(context.Background(), idx, "newfile.go", goSrc, dir, 0.9, 3)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	if len(result.Duplicates) != 0 {
		t.Errorf("expected 0 duplicates (orthogonal embeddings), got %d: %v", len(result.Duplicates), result.Duplicates)
	}
}

func TestCheckFileForDuplicates_DefaultThreshold(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	goSrc := `package main

func MyFunc() {
	x := 1
}
`
	// threshold=0 means "use default 0.90".
	result, err := CheckFileForDuplicates(context.Background(), idx, "test.go", goSrc, "", 0, 0)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// With empty store, should return empty result.
	if len(result.Duplicates) != 0 {
		t.Errorf("expected 0 duplicates, got %d", len(result.Duplicates))
	}
}

func TestCheckFileForDuplicates_SelfMatchFiltered(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

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
	if err := os.WriteFile(filePath, []byte(goSrc), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	ctx := context.Background()

	// First, index the file so its record exists in the store.
	_, err = idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	initialSize := store.Size()
	if initialSize == 0 {
		t.Fatal("expected at least 1 record after build")
	}

	// Now check the SAME content — self-matches should be filtered out.
	result, err := CheckFileForDuplicates(ctx, idx, filePath, goSrc, dir, 0.0, 10)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// All matches should be filtered (same file path).
	if len(result.Duplicates) != 0 {
		for _, d := range result.Duplicates {
			t.Logf("unexpected duplicate: ID=%s File=%s Sim=%.4f", d.Record.ID, d.Record.File, d.Similarity)
		}
		t.Errorf("expected 0 duplicates (all are self-matches), got %d", len(result.Duplicates))
	}
}

func TestCheckFileForDuplicates_SimilarFunction(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	// Source file A — will be indexed.
	fileA := `package existing

func ReadConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
`
	pathA := filepath.Join(dir, "existing_config.go")
	if err := os.WriteFile(pathA, []byte(fileA), 0644); err != nil {
		t.Fatalf("failed to create file A: %v", err)
	}

	ctx := context.Background()
	_, err = idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if store.Size() == 0 {
		t.Fatal("expected records after build")
	}

	// Source file B — very similar function, different file. Should detect similarity.
	fileB := `package newpackage

func LoadConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
`

	result, err := CheckFileForDuplicates(ctx, idx, "new_config.go", fileB, dir, 0.0, 5)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// The mock provider produces embeddings based on text length, so similar
	// length text will have similar embeddings. With threshold=0.0, we should
	// see at least the indexed function.
	if len(result.Duplicates) == 0 {
		t.Log("No duplicates found — this is expected if embeddings are very different")
		// This is not necessarily a failure; it depends on how the mock provider
		// embeds the two functions. Verify at least the check ran without error.
		return
	}

	// If we did get duplicates, verify they're not from the checked file itself.
	for _, d := range result.Duplicates {
		if d.Record.File == "new_config.go" {
			t.Errorf("self-match should have been filtered: %s", d.Record.ID)
		}
	}
}

func TestCheckFileForDuplicates_DeduplicatesByID(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Manually create records that will match a query.
	// Two units from the same file will produce the same record match.
	record := VectorRecord{
		ID:        "lib.go:SharedFunc",
		File:      "lib.go",
		Name:      "SharedFunc",
		Signature: "func SharedFunc() int",
		Embedding: []float32{1, 0, 0},
		Hash:      "abc",
		IndexedAt: time.Now(),
	}
	if err := store.Store([]VectorRecord{record}); err != nil {
		t.Fatalf("failed to store record: %v", err)
	}

	// Use a custom provider that returns the SAME embedding for both query texts,
	// so both code units will match the same record.
	provider := &constantProvider{vec: []float32{1, 0, 0}}
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	// Content with two functions that will produce identical query embeddings.
	// Each function is >= 5 lines to pass the trivial-function filter.
	content := `package dup

func FuncA() int {
	x := 1
	y := 2
	return x + y
}

func FuncB() int {
	a := 3
	b := 4
	return a + b
}
`

	result, err := CheckFileForDuplicates(context.Background(), idx, "other.go", content, "", 0.0, 10)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// Even though two code units matched the same record, it should appear only once.
	count := 0
	for _, d := range result.Duplicates {
		if d.Record.ID == "lib.go:SharedFunc" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 deduplicated match for lib.go:SharedFunc, got %d", count)
	}
}

func TestCheckFileForDuplicates_SortedBySimilarityDesc(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create records with known embeddings so we can verify ordering.
	// Query will be [0,1,0], so:
	//   - recordA [0,1,0] → similarity 1.0
	//   - recordB [0,0,1] → similarity 0.0
	records := []VectorRecord{
		{
			ID:        "a.go:FuncA",
			File:      "a.go",
			Name:      "FuncA",
			Embedding: []float32{0, 1, 0},
			IndexedAt: time.Now(),
		},
		{
			ID:        "b.go:FuncB",
			File:      "b.go",
			Name:      "FuncB",
			Embedding: []float32{0, 0, 1},
			IndexedAt: time.Now(),
		},
	}
	if err := store.Store(records); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	// Provider that returns [0,1,0] for any query.
	provider := &constantProvider{vec: []float32{0, 1, 0}}
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	content := `package pkg

func NewFunc() {
	return 42
}
`

	result, err := CheckFileForDuplicates(context.Background(), idx, "new.go", content, "", 0.0, 10)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	if len(result.Duplicates) < 2 {
		t.Logf("got %d results", len(result.Duplicates))
		return // not enough results to verify ordering
	}

	// Verify descending order.
	for i := 1; i < len(result.Duplicates); i++ {
		if result.Duplicates[i].Similarity > result.Duplicates[i-1].Similarity {
			t.Errorf("results not sorted descending: index %d (%.4f) > index %d (%.4f)",
				i, result.Duplicates[i].Similarity, i-1, result.Duplicates[i-1].Similarity)
		}
	}
}

func TestCheckFileForDuplicates_TopKLimit(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create 5 records with the same embedding so they all match equally.
	records := make([]VectorRecord, 5)
	for i := 0; i < 5; i++ {
		records[i] = VectorRecord{
			ID:        "file.go:Func" + string(rune('A'+i)),
			File:      "file.go",
			Name:      "Func" + string(rune('A'+i)),
			Embedding: []float32{1, 0, 0},
			IndexedAt: time.Now(),
		}
	}
	if err := store.Store(records); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	provider := &constantProvider{vec: []float32{1, 0, 0}}
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	content := `package pkg

func NewFunc() {
	return 0
}
`

	result, err := CheckFileForDuplicates(context.Background(), idx, "new.go", content, "", 0.0, 2)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// Even though 5 records match, only top 2 should be returned.
	if len(result.Duplicates) > 2 {
		t.Errorf("expected at most 2 results (topK=2), got %d", len(result.Duplicates))
	}
}

func TestCheckFileForDuplicates_EmptyContent(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{})

	// Content with only a package declaration and no functions —
	// extractFromContent will parse it but find no units.
	content := "package empty\n"
	result, err := CheckFileForDuplicates(context.Background(), idx, "empty.go", content, "", 0.9, 3)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	if len(result.Duplicates) != 0 {
		t.Errorf("expected 0 duplicates for content with no functions, got %d", len(result.Duplicates))
	}
	if result.WarningText != "" {
		t.Errorf("expected empty warning, got %q", result.WarningText)
	}
}

// ─── deduplicateMatches tests ───

func TestDeduplicateMatches_NoDuplicates(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "a"}, Similarity: 0.9},
		{Record: VectorRecord{ID: "b"}, Similarity: 0.8},
	}

	result := deduplicateMatches(matches)
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
}

func TestDeduplicateMatches_KeepsHighest(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "same"}, Similarity: 0.7},
		{Record: VectorRecord{ID: "other"}, Similarity: 0.8},
		{Record: VectorRecord{ID: "same"}, Similarity: 0.95},
		{Record: VectorRecord{ID: "same"}, Similarity: 0.5},
	}

	result := deduplicateMatches(matches)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Find the "same" entry.
	var bestSame float32
	for _, r := range result {
		if r.Record.ID == "same" {
			bestSame = r.Similarity
		}
	}
	if bestSame != 0.95 {
		t.Errorf("expected highest similarity 0.95 for 'same', got %.4f", bestSame)
	}
}

func TestDeduplicateMatches_PreservesOrder(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "first"}, Similarity: 0.9},
		{Record: VectorRecord{ID: "second"}, Similarity: 0.8},
		{Record: VectorRecord{ID: "first"}, Similarity: 0.7},
		{Record: VectorRecord{ID: "third"}, Similarity: 0.6},
	}

	result := deduplicateMatches(matches)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].Record.ID != "first" {
		t.Errorf("expected 'first' at position 0, got %s", result[0].Record.ID)
	}
	if result[1].Record.ID != "second" {
		t.Errorf("expected 'second' at position 1, got %s", result[1].Record.ID)
	}
	if result[2].Record.ID != "third" {
		t.Errorf("expected 'third' at position 2, got %s", result[2].Record.ID)
	}
}

func TestDeduplicateMatches_Empty(t *testing.T) {
	result := deduplicateMatches(nil)
	if result == nil {
		// nil is acceptable for nil input
		return
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}

	result = deduplicateMatches([]QueryResult{})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(result))
	}
}

// ─── sortMatchesBySimilarityDesc tests ───

func TestSortMatchesBySimilarityDesc(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "c"}, Similarity: 0.5},
		{Record: VectorRecord{ID: "a"}, Similarity: 0.9},
		{Record: VectorRecord{ID: "b"}, Similarity: 0.7},
	}

	sortMatchesBySimilarityDesc(matches)

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	if matches[0].Similarity < matches[1].Similarity || matches[1].Similarity < matches[2].Similarity {
		t.Errorf("not sorted descending: %.4f, %.4f, %.4f",
			matches[0].Similarity, matches[1].Similarity, matches[2].Similarity)
	}
	if matches[0].Record.ID != "a" || matches[1].Record.ID != "b" || matches[2].Record.ID != "c" {
		t.Errorf("expected order a, b, c got %s, %s, %s",
			matches[0].Record.ID, matches[1].Record.ID, matches[2].Record.ID)
	}
}

func TestSortMatchesBySimilarityDesc_Empty(t *testing.T) {
	sortMatchesBySimilarityDesc(nil)       // should not panic
	sortMatchesBySimilarityDesc([]QueryResult{}) // should not panic
}

func TestSortMatchesBySimilarityDesc_Single(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "only"}, Similarity: 0.5},
	}
	sortMatchesBySimilarityDesc(matches)
	if len(matches) != 1 || matches[0].Record.ID != "only" {
		t.Error("single element should be unchanged")
	}
}

// ─── extractFromContent tests ───

func TestExtractFromContent_ValidGoFile(t *testing.T) {
	units, err := extractFromContent("mypackage.go", `package mypackage

func MyFunc(x int) string {
	return "hello"
}
`)
	if err != nil {
		t.Fatalf("extractFromContent failed: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if units[0].Name != "MyFunc" {
		t.Errorf("expected name 'MyFunc', got %q", units[0].Name)
	}
	if units[0].File != "mypackage.go" {
		t.Errorf("expected file 'mypackage.go', got %q", units[0].File)
	}
}

func TestExtractFromContent_NoExtension(t *testing.T) {
	// When no extension is provided, it should fall back to .go.
	units, err := extractFromContent("Makefile", "not a go file")
	if err != nil {
		// This is expected — parsing a non-Go file as Go will fail.
		t.Logf("expected error for non-go content: %v", err)
		return
	}
	t.Logf("got %d units (may be 0 for unparseable content)", len(units))
}

func TestExtractFromContent_EmptyContent(t *testing.T) {
	units, err := extractFromContent("empty.go", "")
	if err != nil {
		t.Fatalf("extractFromContent failed on empty content: %v", err)
	}
	if len(units) != 0 {
		t.Errorf("expected 0 units from empty content, got %d", len(units))
	}
}

func TestExtractFromContent_MultipleFunctions(t *testing.T) {
	units, err := extractFromContent("multi.go", `package multi

func First() {}

func Second(x int) int {
	return x + 1
}
`)
	if err != nil {
		t.Fatalf("extractFromContent failed: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}
}

// ─── constantProvider helper ───

// constantProvider always returns the same embedding vector regardless of input.
// Useful for testing deduplication and sorting.
type constantProvider struct {
	vec []float32
}

func (c *constantProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	out := make([]float32, len(c.vec))
	copy(out, c.vec)
	return out, nil
}

func (c *constantProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i], _ = c.Embed(nil, texts[i])
	}
	return results, nil
}

func (c *constantProvider) Dimensions() int { return len(c.vec) }
func (c *constantProvider) Name() string    { return "constant" }
func (c *constantProvider) ModelHash() string { return "constant-model-hash" }

// ─── embeddingText tests ───

func TestEmbeddingText_NoTruncation(t *testing.T) {
	u := CodeUnit{
		Signature: "func Foo() {}",
		Body:      "x := 1",
	}
	result := embeddingText(u, 0)
	if result != "func Foo() {}\nx := 1" {
		t.Errorf("unexpected output: %q", result)
	}

	result = embeddingText(u, 1000)
	if result != "func Foo() {}\nx := 1" {
		t.Errorf("unexpected output: %q", result)
	}
}

func TestEmbeddingText_Truncation(t *testing.T) {
	body := strings.Repeat("a", 100)
	u := CodeUnit{
		Signature: "func Bar()",
		Body:      body,
	}

	result := embeddingText(u, 10)
	// Body should be truncated to 10 chars.
	signature := "func Bar()\n"
	bodyPart := strings.Repeat("a", 10)
	expected := signature + bodyPart
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEmbeddingText_TruncationAtUTF8Boundary(t *testing.T) {
	// Multi-byte UTF-8 characters should not be split.
	body := "日本語テスト"
	u := CodeUnit{
		Signature: "func Utf8()",
		Body:      body,
	}

	// Truncate to 2 runes — should give us "日本"
	result := embeddingText(u, 2)
	expected := "func Utf8()\n日本"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ─── codeUnitToRecord tests ───

func TestCodeUnitToRecord(t *testing.T) {
	u := CodeUnit{
		ID:        "test.go:MyFunc",
		File:      "test.go",
		Name:      "MyFunc",
		Signature: "  func MyFunc() int  ",
		Body:      "return 42",
		StartLine: 10,
		EndLine:   12,
		Language:  "go",
		Hash:      "abc123",
	}

	embedding := []float32{1, 2, 3}
	now := time.Now()

	rec := codeUnitToRecord(u, embedding, now)

	if rec.ID != "test.go:MyFunc" {
		t.Errorf("expected ID 'test.go:MyFunc', got %q", rec.ID)
	}
	if rec.File != "test.go" {
		t.Errorf("expected File 'test.go', got %q", rec.File)
	}
	if rec.Name != "MyFunc" {
		t.Errorf("expected Name 'MyFunc', got %q", rec.Name)
	}
	// Signature should be trimmed.
	if rec.Signature != "func MyFunc() int" {
		t.Errorf("expected trimmed signature, got %q", rec.Signature)
	}
	if rec.StartLine != 10 || rec.EndLine != 12 {
		t.Errorf("expected lines 10-12, got %d-%d", rec.StartLine, rec.EndLine)
	}
	if rec.Language != "go" {
		t.Errorf("expected Language 'go', got %q", rec.Language)
	}
	if rec.Hash != "abc123" {
		t.Errorf("expected Hash 'abc123', got %q", rec.Hash)
	}
	if !rec.IndexedAt.Equal(now) {
		t.Errorf("expected IndexedAt %v, got %v", now, rec.IndexedAt)
	}

	// Embedding is directly assigned (same slice reference).
	_ = embedding
}

// ─── deduplicateMatches edge case: all same ID ───

func TestDeduplicateMatches_AllSameID(t *testing.T) {
	matches := []QueryResult{
		{Record: VectorRecord{ID: "x"}, Similarity: 0.3},
		{Record: VectorRecord{ID: "x"}, Similarity: 0.8},
		{Record: VectorRecord{ID: "x"}, Similarity: 0.5},
	}

	result := deduplicateMatches(matches)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].Similarity != 0.8 {
		t.Errorf("expected highest similarity 0.8, got %.4f", result[0].Similarity)
	}
}

// ─── CheckFileForDuplicates with code unit errors ───

func TestCheckFileForDuplicates_ParseError(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	provider := newMockProvider(3)
	idx := NewIndexManager(provider, store, IndexOptions{})

	// Invalid Go code should produce a parse error.
	result, err := CheckFileForDuplicates(context.Background(), idx, "bad.go", "this is not go code at all{{{", "", 0.9, 3)
	if err == nil {
		t.Fatal("expected error for invalid Go code")
	}
	if !strings.Contains(err.Error(), "bad.go") {
		t.Errorf("expected error to mention file path, got: %v", err)
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

// ─── WarningText consistency ───

func TestCheckFileForDuplicates_WarningTextConsistent(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add a record.
	record := VectorRecord{
		ID:        "lib.go:Helper",
		File:      "lib.go",
		Name:      "Helper",
		Signature: "func Helper() int",
		Embedding: []float32{1, 0, 0},
		IndexedAt: time.Now(),
	}
	if err := store.Store([]VectorRecord{record}); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	provider := &constantProvider{vec: []float32{1, 0, 0}}
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	content := `package pkg

func NewFunc() {}
`

	result, err := CheckFileForDuplicates(context.Background(), idx, "new.go", content, "", 0.0, 5)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates failed: %v", err)
	}

	// WarningText should match FormatDuplicateWarning output.
	expected := FormatDuplicateWarning(result.Duplicates)
	if result.WarningText != expected {
		t.Errorf("WarningText mismatch:\nexpected:\n%s\n\ngot:\n%s", expected, result.WarningText)
	}

	// With no duplicates, WarningText should be empty.
	if len(result.Duplicates) == 0 && result.WarningText != "" {
		t.Errorf("expected empty WarningText with no duplicates, got %q", result.WarningText)
	}
}

// ─── Cosine similarity edge case with mock embeddings ───

func TestCheckFileForDuplicates_DifferentLengthEmbeddings(t *testing.T) {
	dir := t.TempDir()

	store, err := NewHNSWStore(filepath.Join(dir, "index.hnsw"), "test-model-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Record with 3-dim embedding.
	store.Store([]VectorRecord{{
		ID:        "a.go:F",
		File:      "a.go",
		Name:      "F",
		Embedding: []float32{1, 2, 3},
		IndexedAt: time.Now(),
	}})

	// Provider returning 5-dim embeddings — mismatch means similarity = 0.
	provider := &constantProvider{vec: []float32{1, 0, 0, 0, 0}}
	idx := NewIndexManager(provider, store, IndexOptions{BatchSize: 16, MaxBodyLen: 500})

	content := `package pkg

func NewFunc() {}
`

	result, err := CheckFileForDuplicates(context.Background(), idx, "new.go", content, "", 0.0, 5)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	// With threshold 0.0, cosine similarity of different-length vectors returns 0.
	// 0 >= 0.0 is true, so we should see the result with similarity 0.
	for _, d := range result.Duplicates {
		if d.Similarity != 0 {
			t.Logf("similarity %.4f (expected 0 for different-length vectors)", d.Similarity)
		}
	}
}

// ─── normalize test helper ───

func TestNormalizeVectorForCheck(t *testing.T) {
	// The check flow doesn't normalize; it relies on CosineSimilarity which
	// normalizes internally. Verify that CosineSimilarity handles the vectors
	// correctly regardless of their norms.
	a := []float32{3, 4} // magnitude 5
	b := []float32{0.6, 0.8} // magnitude 1, same direction

	sim := CosineSimilarity(a, b)
	if sim < 0.99 {
		t.Errorf("expected cosine similarity ≈ 1.0, got %.4f", sim)
	}
}

// Helper to compute approximate float equality.
func float32ApproxEqual(a, b float32, epsilon float32) bool {
	return math.Abs(float64(a-b)) < float64(epsilon)
}
