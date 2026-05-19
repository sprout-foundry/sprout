package embedding

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const maxSignatureLen = 2000

// ConversationStore wraps a JSONLFileStore for storing and querying
// conversation turn embeddings. It provides a user-scoped persistent
// store that survives across workspace changes.
//
// The store uses the same static embedding provider as the code index,
// and maintains its own in-memory cache of records for fast queries.
type ConversationStore struct {
	store    *JSONLFileStore   // underlying store for conversation turns
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
	store, err := NewJSONLFileStore(filePath, modelHash)
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

// StoreMemory embeds and stores a memory as a VectorRecord with Type: "memory".
// The memory name is used as the record ID, so calling StoreMemory again with
// the same name replaces the previous record. Returns an error if the memory
// name or content is empty, or if embedding fails.
func (s *ConversationStore) StoreMemory(ctx context.Context, name, content string) error {
	// Validate inputs
	if ctx == nil {
		log.Printf("[conversation-store] skipping memory storage: context is nil")
		return nil
	}
	if name == "" {
		return fmt.Errorf("memory name is empty")
	}
	if content == "" {
		return fmt.Errorf("memory content is empty")
	}

	// Embed the content
	emb, err := s.provider.Embed(ctx, content)
	if err != nil {
		return err
	}

	// Truncate content for signature at a rune boundary
	runes := []rune(content)
	if len(runes) > maxSignatureLen {
		runes = runes[:maxSignatureLen]
	}
	signature := string(runes)

	// Extract title: first non-empty line, trimmed
	var title string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			title = trimmed
			break
		}
	}

	// Create defensive copy of embedding
	embCopy := make([]float32, len(emb))
	copy(embCopy, emb)

	// Create VectorRecord
	record := VectorRecord{
		ID:        name,
		File:      name + ".md",
		Name:      name,
		Signature: signature,
		Embedding: embCopy,
		Type:      "memory",
		IndexedAt: time.Now().UTC(),
		Metadata: map[string]interface{}{
			"title":         title,
			"contentLength": len(content),
		},
	}

	// Store the record
	return s.Store([]VectorRecord{record})
}
