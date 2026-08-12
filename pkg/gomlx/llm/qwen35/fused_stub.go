//go:build linux && arm64 && cgo && ggml

// Package qwen35 stub implementations for non-MLX backends. The fused Metal
// kernel and compiled closure paths are unavailable — callers fall back to
// the eager multi-op path.
package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// compiledDecoder is a no-op stub (never instantiated on non-MLX builds).
type compiledDecoder struct{}

func useCompiledDecode() bool { return false }

func (q *Qwen35) compileDecodeClosure(cache *llm.KVCache) (*compiledDecoder, error) {
	return nil, errFusedUnavailable
}

func (q *Qwen35) forwardDecodeCompiled(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	return 0, errFusedUnavailable
}

// fusedSwiglu always fails — caller falls back to eager SiLU + multiply.
func fusedSwiglu(h, gate, xVal tensor.Array, backend tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	return nil, errFusedUnavailable
}

// fusedComputeG always fails — caller falls back to eager multi-op path.
func fusedComputeG(aLog, a, dtBias tensor.Array, backend tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	return nil, errFusedUnavailable
}

// fusedGatedDeltaUpdate always fails — caller falls back to eager delta math.
func fusedGatedDeltaUpdate(q, k, v, g, beta, state tensor.Array, backend tensor.Backend, stream tensor.Stream) (tensor.Array, tensor.Array, error) {
	return nil, nil, errFusedUnavailable
}

var errFusedUnavailable = fmt.Errorf("fused kernels unavailable on non-MLX backend")
