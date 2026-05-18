package embedding

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
func NewConversationStore(provider EmbeddingProvider, filePath string) (*ConversationStore, error) {
	store, err := NewJSONLFileStore(filePath)
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
