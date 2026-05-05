//go:build !cgo

package embedding

import (
	"context"
	"fmt"
)

var errNoCGO = fmt.Errorf("embeddings not available: built without CGO")

// BundledProvider is a no-op stub used when CGO is disabled.
// All methods return an error; the provider cannot perform inference.
type BundledProvider struct{}

// NewBundledProvider always returns an error in no-CGO builds because
// the ONNX Runtime depends on CGO.
func NewBundledProvider(modelDir string, ortLibPath string) (*BundledProvider, error) {
	return nil, errNoCGO
}

// Embed returns an error.
func (p *BundledProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errNoCGO
}

// EmbedBatch returns an error.
func (p *BundledProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errNoCGO
}

// Dimensions returns 0.
func (p *BundledProvider) Dimensions() int {
	return 0
}

// Name returns an indicator that the provider is unavailable.
func (p *BundledProvider) Name() string {
	return "bundled-minilm (unavailable)"
}

// Close is a no-op.
func (p *BundledProvider) Close() error {
	return nil
}

// CleanupORT is a no-op in no-CGO builds.
func CleanupORT() error {
	return nil
}
