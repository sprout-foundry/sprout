package embedding

import (
	"path/filepath"
	"testing"
	"time"
)

func makeTestRecord(id, file, name string, embedding []float32) VectorRecord {
	return VectorRecord{
		ID:        id,
		File:      file,
		Name:      name,
		Signature: "func " + name + "()",
		StartLine: 1,
		EndLine:   10,
		Language:  "go",
		Embedding: embedding,
		Hash:      "abc123",
		IndexedAt: time.Now(),
	}
}

func TestStoreAndQuery(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLFileStore(filepath.Join(dir, "store.jsonl"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Store records with known embeddings.
	recs := []VectorRecord{
		makeTestRecord("1", "a.go", "funcA", []float32{1, 0, 0}),
		makeTestRecord("2", "b.go", "funcB", []float32{0, 1, 0}),
	}
	if err := store.Store(recs); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	if store.Size() != 2 {
		t.Errorf("expected size 2, got %d", store.Size())
	}

	// Query for similarity to [1,0,0] — should match funcA most.
	results, err := store.Query([]float32{1, 0, 0}, 2, 0.0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be funcA (similarity 1.0).
	if results[0].Record.Name != "funcA" {
		t.Errorf("expected first result funcA, got %s", results[0].Record.Name)
	}
}

func TestStoreUpsert(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLFileStore(filepath.Join(dir, "store.jsonl"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Store initial record.
	rec := makeTestRecord("1", "a.go", "funcA", []float32{1, 0, 0})
	if err := store.Store([]VectorRecord{rec}); err != nil {
		t.Fatalf("failed to store record: %v", err)
	}

	// Update with same ID.
	rec.Name = "funcAUpdated"
	rec.Signature = "func funcAUpdated()"
	if err := store.Store([]VectorRecord{rec}); err != nil {
		t.Fatalf("failed to upsert record: %v", err)
	}

	// Should still have exactly 1 record.
	if store.Size() != 1 {
		t.Errorf("expected size 1 after upsert, got %d", store.Size())
	}

	// Verify the record was updated.
	results, err := store.Query([]float32{1, 0, 0}, 1, 0.0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Record.Name != "funcAUpdated" {
		t.Errorf("expected funcAUpdated, got %s", results[0].Record.Name)
	}
}

func TestDeleteByFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLFileStore(filepath.Join(dir, "store.jsonl"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Store records from two files.
	recs := []VectorRecord{
		makeTestRecord("1", "a.go", "funcA", []float32{1, 0, 0}),
		makeTestRecord("2", "a.go", "funcB", []float32{0, 1, 0}),
		makeTestRecord("3", "b.go", "funcC", []float32{0, 0, 1}),
	}
	if err := store.Store(recs); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	if store.Size() != 3 {
		t.Fatalf("expected size 3, got %d", store.Size())
	}

	// Delete all records from a.go.
	if err := store.DeleteByFile("a.go"); err != nil {
		t.Fatalf("failed to delete by file: %v", err)
	}

	if store.Size() != 1 {
		t.Errorf("expected size 1 after delete, got %d", store.Size())
	}

	// Verify only funcC remains.
	results, err := store.Query([]float32{0, 0, 1}, 5, 0.0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results) != 1 || results[0].Record.Name != "funcC" {
		t.Errorf("expected only funcC, got %d results", len(results))
	}
}

func TestStoreReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.jsonl")

	// Create store, store records, close.
	store1, err := NewJSONLFileStore(path)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	recs := []VectorRecord{
		makeTestRecord("1", "a.go", "funcA", []float32{1, 0, 0}),
		makeTestRecord("2", "b.go", "funcB", []float32{0, 1, 0}),
	}
	if err := store1.Store(recs); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	if err := store1.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	// Create new store at same path — records should reload.
	store2, err := NewJSONLFileStore(path)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 2 {
		t.Errorf("expected size 2 after reload, got %d", store2.Size())
	}

	// Verify records are correct.
	results, err := store2.Query([]float32{1, 0, 0}, 2, 0.0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results after reload, got %d", len(results))
	}

	// First should be funcA (most similar to [1,0,0]).
	if results[0].Record.Name != "funcA" {
		t.Errorf("expected first result funcA, got %s", results[0].Record.Name)
	}
}

func TestEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLFileStore(filepath.Join(dir, "store.jsonl"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query an empty store.
	results, err := store.Query([]float32{1, 0, 0}, 5, 0.0)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestStoreNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLFileStore(filepath.Join(dir, "nonexistent.jsonl"))
	if err != nil {
		t.Fatalf("failed to create store for nonexistent file: %v", err)
	}
	defer store.Close()

	if store.Size() != 0 {
		t.Errorf("expected size 0, got %d", store.Size())
	}
}
