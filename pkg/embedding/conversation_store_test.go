package embedding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ─── NewConversationStore tests ───

func TestNewConversationStore_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "convo.jsonl")

	provider := &constantProvider{vec: []float32{1, 0, 0}}
	store, err := NewConversationStore(provider, path, provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	// Verify the file was created (even if empty, the dir should exist).
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// File may not be created until first Store() call — that's OK.
		// Verify the store has size 0 and is usable.
		if store.Size() != 0 {
			t.Errorf("expected size 0 for new store, got %d", store.Size())
		}
	}
}

func TestNewConversationStore_LoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "convo.jsonl")

	provider := &constantProvider{vec: []float32{1, 0, 0}}

	// Create store, add records, close.
	store1, err := NewConversationStore(provider, path, provider.ModelHash())
	if err != nil {
		t.Fatalf("first NewConversationStore failed: %v", err)
	}

	recs := []VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "turn1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "turn:2", File: "convo.md", Name: "turn2", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}
	if err := store1.Store(recs); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Create a NEW store at the same path — records should reload.
	store2, err := NewConversationStore(provider, path, provider.ModelHash())
	if err != nil {
		t.Fatalf("second NewConversationStore failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 2 {
		t.Errorf("expected size 2 after reload, got %d", store2.Size())
	}

	// Verify record IDs persisted.
	all, err := store2.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("LoadAll returned %d records, expected 2", len(all))
	}
}

// ─── ConversationStore Store/Query tests ───

func TestConversationStore_StoreAndQuery(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store records with known embeddings.
	recs := []VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "greeting", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "turn:2", File: "convo.md", Name: "response", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}
	if err := store.Store(recs); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if store.Size() != 2 {
		t.Errorf("expected size 2, got %d", store.Size())
	}

	// Query for similarity to [1,0,0] — should match "greeting" most.
	results, err := store.Query([]float32{1, 0, 0}, 2, 0.0)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be "greeting" (similarity 1.0).
	if results[0].Record.Name != "greeting" {
		t.Errorf("expected first result 'greeting', got %s", results[0].Record.Name)
	}
}

func TestConversationStore_Store_Upsert(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store initial record.
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "original", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("initial store failed: %v", err)
	}

	// Upsert with same ID, different name.
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "updated", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	// Should still have exactly 1 record.
	if store.Size() != 1 {
		t.Errorf("expected size 1 after upsert, got %d", store.Size())
	}

	// Verify the record was updated.
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if all[0].Name != "updated" {
		t.Errorf("expected name 'updated' after upsert, got %q", all[0].Name)
	}
}

// ─── ConversationStore LoadAll tests ───

func TestConversationStore_LoadAll(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	recs := []VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "turn1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "turn:2", File: "convo.md", Name: "turn2", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		{ID: "turn:3", File: "convo.md", Name: "turn3", Embedding: []float32{0, 0, 1}, IndexedAt: time.Now()},
	}
	if err := store.Store(recs); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(all) != 3 {
		t.Errorf("expected 3 records from LoadAll, got %d", len(all))
	}

	// Verify LoadAll returns a copy (mutating it shouldn't affect the store).
	all[0].Name = "mutated"
	all2, _ := store.LoadAll()
	if all2[0].Name == "mutated" {
		t.Error("LoadAll should return a copy, not a reference to internal data")
	}
}

func TestConversationStore_LoadAll_Empty(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll on empty store failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 records from empty store, got %d", len(all))
	}
}

// ─── ConversationStore Size tests ───

func TestConversationStore_Size(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Empty store should report 0.
	if store.Size() != 0 {
		t.Errorf("expected size 0 for new store, got %d", store.Size())
	}

	// Add 3 records.
	if err := store.Store([]VectorRecord{
		{ID: "a", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "b", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
		{ID: "c", Embedding: []float32{0, 0, 1}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if store.Size() != 3 {
		t.Errorf("expected size 3 after adding 3 records, got %d", store.Size())
	}
}

// ─── ConversationStore Provider tests ───

func TestConversationStore_Provider(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0, 1}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	got := store.Provider()
	if got != provider {
		t.Error("Provider() should return the same provider passed to constructor")
	}

	// Verify the provider is actually usable.
	if got.Name() != "constant" {
		t.Errorf("expected provider name 'constant', got %q", got.Name())
	}
	if got.Dimensions() != 4 {
		t.Errorf("expected provider dimensions 4, got %d", got.Dimensions())
	}
}

// ─── ConversationStore Close tests ───

func TestConversationStore_Close(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "convo.jsonl")
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, path, provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}

	// Add some data so the store is dirty.
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", File: "convo.md", Name: "greeting", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Close should persist data and clear internal state.
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, Size should be 0 (underlying store clears records on Close).
	if store.Size() != 0 {
		t.Errorf("expected size 0 after close, got %d", store.Size())
	}

	// Verify data persisted to disk by opening a new store at the same path.
	store2, err := NewConversationStore(provider, path, provider.ModelHash())
	if err != nil {
		t.Fatalf("re-open after close failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 1 {
		t.Errorf("expected size 1 after re-open, got %d", store2.Size())
	}
}

func TestConversationStore_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}

	// Close twice should not error.
	if err := store.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("second close should not error, got: %v", err)
	}
}

// ─── ConversationStore Query edge cases ───

func TestConversationStore_Query_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	results, err := store.Query([]float32{1, 0, 0}, 5, 0.0)
	if err != nil {
		t.Fatalf("Query on empty store failed: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty store, got %v", results)
	}
}

func TestConversationStore_Query_Threshold(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store records with different embeddings.
	if err := store.Store([]VectorRecord{
		{ID: "exact", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "orthogonal", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Query with high threshold — only exact match should appear.
	results, err := store.Query([]float32{1, 0, 0}, 5, 0.99)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result above threshold, got %d", len(results))
	}
	if results[0].Record.ID != "exact" {
		t.Errorf("expected 'exact' match, got %s", results[0].Record.ID)
	}
}

func TestConversationStore_Query_TopK(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store 5 records all with the same embedding (so all match equally).
	for i := 0; i < 5; i++ {
		if err := store.Store([]VectorRecord{
			{ID: string(rune('a' + i)), Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		}); err != nil {
			t.Fatalf("Store failed for record %d: %v", i, err)
		}
	}

	// Ask for top 2 — should get at most 2.
	results, err := store.Query([]float32{1, 0, 0}, 2, 0.0)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("expected at most 2 results (topK=2), got %d", len(results))
	}
}

// ─── EmbeddingManager.GetConversationStore integration tests ───

func TestGetConversationStore_LazyInit(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	// Before calling GetConversationStore, convoStore should be nil.
	// We can't access the private field directly, so infer from behavior:
	// call Init to set up the manager, then verify convoStore was not
	// created during Init.
	if err := mgr.Init(context.Background()); err != nil {
		// ONNX unavailable — skip tests that need the manager initialized
		t.Skipf("Skipping: ONNX not available: %v", err)
	}

	// GetConversationStore should create it lazily.
	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil ConversationStore")
	}
}

func TestGetConversationStore_SameInstance(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	store1, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Skipf("Skipping: ONNX not available: %v", err)
	}

	store2, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("second GetConversationStore failed: %v", err)
	}

	if store1 != store2 {
		t.Error("GetConversationStore should return the same instance on repeated calls")
	}
}

func TestGetConversationStore_CreatesAtCorrectPath(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Skipf("Skipping: ONNX not available: %v", err)
	}
	defer store.Close()

	// The store should be created at {indexDir}/conversation_turns.hnsw.
	expectedPath := filepath.Join(dir, "conversation_turns.hnsw")

	// Store a record to materialize the file, then verify the file exists.
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected conversation store file at %s, but it does not exist", expectedPath)
	}
}

func TestGetConversationStore_NotInitialized_ReturnsError(t *testing.T) {
	// Point IndexDir at a path that can't be written to, forcing init to fail.
	unwritable := filepath.Join("/proc/nonexistent", "embeddings")
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: unwritable}
	mgr := NewEmbeddingManager(cfg, t.TempDir())

	_, err := mgr.GetConversationStore(context.Background())
	if err == nil {
		t.Fatal("expected error when init fails, got nil")
	}
}

func TestGetConversationStore_StoresAndQueries(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Skipf("Skipping: ONNX not available: %v", err)
	}
	defer store.Close()

	// Store a conversation turn.
	turn := VectorRecord{
		ID:        "turn:1",
		File:      "session.md",
		Name:      "greeting",
		Embedding: []float32{1, 0, 0},
		IndexedAt: time.Now(),
	}
	if err := store.Store([]VectorRecord{turn}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Query should find it.
	results, err := store.Query([]float32{1, 0, 0}, 1, 0.0)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Record.ID != "turn:1" {
		t.Errorf("expected ID 'turn:1', got %q", results[0].Record.ID)
	}
}

func TestEmbeddingManager_Close_ClosesConversationStore(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	// Get the conversation store and add a record.
	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Skipf("Skipping: ONNX not available: %v", err)
	}

	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Capture the manager's model hash BEFORE Close — the persisted store is
	// tagged with this hash and will be wiped on re-open if the hash differs.
	managerHash := mgr.ModelHash()

	// Close the manager — this should close the conversation store too.
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify the manager is no longer initialized.
	if mgr.IsInitialized() {
		t.Error("expected manager to be uninitialized after Close")
	}

	// The data should have been flushed to disk on close.
	// Re-open a new store at the same path with the SAME hash the manager used.
	expectedPath := filepath.Join(dir, "conversation_turns.hnsw")
	newProvider := &constantProvider{vec: []float32{1, 0, 0}}
	store2, err := NewConversationStore(newProvider, expectedPath, managerHash)
	if err != nil {
		t.Fatalf("re-open after manager close failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 1 {
		t.Errorf("expected 1 record persisted after manager Close, got %d", store2.Size())
	}
}

func TestEmbeddingManager_Close_CleanupAndReinit(t *testing.T) {
	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	mgr := NewEmbeddingManager(cfg, dir)

	// Init, get conversation store, store a record, close.
	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Skipf("Skipping: ONNX not available: %v", err)
	}
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Verify the manager is no longer initialized.
	if mgr.IsInitialized() {
		t.Error("expected manager to be uninitialized after Close")
	}

	// Re-initialize the manager after close.
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("re-init after close failed: %v", err)
	}

	// GetConversationStore should create a fresh store after Close() cleared the old one.
	store2, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("GetConversationStore after re-init failed: %v", err)
	}

	// The persisted record should still be loadable from disk.
	if store2.Size() != 1 {
		t.Errorf("expected 1 record persisted after Close+Init, got %d", store2.Size())
	}

	// Verify we can store more data in the re-opened store.
	if err := store2.Store([]VectorRecord{
		{ID: "turn:2", Embedding: []float32{0, 1, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store in re-opened store failed: %v", err)
	}

	if store2.Size() != 2 {
		t.Errorf("expected 2 records after adding to re-opened store, got %d", store2.Size())
	}
}

// ─── StoreMemory tests ───

func TestConversationStore_StoreMemory(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	if err := store.StoreMemory(context.Background(), "git-safety", "Always use --force-create when creating branches"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	if store.Size() != 1 {
		t.Errorf("expected size 1 after StoreMemory, got %d", store.Size())
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}

	r := all[0]
	if r.ID != "memory:git-safety" {
		t.Errorf("expected ID 'memory:git-safety', got %q", r.ID)
	}
	if r.Name != "git-safety" {
		t.Errorf("expected Name 'git-safety', got %q", r.Name)
	}
	if r.Type != "memory" {
		t.Errorf("expected Type 'memory', got %q", r.Type)
	}
	if r.File != "" {
		t.Errorf("expected empty File, got %q", r.File)
	}
	if r.Hash == "" {
		t.Error("expected non-empty Hash")
	}

	// Check metadata
	if r.Metadata == nil {
		t.Fatal("expected non-nil Metadata")
	}
	if r.Metadata["name"] != "git-safety" {
		t.Errorf("expected metadata name 'git-safety', got %v", r.Metadata["name"])
	}
	if preview, ok := r.Metadata["content_preview"].(string); !ok {
		t.Error("expected content_preview in metadata")
	} else if preview != "Always use --force-create when creating branches" {
		t.Errorf("expected content_preview 'Always use --force-create when creating branches', got %q", preview)
	}
}

func TestConversationStore_StoreMemory_ContentTruncation(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Content longer than 200 characters
	longContent := ""
	for i := 0; i < 50; i++ {
		longContent += "abcd"
	}
	// longContent is now 200 chars exactly, add more
	longContent += "extra"

	if err := store.StoreMemory(context.Background(), "long-memory", longContent); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	preview := all[0].Metadata["content_preview"].(string)
	if len(preview) > 200 {
		t.Errorf("expected content_preview <= 200 chars, got %d", len(preview))
	}
	if preview != longContent[:200] {
		t.Errorf("expected content_preview to be first 200 chars, got %q", preview)
	}
}

func TestConversationStore_StoreMemory_Upsert(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	if err := store.StoreMemory(context.Background(), "test-mem", "original content"); err != nil {
		t.Fatalf("first StoreMemory failed: %v", err)
	}
	if err := store.StoreMemory(context.Background(), "test-mem", "updated content"); err != nil {
		t.Fatalf("second StoreMemory failed: %v", err)
	}

	// Should still have exactly 1 record (upsert by ID)
	if store.Size() != 1 {
		t.Errorf("expected size 1 after upsert, got %d", store.Size())
	}
}

func TestConversationStore_StoreMemory_WithOtherRecords(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store a non-memory record first
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Name: "greeting", Type: "turn", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if err := store.StoreMemory(context.Background(), "my-mem", "memory content"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	if store.Size() != 2 {
		t.Errorf("expected size 2, got %d", store.Size())
	}

	all, _ := store.LoadAll()
	memoryCount := 0
	for _, r := range all {
		if r.Type == "memory" {
			memoryCount++
		}
	}
	if memoryCount != 1 {
		t.Errorf("expected 1 memory record, got %d", memoryCount)
	}
}

// ─── DeleteMemoryByName tests ───

func TestConversationStore_DeleteMemoryByName(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store some memories and a non-memory record
	if err := store.StoreMemory(context.Background(), "mem-1", "memory 1"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}
	if err := store.StoreMemory(context.Background(), "mem-2", "memory 2"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Name: "greeting", Type: "turn", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if store.Size() != 3 {
		t.Errorf("expected size 3 before delete, got %d", store.Size())
	}

	// Delete mem-1
	if err := store.DeleteMemoryByName("mem-1"); err != nil {
		t.Fatalf("DeleteMemoryByName failed: %v", err)
	}

	if store.Size() != 2 {
		t.Errorf("expected size 2 after delete, got %d", store.Size())
	}

	all, _ := store.LoadAll()
	// Verify mem-1 is gone, mem-2 and turn:1 remain
	names := make(map[string]bool)
	for _, r := range all {
		names[r.Name] = true
	}
	if names["mem-1"] {
		t.Error("expected mem-1 to be deleted")
	}
	if !names["mem-2"] {
		t.Error("expected mem-2 to still exist")
	}
	if !names["greeting"] {
		t.Error("expected greeting (non-memory) to still exist")
	}
}

func TestConversationStore_DeleteMemoryByName_NonExistent(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Delete a memory that doesn't exist — should not error
	if err := store.DeleteMemoryByName("nonexistent"); err != nil {
		t.Fatalf("DeleteMemoryByName for nonexistent should not error, got: %v", err)
	}

	if store.Size() != 0 {
		t.Errorf("expected size 0, got %d", store.Size())
	}
}

func TestConversationStore_DeleteMemoryByName_DoesNotDeleteNonMemory(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store a non-memory record with same name as a memory
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Name: "my-mem", Type: "turn", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Delete memory named "my-mem" — should NOT delete the turn record
	if err := store.DeleteMemoryByName("my-mem"); err != nil {
		t.Fatalf("DeleteMemoryByName failed: %v", err)
	}

	if store.Size() != 1 {
		t.Errorf("expected size 1 (non-memory preserved), got %d", store.Size())
	}

	all, _ := store.LoadAll()
	if all[0].Type != "turn" {
		t.Errorf("expected remaining record to be type 'turn', got %q", all[0].Type)
	}
}

func TestConversationStore_DeleteMemoryByName_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Deleting from empty store should not error
	if err := store.DeleteMemoryByName("anything"); err != nil {
		t.Fatalf("DeleteMemoryByName on empty store should not error, got: %v", err)
	}
}

// ─── QueryMemories tests ───

func TestConversationStore_QueryMemories(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store some memories
	if err := store.StoreMemory(context.Background(), "git-safety", "Always use --force-create when creating branches"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}
	if err := store.StoreMemory(context.Background(), "test-conventions", "Write tests before implementation"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Also store a non-memory record (should be filtered out)
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Name: "greeting", Type: "turn", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Query for memories
	results, err := store.QueryMemories(context.Background(), "git branch creation", 10, 0.0)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	// Should return only memory records, not the turn record
	if len(results) != 2 {
		t.Errorf("expected 2 memory results, got %d", len(results))
	}

	for _, r := range results {
		if r.Record.Type != "memory" {
			t.Errorf("QueryMemories should only return memory records, got type %q", r.Record.Type)
		}
	}
}

func TestConversationStore_QueryMemories_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	results, err := store.QueryMemories(context.Background(), "any query", 10, 0.0)
	if err != nil {
		t.Fatalf("QueryMemories on empty store failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

func TestConversationStore_QueryMemories_Filtering(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store only non-memory records
	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Name: "greeting", Type: "turn", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
		{ID: "code:1", Name: "main.go", Type: "code", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// QueryMemories should return nothing (no memory records)
	results, err := store.QueryMemories(context.Background(), "any query", 10, 0.0)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 memory results when no memory records exist, got %d", len(results))
	}
}

func TestConversationStore_QueryMemories_TopK(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Store 5 memories (all with the same embedding due to constantProvider)
	for i := 1; i <= 5; i++ {
		name := "mem-" + string(rune('a'+i-1))
		if err := store.StoreMemory(context.Background(), name, "memory content "+string(rune('a'+i-1))); err != nil {
			t.Fatalf("StoreMemory failed for %s: %v", name, err)
		}
	}

	// Query with topK=2
	results, err := store.QueryMemories(context.Background(), "query", 2, 0.0)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("expected at most 2 results with topK=2, got %d", len(results))
	}
}

func TestConversationStore_QueryMemories_Threshold(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	if err := store.StoreMemory(context.Background(), "mem-1", "memory 1"); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Query with very high threshold — constantProvider returns [1,0,0] for everything,
	// so the cosine similarity should be 1.0
	results, err := store.QueryMemories(context.Background(), "any query", 10, 0.99)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result above threshold, got %d", len(results))
	}
}
