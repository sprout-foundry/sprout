package embedding

import (
	"context"
	"crypto/md5"
	"fmt"
	"time"
)

// ConversationStore wraps a VectorStore for storing and querying
// conversation turn embeddings. It provides a user-scoped persistent
// store that survives across workspace changes.
//
// The store uses the same static embedding provider as the code index,
// and maintains its own in-memory cache of records for fast queries.
type ConversationStore struct {
	store    VectorStore       // underlying store for conversation turns
	provider EmbeddingProvider // shared static embedding provider
}

// NewConversationStore creates a new conversation store at the given path.
// The parent directory is created if it does not exist.
// If a file already exists at path, its records are loaded into memory.
//
// The provider is used for embedding operations and is not closed when
// the store is closed (the provider's lifecycle is managed externally).
// The modelHash is used to detect model changes and invalidate stale records.
func NewConversationStore(provider EmbeddingProvider, filePath string, modelHash string) (*ConversationStore, error) {
	store, err := NewHNSWStore(filePath, modelHash)
	if err != nil {
		return nil, err
	}

	return &ConversationStore{
		store:    store,
		provider: provider,
	}, nil
}

// Store adds records to the conversation store. If a record with the same
// ID already exists, it is replaced. Records are kept sorted by ID for
// deterministic output.
func (s *ConversationStore) Store(records []VectorRecord) error {
	return s.store.Store(records)
}

// Query returns the top-K records most similar to vec, with similarity >= threshold.
func (s *ConversationStore) Query(vec []float32, topK int, threshold float32) ([]QueryResult, error) {
	return s.store.Query(vec, topK, threshold)
}

// LoadAll returns a copy of all records currently in the store.
func (s *ConversationStore) LoadAll() ([]VectorRecord, error) {
	return s.store.LoadAll()
}

// Size returns the number of records currently in the store.
func (s *ConversationStore) Size() int {
	return s.store.Size()
}

// Provider returns the embedding provider used by this store.
// The provider remains usable after Close() since its lifecycle
// is managed externally by the EmbeddingManager.
func (s *ConversationStore) Provider() EmbeddingProvider {
	return s.provider
}

// Close releases any resources held by the store.
// The embedding provider is not closed (its lifecycle is managed externally).
func (s *ConversationStore) Close() error {
	return s.store.Close()
}

// StoreMemory embeds memory content and stores it as a VectorRecord with Type "memory".
// The record ID is "memory:" + name to ensure unique naming and easy lookup.
func (s *ConversationStore) StoreMemory(ctx context.Context, name string, content string) error {
	embedding, err := s.provider.Embed(ctx, content)
	if err != nil {
		return fmt.Errorf("failed to embed memory: %w", err)
	}
	if len(embedding) == 0 {
		return fmt.Errorf("embedding returned empty result")
	}

	contentPreview := content
	if len(contentPreview) > 200 {
		contentPreview = contentPreview[:200]
	}

	record := VectorRecord{
		ID:        "memory:" + name,
		File:      "",
		Name:      name,
		Type:      "memory",
		Embedding: embedding,
		Hash:      fmt.Sprintf("%x", md5.Sum([]byte(content))),
		IndexedAt: time.Now(),
		Metadata:  map[string]interface{}{"name": name, "content_preview": contentPreview},
	}

	return s.Store([]VectorRecord{record})
}

// DeleteMemoryByName removes all memory records with the given name.
// This is useful when a memory file is deleted or updated and its old
// embedding should be removed from the store.
func (s *ConversationStore) DeleteMemoryByName(name string) error {
	all, err := s.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load records: %w", err)
	}

	var ids []string
	for _, r := range all {
		if r.Type == "memory" && r.Name == name {
			ids = append(ids, r.ID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	return s.store.DeleteByIDs(ids)
}

// QueryMemories searches memory records by embedding the query and returning
// top-K results. Results are filtered to only include records with Type "memory".
func (s *ConversationStore) QueryMemories(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	embedding, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(embedding) == 0 {
		return nil, fmt.Errorf("embedding returned empty result")
	}

	results, err := s.Query(embedding, topK, threshold)
	if err != nil {
		return nil, err
	}

	// Filter to only memory records
	var memoryResults []QueryResult
	for _, r := range results {
		if r.Record.Type == "memory" {
			memoryResults = append(memoryResults, r)
		}
	}

	return memoryResults, nil
}
