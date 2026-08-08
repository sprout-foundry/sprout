//go:build !darwin || !arm64 || !cgo || !mlx

package embedding

import (
	"context"
	"fmt"
)

// MLXEmbeddingProvider is a non-functional stub on platforms without Apple
// Silicon GPU. The constructor returns an error so callers fall back to the
// ONNX provider.
type MLXEmbeddingProvider struct{}

func NewMLXEmbeddingProvider(ctx context.Context, modelPath, tokenizerPath string) (*MLXEmbeddingProvider, error) {
	return nil, fmt.Errorf("mlx embedding: not available (requires Apple Silicon + cgo)")
}

func (p *MLXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("mlx embedding: not available")
}

func (p *MLXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("mlx embedding: not available")
}

func (p *MLXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	return nil, fmt.Errorf("mlx embedding: not available")
}

func (p *MLXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	return nil, fmt.Errorf("mlx embedding: not available")
}

func (p *MLXEmbeddingProvider) Dimensions() int   { return 0 }
func (p *MLXEmbeddingProvider) Name() string      { return "mlx-unavailable" }
func (p *MLXEmbeddingProvider) ModelHash() string { return "" }
func (p *MLXEmbeddingProvider) Close() error      { return nil }
