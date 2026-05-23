//go:build !js && cgo

package embedding

import (
	"context"
	"fmt"
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
	requireONNXTestModel(t)
	modelDir := DefaultModelDir()
	modelName := EmbeddingGemma300MConfig().Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

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
		// Embed related texts
		authFunc, _ := provider.Embed(ctx, "function that authenticates users with JWT tokens")
		loginQuery, _ := provider.Embed(ctx, "user login and session management")
		fileIO, _ := provider.Embed(ctx, "reading and writing files to disk")

		simRelated := cosineSimilarity(authFunc, loginQuery)
		simUnrelated := cosineSimilarity(authFunc, fileIO)

		t.Logf("Similarity(auth, login):  %.4f", simRelated)
		t.Logf("Similarity(auth, fileIO): %.4f", simUnrelated)

		if simRelated <= simUnrelated {
			t.Errorf("Related texts should have higher similarity: related=%.4f, unrelated=%.4f",
				simRelated, simUnrelated)
		}

		if simRelated < 0.40 {
			t.Errorf("Related texts similarity too low: %.4f (need >= 0.40)", simRelated)
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
	requireONNXTestModel(t)
	modelDir := DefaultModelDir()
	modelName := EmbeddingGemma300MConfig().Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

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
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	return float32(float64(x) * (1.0 / float64(sqrt32Approx(x))))
}

// Simple sqrt for normalization purposes.
func sqrt32Approx(x float32) float32 {
	if x <= 0 {
		return 0
	}
	var result float32 = x
	for i := 0; i < 5; i++ {
		result = (result + x/result) / 2
	}
	return result
}

func init() {
	// Ensure fmt is available for test logging
	_ = fmt.Sprintf("")
}

// requireONNXTestModel gates the ONNX e2e tests on both (a) the model files
// being present on disk and (b) an explicit opt-in environment variable.
//
// The opt-in lets a dedicated CI job stage the 200MB model once and run the
// real ONNX path (SPROUT_RUN_ONNX_TESTS=1), while the everyday test command
// (`go test ./...`) stays fast and predictable. Without the env var, even a
// developer who happens to have the model locally won't accidentally run a
// minute-long e2e test as part of their unit-test loop.
func requireONNXTestModel(t *testing.T) {
	t.Helper()
	if os.Getenv("SPROUT_RUN_ONNX_TESTS") == "" {
		t.Skip("SPROUT_RUN_ONNX_TESTS is unset — skipping ONNX e2e tests. " +
			"Set it to 1 to opt in (requires the embeddinggemma-300m model staged at " +
			"~/.config/sprout/models/embeddinggemma-300m/).")
	}
	modelDir := DefaultModelDir()
	modelName := EmbeddingGemma300MConfig().Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skipf("ONNX test opted in but model is absent at %s — run sprout once to bootstrap, or pre-stage the file in CI", modelPath)
	}
	if _, err := os.Stat(tokenizerPath); os.IsNotExist(err) {
		t.Skipf("ONNX test opted in but tokenizer is absent at %s", tokenizerPath)
	}
}
