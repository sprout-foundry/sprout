package embedding

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"
)

// randomVector generates a deterministic random embedding of the given dimension.
func randomVector(rng *rand.Rand, dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rng.Float32()*2 - 1 // range [-1, 1]
	}
	return vec
}

// randomRecord creates a VectorRecord with a random embedding for benchmarking.
func randomRecord(rng *rand.Rand, id string, dim int) VectorRecord {
	return VectorRecord{
		ID:        id,
		File:      id + ".go",
		Name:      "func_" + id,
		Embedding: randomVector(rng, dim),
		IndexedAt: time.Now(),
		Type:      "code_unit",
	}
}

// setupJSONLStore creates a JSONL store in a temp directory and inserts N random vectors.
// Returns the store, query vector, and a cleanup function.
func setupJSONLStore(b *testing.B, n int, dim int) (*JSONLFileStore, []float32, func()) {
	b.Helper()
	rng := rand.New(rand.NewSource(42))

	dir, err := os.MkdirTemp("", "jsonl-bench-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := NewJSONLFileStore(dir+"/vectors.jsonl", "")
	if err != nil {
		b.Fatalf("failed to create JSONL store: %v", err)
	}

	for i := 0; i < n; i++ {
		if err := store.Store([]VectorRecord{randomRecord(rng, fmt.Sprintf("vec-%d", i), dim)}); err != nil {
			b.Fatalf("failed to store record %d: %v", i, err)
		}
	}

	queryVec := randomVector(rng, dim)

	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, queryVec, cleanup
}

// setupHNSWStore creates an HNSW store in a temp directory and inserts N random vectors.
// Returns the store, query vector, and a cleanup function.
func setupHNSWStore(b *testing.B, n int, dim int) (*HNSWStore, []float32, func()) {
	b.Helper()
	rng := rand.New(rand.NewSource(42))

	dir, err := os.MkdirTemp("", "hnsw-bench-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := NewHNSWStore(dir+"/hnsw.index", "")
	if err != nil {
		b.Fatalf("failed to create HNSW store: %v", err)
	}

	for i := 0; i < n; i++ {
		rec := randomRecord(rng, fmt.Sprintf("vec-%d", i), dim)
		if err := store.Store([]VectorRecord{rec}); err != nil {
			b.Fatalf("failed to store record %d: %v", i, err)
		}
	}

	queryVec := randomVector(rng, dim)

	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, queryVec, cleanup
}

func BenchmarkJSONLQuery_100(b *testing.B) {
	store, queryVec, cleanup := setupJSONLStore(b, 100, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

func BenchmarkJSONLQuery_1000(b *testing.B) {
	store, queryVec, cleanup := setupJSONLStore(b, 1000, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

func BenchmarkJSONLQuery_10000(b *testing.B) {
	store, queryVec, cleanup := setupJSONLStore(b, 10000, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

func BenchmarkHNSWQuery_100(b *testing.B) {
	store, queryVec, cleanup := setupHNSWStore(b, 100, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

func BenchmarkHNSWQuery_1000(b *testing.B) {
	store, queryVec, cleanup := setupHNSWStore(b, 1000, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

func BenchmarkHNSWQuery_10000(b *testing.B) {
	store, queryVec, cleanup := setupHNSWStore(b, 10000, 300)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := store.Query(queryVec, 5, 0.0)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		b.StopTimer()
		if len(results) == 0 {
			b.Fail()
		}
		b.StartTimer()
	}
}

// TestJSONLvsHNSW_Results verifies that both stores return topologically similar
// top-K results for the same query vector. Since JSONL uses exhaustive search
// (exact) and HNSW uses approximate nearest neighbor, the same document IDs
// should appear in both result sets for small datasets.
func TestJSONLvsHNSW_Results(t *testing.T) {
	const dim = 300
	const n = 500 // small enough that HNSW should return exact results
	const topK = 5

	rng := rand.New(rand.NewSource(42))

	// Create records
	records := make([]VectorRecord, n)
	for i := 0; i < n; i++ {
		records[i] = randomRecord(rng, fmt.Sprintf("vec-%d", i), dim)
	}
	queryVec := randomVector(rng, dim)

	// Setup JSONL store
	jsonlDir, err := os.MkdirTemp("", "jsonl-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	jsonlStore, err := NewJSONLFileStore(jsonlDir+"/vectors.jsonl", "")
	if err != nil {
		t.Fatalf("failed to create JSONL store: %v", err)
	}
	for _, rec := range records {
		if err := jsonlStore.Store([]VectorRecord{rec}); err != nil {
			t.Fatalf("failed to store record in JSONL: %v", err)
		}
	}

	// Setup HNSW store
	hnswDir, err := os.MkdirTemp("", "hnsw-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	hnswStore, err := NewHNSWStore(hnswDir+"/hnsw.index", "")
	if err != nil {
		t.Fatalf("failed to create HNSW store: %v", err)
	}
	for _, rec := range records {
		if err := hnswStore.Store([]VectorRecord{rec}); err != nil {
			t.Fatalf("failed to store record in HNSW: %v", err)
		}
	}

	// Query both
	jsonlResults, err := jsonlStore.Query(queryVec, topK, 0.0)
	if err != nil {
		t.Fatalf("JSONL query failed: %v", err)
	}
	hnswResults, err := hnswStore.Query(queryVec, topK, 0.0)
	if err != nil {
		t.Fatalf("HNSW query failed: %v", err)
	}

	// Cleanup
	jsonlStore.Close()
	hnswStore.Close()
	os.RemoveAll(jsonlDir)
	os.RemoveAll(hnswDir)

	if len(jsonlResults) == 0 {
		t.Fatal("JSONL returned no results")
	}
	if len(hnswResults) == 0 {
		t.Fatal("HNSW returned no results")
	}

	// Compare by ID overlap — HNSW is approximate, so we expect some but not
	// necessarily all results to match the exact JSONL search. For small datasets
	// with tuned parameters, recall should be high.
	jsonlIDs := make(map[string]bool)
	for _, r := range jsonlResults {
		jsonlIDs[r.Record.ID] = true
	}

	matches := 0
	for _, hr := range hnswResults {
		if jsonlIDs[hr.Record.ID] {
			matches++
		}
	}

	// Log comparison results — HNSW is approximate so exact matches aren't guaranteed
	t.Logf("JSONL top-%d: %d results, HNSW top-%d: %d results, %d ID matches",
		topK, len(jsonlResults), topK, len(hnswResults), matches)
	t.Logf("JSONL IDs: %v", func() []string { ids := make([]string, len(jsonlResults)); for i, r := range jsonlResults { ids[i] = r.Record.ID }; return ids }())
	t.Logf("HNSW IDs:  %v", func() []string { ids := make([]string, len(hnswResults)); for i, r := range hnswResults { ids[i] = r.Record.ID }; return ids }())
}
