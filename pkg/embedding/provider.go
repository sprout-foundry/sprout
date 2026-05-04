// Package embedding provides an embedding provider interface, vector store,
// and similarity utilities for semantic search and duplicate detection.
package embedding

import (
	"context"
	"time"
)

// EmbeddingProvider produces vector embeddings for text input.
// Implementations typically wrap an external model (e.g., OpenAI, local Ollama).
type EmbeddingProvider interface {
	// Embed returns a fixed-dimension embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns embedding vectors for multiple texts.
	// The returned slice has the same length and order as input.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of vectors produced by this provider.
	Dimensions() int

	// Name returns a human-readable identifier for the provider.
	Name() string
}

// VectorStore persists and queries vector embeddings with metadata.
type VectorStore interface {
	// Store adds records to the store. If a record with the same ID exists,
	// it is replaced.
	Store(records []VectorRecord) error

	// Query returns the top-K records whose embeddings are most similar to
	// the query vector, with similarity >= threshold.
	Query(vec []float32, topK int, threshold float32) ([]QueryResult, error)

	// DeleteByFile removes all records whose File field matches filePath.
	DeleteByFile(filePath string) error

	// Size returns the total number of records in the store.
	Size() int

	// Close releases any resources held by the store.
	Close() error
}

// VectorRecord represents a single vectorized item with metadata.
// Used for duplicate detection and semantic search over code files.
type VectorRecord struct {
	// ID is a unique identifier for this record.
	ID string `json:"id"`

	// File is the file path the record comes from.
	File string `json:"file"`

	// Name is the symbol or block name (e.g., function name).
	Name string `json:"name"`

	// Signature is the function or block signature text.
	Signature string `json:"signature"`

	// StartLine is the 1-based starting line number of the record.
	StartLine int `json:"startLine"`

	// EndLine is the 1-based ending line number of the record.
	EndLine int `json:"endLine"`

	// Language is the programming language (e.g., "go", "python").
	Language string `json:"language"`

	// Embedding is the vector embedding of the record's content.
	Embedding []float32 `json:"embedding"`

	// Hash is a content hash for duplicate detection.
	Hash string `json:"hash"`

	// IndexedAt is the time when the record was indexed.
	IndexedAt time.Time `json:"indexedAt"`
}

// QueryResult pairs a VectorRecord with its similarity score for ranking.
type QueryResult struct {
	Record     VectorRecord
	Similarity float32
}
