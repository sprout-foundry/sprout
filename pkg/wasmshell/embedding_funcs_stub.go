//go:build !wasm

package wasmshell

import (
	"context"
	"errors"
)

// ErrEmbeddingDisabled is returned when Mode=Off and Embed() is called.
var ErrEmbeddingDisabled = errors.New("embedding: disabled by mode=off")

// ErrEmbeddingUnavailable is returned when the requested mode's
// provider isn't available (e.g. Mode=ONNX but no JS bridge).
var ErrEmbeddingUnavailable = errors.New("embedding: requested provider unavailable")

// EmbeddingProvider is a no-op wrapper on native builds.
// The real implementation lives in embedding_funcs.go (wasm build tag).
type EmbeddingProvider struct {
	Mode EmbeddingMode
}

// NewEmbeddingProvider returns a no-op wrapper on native builds.
func NewEmbeddingProvider() *EmbeddingProvider {
	return &EmbeddingProvider{Mode: CurrentMode()}
}

// Embed is a no-op on native builds.
func (p *EmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

// NewJSEmbeddingAPI is a no-op on native builds.
func NewJSEmbeddingAPI() {}

// serializeEmbeddingStatus is a no-op on native builds.
func serializeEmbeddingStatus(s EmbeddingStatus) map[string]interface{} {
	return map[string]interface{}{
		"mode":           string(s.Mode),
		"onnx_available": s.ONNXAvailable,
	}
}
