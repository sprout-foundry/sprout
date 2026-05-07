package embedding

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"sync"
)

//go:embed static_model.bin
var staticModelData []byte

// StaticProvider is an EmbeddingProvider backed by a model2vec static model.
// It uses pure Go with no CGO dependencies.
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

	tokenIDs := p.tokenizer.Tokenize(text)
	if len(tokenIDs) == 0 {
		return make([]float32, p.model.dims), nil
	}

	// Mean pool: average the int8 embedding rows for each token
	sum := make([]float64, p.model.dims)
	for _, id := range tokenIDs {
		if int(id) >= p.model.vocabSize {
			continue
		}
		offset := int(id) * p.model.dims
		for d := 0; d < p.model.dims; d++ {
			sum[d] += float64(p.model.embeddings[offset+d])
		}
	}

	// Compute average and normalize
	result := make([]float32, p.model.dims)
	norm := float64(0)
	for d := 0; d < p.model.dims; d++ {
		avg := sum[d] / float64(len(tokenIDs))
		result[d] = float32(avg)
		norm += float64(avg) * float64(avg)
	}

	// L2 normalize
	norm = math.Sqrt(norm)
	if norm > 1e-9 {
		for d := range result {
			result[d] /= float32(norm)
		}
	}

	return result, nil
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
// Results are returned in the same order as input.
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

	// Tokenize all texts
	tokenBatches := p.tokenizer.TokenizeBatch(texts)

	// Compute embeddings for each text
	results := make([][]float32, len(texts))
	for i, tokenIDs := range tokenBatches {
		if len(tokenIDs) == 0 {
			results[i] = make([]float32, p.model.dims)
			continue
		}

		// Mean pool
		sum := make([]float64, p.model.dims)
		for _, id := range tokenIDs {
			if int(id) >= p.model.vocabSize {
				continue
			}
			offset := int(id) * p.model.dims
			for d := 0; d < p.model.dims; d++ {
				sum[d] += float64(p.model.embeddings[offset+d])
			}
		}

		// Compute average and normalize
		embedding := make([]float32, p.model.dims)
		norm := float64(0)
		for d := 0; d < p.model.dims; d++ {
			avg := sum[d] / float64(len(tokenIDs))
			embedding[d] = float32(avg)
			norm += float64(avg) * float64(avg)
		}

		// L2 normalize
		norm = math.Sqrt(norm)
		if norm > 1e-9 {
			for d := range embedding {
				embedding[d] /= float32(norm)
			}
		}

		results[i] = embedding
	}

	return results, nil
}

// Dimensions returns the dimensionality of vectors produced by this provider.
func (p *StaticProvider) Dimensions() int {
	return p.model.dims
}

// Name returns a human-readable identifier for the provider.
func (p *StaticProvider) Name() string {
	return "bundled-static"
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
