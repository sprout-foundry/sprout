//go:build linux && (arm64 || amd64) && cgo && ggml

// Package qwen35 stub implementations for non-MLX backends. The fused Metal
// kernel and compiled closure paths are unavailable — callers fall back to
// the eager multi-op path.
package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// PrepareCompiledDecode always fails on non-MLX backends; the Model layer
// falls back to the eager decode path (interface assertion governs).
func (q *Qwen35) PrepareCompiledDecode(promptLen, maxTokens int, cache *llm.KVCache) error {
	return errFusedUnavailable
}

// ForwardDecodeCompiled always fails on non-MLX backends.
func (q *Qwen35) ForwardDecodeCompiled(tokenArr tensor.Array, pos int) (tensor.Array, error) {
	return nil, errFusedUnavailable
}

// ReleaseCompiledDecode is a no-op on non-MLX backends.
func (q *Qwen35) ReleaseCompiledDecode() {}

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
