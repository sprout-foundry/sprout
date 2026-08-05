package embedding

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// An index with no records cannot tell "no such code exists" from "nothing has
// been indexed". Callers that treat an empty result as the former report a
// false negative the agent then acts on, so readiness has to be answerable
// before a query is issued.
func TestReadinessDistinguishesEmptyFromAnswerable(t *testing.T) {
	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: t.TempDir()}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	if r := mgr.Readiness(); r.Initialized || r.CanAnswerQueries() {
		t.Errorf("uninitialized manager reported %+v; want not initialized and not answerable", r)
	}

	store, err := NewHNSWStore(filepath.Join(t.TempDir(), "i.hnsw"), "hash")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	provider := newMockProvider(8)
	mgr.SetForTesting(provider, store, NewIndexManager(provider, store, IndexOptions{}))

	r := mgr.Readiness()
	if !r.Initialized {
		t.Error("initialized manager reported Initialized=false")
	}
	if r.Records != 0 {
		t.Errorf("empty store reported %d records", r.Records)
	}
	if r.CanAnswerQueries() {
		t.Error("an initialized but EMPTY index reported itself answerable — this is exactly the state that turns 'nothing indexed' into 'no such code'")
	}

	if err := store.Store([]VectorRecord{{
		ID: "a.go:Foo#L1", File: "a.go", Name: "Foo", Embedding: make([]float32, 8),
	}}); err != nil {
		t.Fatalf("store record: %v", err)
	}

	r = mgr.Readiness()
	if r.Records != 1 {
		t.Errorf("Records = %d, want 1", r.Records)
	}
	if !r.CanAnswerQueries() {
		t.Errorf("populated index reported not answerable: %+v", r)
	}
}

// Readiness must be one snapshot. Composing IsInitialized/IsBuilding/IndexSize
// reads three separate instants, so a caller can see "idle" and "empty" from
// different moments and conclude the index is done when a build is running.
func TestReadinessIsASingleSnapshot(t *testing.T) {
	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: t.TempDir()}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	store, err := NewHNSWStore(filepath.Join(t.TempDir(), "i.hnsw"), "hash")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	provider := newMockProvider(8)
	mgr.SetForTesting(provider, store, NewIndexManager(provider, store, IndexOptions{}))

	mgr.mu.Lock()
	mgr.building = true
	mgr.mu.Unlock()

	r := mgr.Readiness()
	if !r.Building {
		t.Error("Readiness did not report an in-progress build")
	}
	if r.CanAnswerQueries() {
		t.Error("a building, still-empty index reported itself answerable")
	}
}
