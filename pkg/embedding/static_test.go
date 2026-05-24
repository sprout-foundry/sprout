//go:build !js && staticmodel

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

	if model.Dims() != 384 {
		t.Errorf("Expected dimensions 384, got %d", model.Dims())
	}

	if model.VocabSize() != 32000 {
		t.Errorf("Expected vocab size 32000, got %d", model.VocabSize())
	}

	if len(model.GetEmbedding(0)) != model.Dims() {
		t.Errorf("Expected embedding length %d, got %d",
			model.Dims(), len(model.GetEmbedding(0)))
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

	if provider.Dimensions() != 384 {
		t.Errorf("Expected dimensions 384, got %d", provider.Dimensions())
	}

	if provider.Name() != "static-384d-f32" {
		t.Errorf("Expected name 'static-384d-f32', got %s", provider.Name())
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
	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions for empty string, got %d", len(vec))
	}

	// Test single word
	vec, err = provider.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Failed to embed single word: %v", err)
	}
	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions for single word, got %d", len(vec))
	}

	// Test multiple words
	vec, err = provider.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Failed to embed multiple words: %v", err)
	}
	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions for multiple words, got %d", len(vec))
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
	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions, got %d", len(vec))
	}

	// Test non-empty string
	vec, err = provider.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Failed to embed text: %v", err)
	}
	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions, got %d", len(vec))
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
		if len(vec) != 384 {
			t.Errorf("Vector %d: Expected 384 dimensions, got %d", i, len(vec))
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
