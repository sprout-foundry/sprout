package embedding

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"math"
	"sync"
)

//go:embed static_model.bin
var staticModelData []byte

// StaticProvider is an EmbeddingProvider backed by a static model (e.g. nomic-embed-text-v2-moe).
// Supports int8 (v1), float32 (v2), and Unigram Viterbi (v3) embedding formats.
// Pure Go, no CGO — works in WASM.
type StaticProvider struct {
	mu        sync.RWMutex
	model     *StaticModel
	tokenizer *StaticTokenizer
	closed    bool
}

// NewStaticProvider creates a provider from embedded model data.
func NewStaticProvider() (*StaticProvider, error) {
	if len(staticModelData) == 0 {
		return nil, fmt.Errorf("static model data is empty")
	}

	model, err := LoadStaticModel(staticModelData)
	if err != nil {
		return nil, fmt.Errorf("load static model: %w", err)
	}

	if err := model.Validate(); err != nil {
		return nil, fmt.Errorf("validate static model: %w", err)
	}

	tokenizer := &StaticTokenizer{
		vocabMap:        model.vocabMap,
		vocabSize:       model.vocabSize,
		unkID:           model.unkID,
		usesSpacePrefix: model.usesSpacePrefix,
		tokenizerType:   model.tokenizerType,
	}

	// For v3 Unigram models, populate full vocab + weights + mapping
	if model.HasViterbiData() {
		tokenizer.vocabFull = model.vocabFull
		tokenizer.vocabFullMap = model.vocabFullMap
		tokenizer.weights = model.weights
		tokenizer.mapping = model.mapping
	}

	return &StaticProvider{
		model:     model,
		tokenizer: tokenizer,
	}, nil
}

// Embed returns a L2-normalized embedding vector for the given text.
func (p *StaticProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	tokenIDs := p.tokenize(text)
	if len(tokenIDs) == 0 {
		return make([]float32, p.model.dims), nil
	}

	sum := make([]float64, p.model.dims)
	n := len(tokenIDs)

	for _, id := range tokenIDs {
		if int(id) >= p.model.vocabSize {
			continue
		}
		offset := int(id) * p.model.dims
		if p.model.isFloat32 {
			for d := 0; d < p.model.dims; d++ {
				sum[d] += float64(p.model.embeddingsF32[offset+d])
			}
		} else {
			for d := 0; d < p.model.dims; d++ {
				sum[d] += float64(p.model.embeddingsI8[offset+d])
			}
		}
	}

	return p.finalize(sum, n), nil
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
func (p *StaticProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	tokenBatches := p.tokenizer.TokenizeBatch(texts)
	results := make([][]float32, len(texts))

	for i, tokenIDs := range tokenBatches {
		if len(tokenIDs) == 0 {
			results[i] = make([]float32, p.model.dims)
			continue
		}

		sum := make([]float64, p.model.dims)
		for _, id := range tokenIDs {
			if int(id) >= p.model.vocabSize {
				continue
			}
			offset := int(id) * p.model.dims
			if p.model.isFloat32 {
				for d := 0; d < p.model.dims; d++ {
					sum[d] += float64(p.model.embeddingsF32[offset+d])
				}
			} else {
				for d := 0; d < p.model.dims; d++ {
					sum[d] += float64(p.model.embeddingsI8[offset+d])
				}
			}
		}

		results[i] = p.finalize(sum, len(tokenIDs))
	}

	return results, nil
}

// tokenize is an inline wrapper to avoid repeating the call.
func (p *StaticProvider) tokenize(text string) []uint16 {
	return p.tokenizer.Tokenize(text)
}

// finalize computes the mean-pooled, L2-normalized result from a sum vector.
func (p *StaticProvider) finalize(sum []float64, n int) []float32 {
	result := make([]float32, p.model.dims)
	norm := float64(0)
	for d := 0; d < p.model.dims; d++ {
		avg := sum[d] / float64(n)
		result[d] = float32(avg)
		norm += float64(avg) * float64(avg)
	}
	norm = math.Sqrt(norm)
	if norm > 1e-9 {
		for d := range result {
			result[d] /= float32(norm)
		}
	}
	return result
}

// Dimensions returns the dimensionality of vectors produced by this provider.
func (p *StaticProvider) Dimensions() int {
	return p.model.dims
}

// Name returns a human-readable identifier for the provider.
func (p *StaticProvider) Name() string {
	if p.model == nil {
		return "static-fallback"
	}
	// e.g. "nomic-embed-text-v2-moe-384d-f32" or "bge-base-en-v1.5-256d-i8"
	dtype := "f32"
	if !p.model.isFloat32 {
		dtype = "i8"
	}
	return fmt.Sprintf("static-%dd-%s", p.model.dims, dtype)
}

// ModelHash returns a SHA-256 hex digest of the embedded model data.
func (p *StaticProvider) ModelHash() string {
	return fmt.Sprintf("%x", sha256.Sum256(staticModelData))
}

// DebugTokenize returns token strings and embedding IDs for debugging purposes.
func (p *StaticProvider) DebugTokenize(text string) ([]string, []uint16) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tokenizer.TokenizeWithTokens(text)
}

// Close releases resources held by the provider.
func (p *StaticProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	// No external resources to clean up
	p.model = nil
	p.tokenizer = nil
	return nil
}
