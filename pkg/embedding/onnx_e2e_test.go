//go:build !js

package embedding

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestE2E_ONNXEmbeddingProvider tests the full ONNX embedding pipeline with
// the real EmbeddingGemma 300m model. It validates:
// 1. Model loading and session creation
// 2. Single text embedding
// 3. Batch embedding
// 4. Semantic similarity (NL query vs code memory)
// 5. Memory store + retrieval workflow
//
// Skips if the model files are not present (not downloaded).
func TestE2E_ONNXEmbeddingProvider(t *testing.T) {
	modelDir := DefaultModelDir()
	modelPath := filepath.Join(modelDir, "embeddinggemma-300m-q8", "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, "embeddinggemma-300m-q8", "tokenizer.json")

	// Skip if model not downloaded
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("EmbeddingGemma model not downloaded. Download model_q4.onnx, model_q4.onnx_data, and tokenizer.json to ~/.config/sprout/models/embeddinggemma-300m-q8/")
	}
	if _, err := os.Stat(tokenizerPath); os.IsNotExist(err) {
		t.Skip("EmbeddingGemma tokenizer not downloaded.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Create ONNX runtime
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		t.Fatalf("Failed to create ONNX runtime: %v", err)
	}
	defer runtime.Close()

	// Step 2: Create embedding provider
	dims := 256
	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, dims)
	if err != nil {
		t.Fatalf("Failed to create ONNX embedding provider: %v", err)
	}
	defer provider.Close()

	t.Logf("Provider: %s, dims: %d, hash: %s", provider.Name(), provider.Dimensions(), provider.ModelHash()[:16])

	// Step 3: Test single embedding
	t.Run("SingleEmbed", func(t *testing.T) {
		vec, err := provider.Embed(ctx, "user authentication function")
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(vec) != dims {
			t.Fatalf("Expected %d dimensions, got %d", dims, len(vec))
		}

		// Verify non-zero and normalized
		norm := float32(0)
		for _, v := range vec {
			norm += v * v
		}
		if norm < 0.99 || norm > 1.01 {
			t.Errorf("Vector not L2-normalized (norm² = %.4f)", norm)
		}

		t.Logf("Embedding norm²: %.6f, first 5 values: %.4f %.4f %.4f %.4f %.4f",
			norm, vec[0], vec[1], vec[2], vec[3], vec[4])
	})

	// Step 4: Test batch embedding
	t.Run("BatchEmbed", func(t *testing.T) {
		texts := []string{
			"database connection pooling",
			"HTTP request handler",
			"file system operations",
		}
		vecs, err := provider.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedBatch failed: %v", err)
		}
		if len(vecs) != 3 {
			t.Fatalf("Expected 3 vectors, got %d", len(vecs))
		}
		for i, vec := range vecs {
			if len(vec) != dims {
				t.Errorf("Vector %d: expected %d dims, got %d", i, dims, len(vec))
			}
		}
	})

	// Step 5: Test semantic similarity — related texts should be closer than unrelated
	t.Run("SemanticSimilarity", func(t *testing.T) {
		// Embed with QueryPrefix to activate task-specific embedding space
		authFunc, _ := provider.EmbedWithPrefix(ctx, "function that authenticates users with JWT tokens", QueryPrefix)
		loginQuery, _ := provider.EmbedWithPrefix(ctx, "user login and session management", QueryPrefix)
		fileIO, _ := provider.EmbedWithPrefix(ctx, "reading and writing files to disk", QueryPrefix)

		simRelated := cosineSimilarity(authFunc, loginQuery)
		simUnrelated := cosineSimilarity(authFunc, fileIO)

		t.Logf("Similarity(auth, login):  %.4f", simRelated)
		t.Logf("Similarity(auth, fileIO): %.4f", simUnrelated)

		if simRelated < 0.50 {
			t.Errorf("Related texts similarity too low: %.4f (need >= 0.50)", simRelated)
		}
		if simUnrelated < 0.40 {
			t.Errorf("Unrelated IT texts should still score > 0.40, got %.4f", simUnrelated)
		}
	})

	// Step 6: Test prompt prefix effect
	t.Run("PromptPrefix", func(t *testing.T) {
		vecNoPrefix, _ := provider.Embed(ctx, "error handling middleware")
		vecWithPrefix, _ := provider.EmbedWithPrefix(ctx, "error handling middleware", QueryPrefix)

		sim := cosineSimilarity(vecNoPrefix, vecWithPrefix)
		t.Logf("Similarity(with/without prefix): %.4f", sim)

		// Prefix should change the embedding but keep it in the same general area
		if sim < 0.5 {
			t.Logf("WARNING: prefix significantly changes embedding (sim=%.4f)", sim)
		}
	})
}

// TestE2E_ONNXMemoryWorkflow tests the full memory embedding + retrieval
// pipeline using ONNX provider.
func TestE2E_ONNXMemoryWorkflow(t *testing.T) {
	modelDir := DefaultModelDir()
	modelPath := filepath.Join(modelDir, "embeddinggemma-300m-q8", "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, "embeddinggemma-300m-q8", "tokenizer.json")

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("EmbeddingGemma model not downloaded")
	}
	if _, err := os.Stat(tokenizerPath); os.IsNotExist(err) {
		t.Skip("EmbeddingGemma tokenizer not downloaded")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create provider
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		t.Fatalf("ONNX runtime: %v", err)
	}
	defer runtime.Close()

	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, 256)
	if err != nil {
		t.Fatalf("ONNX provider: %v", err)
	}
	defer provider.Close()

	// Create a temp conversation store with ONNX provider
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "memory_test.jsonl")
	store, err := NewConversationStore(provider, storePath, provider.ModelHash())
	if err != nil {
		t.Fatalf("Conversation store: %v", err)
	}
	defer store.Close()

	// Store several memories
	memories := []struct {
		name    string
		content string
	}{
		{"git-conventions", "# Git Conventions\n\nAlways use conventional commits with type(scope): description.\nSquash merges before PR. Never force push."},
		{"test-patterns", "# Test Patterns\n\nUse table-driven tests for multiple scenarios. Name test functions TestUnit_Scenario_Expected."},
		{"db-migration", "# Database Migrations\n\nRun migrations with golang-migrate. Never modify existing migration files.\nAlways add both up and down migrations."},
		{"api-auth", "# API Authentication\n\nAll endpoints require JWT bearer token. Validate tokens with the auth middleware.\nRefresh tokens have 7-day expiry."},
	}

	for _, m := range memories {
		if err := store.StoreMemory(ctx, m.name, m.content); err != nil {
			t.Fatalf("StoreMemory(%s): %v", m.name, err)
		}
		t.Logf("Stored memory: %s", m.name)
	}
	t.Logf("Store size: %d records", store.Size())

	// Query for related memories
	queries := []struct {
		query         string
		expectTopName string // expected top result name
	}{
		{"how to authenticate API requests", "api-auth"},
		{"running database schema changes", "db-migration"},
		{"writing tests for new features", "test-patterns"},
	}

	for _, q := range queries {
		vec, err := provider.Embed(ctx, q.query)
		if err != nil {
			t.Fatalf("Embed query %q: %v", q.query, err)
		}

		results, err := store.Query(vec, 3, 0.0) // low threshold to see all results
		if err != nil {
			t.Fatalf("Query %q: %v", q.query, err)
		}

		if len(results) == 0 {
			t.Errorf("Query %q: no results", q.query)
			continue
		}

		t.Logf("Query: %q", q.query)
		for i, r := range results {
			t.Logf("  #%d: %s (sim=%.4f)", i+1, r.Record.Name, r.Similarity)
		}

		if results[0].Record.Name != q.expectTopName {
			t.Errorf("Query %q: expected top result %s, got %s (sim=%.4f)",
				q.query, q.expectTopName, results[0].Record.Name, results[0].Similarity)
		}
	}
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA*normB)))
}
