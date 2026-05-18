package embedding

import (
	"context"
	"math"
	"testing"
)

func TestLoadStaticModel(t *testing.T) {
	model, err := LoadStaticModel(staticModelData)
	if err != nil {
		t.Fatalf("Failed to load static model: %v", err)
	}

	if model.dims != 256 {
		t.Errorf("Expected dimensions 256, got %d", model.dims)
	}

	if model.vocabSize != 29525 {
		t.Errorf("Expected vocab size 29525, got %d", model.vocabSize)
	}

	if len(model.embeddings) != model.vocabSize*model.dims {
		t.Errorf("Expected %d embedding entries, got %d",
			model.vocabSize*model.dims, len(model.embeddings))
	}

	if err := model.Validate(); err != nil {
		t.Errorf("Model validation failed: %v", err)
	}
}

func TestNewStaticProvider(t *testing.T) {
	provider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	if provider.Dimensions() != 256 {
		t.Errorf("Expected dimensions 256, got %d", provider.Dimensions())
	}

	if provider.Name() != "bge-base-en-v1.5-256d" {
		t.Errorf("Expected name 'bge-base-en-v1.5-256d', got %s", provider.Name())
	}
}

func TestStaticTokenizer(t *testing.T) {
	provider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test empty string
	vec, err := provider.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("Failed to embed empty string: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("Expected 256 dimensions for empty string, got %d", len(vec))
	}

	// Test single word
	vec, err = provider.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Failed to embed single word: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("Expected 256 dimensions for single word, got %d", len(vec))
	}

	// Test multiple words
	vec, err = provider.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Failed to embed multiple words: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("Expected 256 dimensions for multiple words, got %d", len(vec))
	}
}

func TestStaticProviderEmbed(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test empty string
	vec, err := provider.Embed(ctx, "")
	if err != nil {
		t.Fatalf("Failed to embed empty string: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("Expected 256 dimensions, got %d", len(vec))
	}

	// Test non-empty string
	vec, err = provider.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Failed to embed text: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("Expected 256 dimensions, got %d", len(vec))
	}

	// Verify L2 normalization is approximately 1
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm < 0.999 || norm > 1.001 {
		t.Errorf("Expected L2 norm ~1.0, got %f", norm)
	}
}

func TestStaticProviderEmbedBatch(t *testing.T) {
	ctx := context.Background()
	provider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	texts := []string{"hello", "world", "", "test"}
	vecs, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("Failed to embed batch: %v", err)
	}

	if len(vecs) != 4 {
		t.Errorf("Expected 4 vectors, got %d", len(vecs))
	}

	for i, vec := range vecs {
		if len(vec) != 256 {
			t.Errorf("Vector %d: Expected 256 dimensions, got %d", i, len(vec))
		}
	}
}

func TestStaticProviderClose(t *testing.T) {
	provider, err := NewStaticProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	if err := provider.Close(); err != nil {
		t.Fatalf("Failed to close provider: %v", err)
	}

	// Double close should be safe
	if err := provider.Close(); err != nil {
		t.Fatalf("Double close should not error: %v", err)
	}
}
