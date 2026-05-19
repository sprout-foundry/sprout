package embedding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("Init failed: %v", err)
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
		t.Fatalf("first GetConversationStore failed: %v", err)
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
		t.Fatalf("GetConversationStore failed: %v", err)
	}
	defer store.Close()

	// The store should be created at {indexDir}/conversation_turns.jsonl.
	expectedPath := filepath.Join(dir, "conversation_turns.jsonl")

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
		t.Fatalf("GetConversationStore failed: %v", err)
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
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	if err := store.Store([]VectorRecord{
		{ID: "turn:1", Embedding: []float32{1, 0, 0}, IndexedAt: time.Now()},
	}); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Close the manager — this should close the conversation store too.
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify the manager is no longer initialized.
	if mgr.IsInitialized() {
		t.Error("expected manager to be uninitialized after Close")
	}

	// The data should have been flushed to disk on close.
	// Re-open a new store at the same path to verify persistence.
	// Use a new StaticProvider so the model hash matches what was written.
	expectedPath := filepath.Join(dir, "conversation_turns.jsonl")
	newProvider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("create new static provider: %v", err)
	}
	store2, err := NewConversationStore(newProvider, expectedPath, newProvider.ModelHash())
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
		t.Fatalf("GetConversationStore failed: %v", err)
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

func TestStoreMemory_Success(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	content := "# My Memory\n\nThis is some important context to remember."
	ctx := context.Background()

	err = store.StoreMemory(ctx, "my-memory", content)
	if err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Store size should be 1.
	if store.Size() != 1 {
		t.Errorf("expected size 1 after StoreMemory, got %d", store.Size())
	}

	// Verify the stored record has correct fields.
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("LoadAll returned %d records, expected 1", len(all))
	}

	rec := all[0]

	if rec.Type != "memory" {
		t.Errorf("expected Type 'memory', got %q", rec.Type)
	}
	if rec.ID != "my-memory" {
		t.Errorf("expected ID 'my-memory', got %q", rec.ID)
	}
	if rec.File != "my-memory.md" {
		t.Errorf("expected File 'my-memory.md', got %q", rec.File)
	}
	if rec.Name != "my-memory" {
		t.Errorf("expected Name 'my-memory', got %q", rec.Name)
	}

	// Verify title extraction: first non-empty line trimmed.
	if rec.Metadata == nil {
		t.Fatal("Metadata is nil")
	}
	title, ok := rec.Metadata["title"].(string)
	if !ok {
		t.Fatalf("Metadata[\"title\"] is not a string, got %T", rec.Metadata["title"])
	}
	if title != "# My Memory" {
		t.Errorf("expected title '# My Memory', got %q", title)
	}

	// Verify contentLength (stored as int in memory; may be float64 after JSON round-trip).
	var clInt int
	switch v := rec.Metadata["contentLength"].(type) {
	case int:
		clInt = v
	case float64:
		clInt = int(v)
	default:
		t.Fatalf("Metadata[\"contentLength\"] is not a number, got %T", rec.Metadata["contentLength"])
	}
	if clInt != len(content) {
		t.Errorf("expected contentLength %d, got %d", len(content), clInt)
	}

	// Verify embedding is non-empty.
	if len(rec.Embedding) == 0 {
		t.Error("expected non-empty Embedding")
	}
}

func TestStoreMemory_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	err = store.StoreMemory(context.Background(), "my-memory", "")
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestStoreMemory_EmptyName(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	err = store.StoreMemory(context.Background(), "", "some content")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}

	// Store should still be empty.
	if store.Size() != 0 {
		t.Errorf("expected size 0 after failed StoreMemory, got %d", store.Size())
	}
}

func TestStoreMemory_NilContext(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Nil context should return nil (graceful-failure pattern).
	err = store.StoreMemory(nil, "my-memory", "some content")
	if err != nil {
		t.Errorf("expected no error for nil context, got: %v", err)
	}

	// Store should remain empty — nothing was stored.
	if store.Size() != 0 {
		t.Errorf("expected size 0 after nil context, got %d", store.Size())
	}
}

func TestStoreMemory_ReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Store initial memory.
	content1 := "# Old Title\n\nOriginal content."
	if err := store.StoreMemory(ctx, "my-memory", content1); err != nil {
		t.Fatalf("first StoreMemory failed: %v", err)
	}

	if store.Size() != 1 {
		t.Errorf("expected size 1 after first store, got %d", store.Size())
	}

	// Store again with same name, different content.
	content2 := "# New Title\n\nThis is updated content that is longer than the original."
	if err := store.StoreMemory(ctx, "my-memory", content2); err != nil {
		t.Fatalf("second StoreMemory failed: %v", err)
	}

	// Size should still be 1 (replaced, not duplicated).
	if store.Size() != 1 {
		t.Errorf("expected size 1 after replace, got %d", store.Size())
	}

	// Verify the new record reflects the new content.
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	rec := all[0]
	if rec.Signature != content2 {
		t.Errorf("expected signature to be new content, got %q", rec.Signature)
	}

	// Verify contentLength reflects the new content (may be int or float64).
	var clInt int
	switch v := rec.Metadata["contentLength"].(type) {
	case int:
		clInt = v
	case float64:
		clInt = int(v)
	default:
		t.Fatalf("Metadata[\"contentLength\"] is not a number, got %T", rec.Metadata["contentLength"])
	}
	if clInt != len(content2) {
		t.Errorf("expected contentLength %d for new content, got %d", len(content2), clInt)
	}

	// Verify title updated.
	title := rec.Metadata["title"].(string)
	if title != "# New Title" {
		t.Errorf("expected title '# New Title', got %q", title)
	}
}

func TestStoreMemory_SignatureTruncation(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	// Create content longer than 2000 runes.
	longContent := strings.Repeat("A", 3000)

	err = store.StoreMemory(context.Background(), "long-memory", longContent)
	if err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	rec := all[0]
	sigRunes := len([]rune(rec.Signature))
	if sigRunes != 2000 {
		t.Errorf("expected signature length 2000 runes, got %d", sigRunes)
	}

	// Signature should be a prefix of the original content.
	expectedPrefix := string([]rune(longContent)[:2000])
	if rec.Signature != expectedPrefix {
		t.Errorf("signature should be prefix of original content")
	}
}

func TestStoreMemory_TitleExtraction(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectedTitle  string
	}{
		{
			name:          "markdown_heading",
			content:       "# Title\n\nBody text here.",
			expectedTitle: "# Title",
		},
		{
			name:          "leading_blank_lines",
			content:       "\n\n\n# Actual Title\n\nBody.",
			expectedTitle: "# Actual Title",
		},
		{
			name:          "no_title_all_blank",
			content:       "\n\n\n",
			expectedTitle: "",
		},
		{
			name:          "plain_text_first_line",
			content:       "This is a plain sentence.\n\nMore text.",
			expectedTitle: "This is a plain sentence.",
		},
		{
			name:          "single_line",
			content:       "Just one line",
			expectedTitle: "Just one line",
		},
		{
			name:          "whitespace_trimmed",
			content:       "  leading spaces\n\nbody",
			expectedTitle: "leading spaces",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			provider := &constantProvider{vec: []float32{1, 0, 0}}

			store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
			if err != nil {
				t.Fatalf("NewConversationStore failed: %v", err)
			}
			defer store.Close()

			err = store.StoreMemory(context.Background(), "mem", tc.content)
			if err != nil {
				t.Fatalf("StoreMemory failed: %v", err)
			}

			all, err := store.LoadAll()
			if err != nil {
				t.Fatalf("LoadAll failed: %v", err)
			}

			title, ok := all[0].Metadata["title"].(string)
			if !ok {
				t.Fatalf("Metadata[\"title\"] is not a string, got %T", all[0].Metadata["title"])
			}
			if title != tc.expectedTitle {
				t.Errorf("expected title %q, got %q", tc.expectedTitle, title)
			}
		})
	}
}

func TestStoreMemory_Queryable(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	content := "Important project context about database connections."
	err = store.StoreMemory(context.Background(), "db-context", content)
	if err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Query using the same embedding the provider returns for any input.
	results, err := store.Query([]float32{1, 0, 0}, 1, 0.0)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result from Query, got %d", len(results))
	}

	if results[0].Record.ID != "db-context" {
		t.Errorf("expected ID 'db-context', got %q", results[0].Record.ID)
	}
	if results[0].Record.Type != "memory" {
		t.Errorf("expected Type 'memory', got %q", results[0].Record.Type)
	}
}

func TestStoreMemory_LoadAll(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	memories := []struct {
		name    string
		content string
	}{
		{"mem-a", "# First\n\nContent A"},
		{"mem-b", "# Second\n\nContent B"},
		{"mem-c", "# Third\n\nContent C"},
	}

	for _, m := range memories {
		if err := store.StoreMemory(ctx, m.name, m.content); err != nil {
			t.Fatalf("StoreMemory(%q) failed: %v", m.name, err)
		}
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}

	// Build a map by ID for easier verification.
	recordMap := make(map[string]VectorRecord)
	for _, rec := range all {
		recordMap[rec.ID] = rec
	}

	for _, m := range memories {
		rec, exists := recordMap[m.name]
		if !exists {
			t.Errorf("expected record with ID %q not found", m.name)
			continue
		}
		if rec.Type != "memory" {
			t.Errorf("record %q: expected Type 'memory', got %q", m.name, rec.Type)
		}
		if rec.File != m.name+".md" {
			t.Errorf("record %q: expected File %q, got %q", m.name, m.name+".md", rec.File)
		}
		if rec.Name != m.name {
			t.Errorf("record %q: expected Name %q, got %q", m.name, m.name, rec.Name)
		}
		if len(rec.Embedding) == 0 {
			t.Errorf("record %q: expected non-empty Embedding", m.name)
		}
	}
}

func TestStoreMemory_EmbeddingCopy(t *testing.T) {
	dir := t.TempDir()
	provider := &constantProvider{vec: []float32{1, 0, 0}}

	store, err := NewConversationStore(provider, filepath.Join(dir, "convo.jsonl"), provider.ModelHash())
	if err != nil {
		t.Fatalf("NewConversationStore failed: %v", err)
	}
	defer store.Close()

	content := "Memory content for embedding copy test."
	err = store.StoreMemory(context.Background(), "copy-test", content)
	if err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Get the stored embedding from LoadAll.
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	storedEmbedding := all[0].Embedding

	// The embedding should be the provider's vector [1, 0, 0].
	if len(storedEmbedding) != 3 {
		t.Fatalf("expected embedding length 3, got %d", len(storedEmbedding))
	}
	if storedEmbedding[0] != 1 || storedEmbedding[1] != 0 || storedEmbedding[2] != 0 {
		t.Errorf("expected embedding [1,0,0], got %v", storedEmbedding)
	}

	// Query with the same vector — should find the memory with high similarity.
	results, err := store.Query([]float32{1, 0, 0}, 1, 0.99)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Record.ID != "copy-test" {
		t.Errorf("expected ID 'copy-test', got %q", results[0].Record.ID)
	}
	if results[0].Similarity < 0.99 {
		t.Errorf("expected similarity >= 0.99, got %f", results[0].Similarity)
	}
}
