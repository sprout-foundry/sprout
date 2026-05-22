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
