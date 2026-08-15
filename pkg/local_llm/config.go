//go:build darwin && arm64 && cgo && mlx

// Package local_llm implements a local LLM inference engine using Apple's MLX
// framework for GPU-accelerated text generation on Apple Silicon. It runs a
// decoder-only transformer (Qwen3 architecture) entirely in Go via CGO — no
// Python, no llama.cpp, no external runtime.
//
// The package only compiles on darwin/arm64 with cgo and the mlx build tag.
// On all other platforms, the stub (stub.go) returns an error from New, so
// callers can fall back to cloud providers.
package local_llm

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// ModelConfig holds the architecture constants for a Qwen3-family model.
// Values are loaded from the model's config.json at init time.
type ModelConfig struct {
	VocabSize        int
	HiddenSize       int
	IntermediateSize int
	NumLayers        int
	NumHeads         int
	NumKVHeads       int
	HeadDim          int
	RMSNormEPS       float32
	RopeTheta        float64
	BOSTokenID       int
	EOSTokenID       int
}

// Qwen3_0_6B returns the config for the Qwen3-0.6B model.
func Qwen3_0_6B() ModelConfig {
	return ModelConfig{
		VocabSize:        151936,
		HiddenSize:       1024,
		IntermediateSize: 3072,
		NumLayers:        28,
		NumHeads:         16,
		NumKVHeads:       8,
		HeadDim:          128,
		RMSNormEPS:       1e-6,
		RopeTheta:        1000000.0,
		BOSTokenID:       151643,
		EOSTokenID:       151645,
	}
}

// weights holds all MLX arrays for the model. Weights are loaded once at init
// and reused for every forward pass. The arrays are in fp32 (converted from
// bf16 during loading) because our CGO wrapper does fp32 matmul.
type weights struct {
	embedTokens *mlx.Array // [vocab_size, hidden_size]
	layers      []layerWeights
	normWeight  *mlx.Array // [hidden_size] — final RMSNorm
}

type layerWeights struct {
	inputNorm *mlx.Array // [hidden_size] — RMSNorm before attention

	// Attention: Qwen3 uses GQA (grouped-query attention)
	// q/k/v/o projections have no bias
	qProj *mlx.Array // [hidden_size, num_heads * head_dim]
	kProj *mlx.Array // [hidden_size, num_kv_heads * head_dim]
	vProj *mlx.Array // [hidden_size, num_kv_heads * head_dim]
	oProj *mlx.Array // [num_heads * head_dim, hidden_size]

	// QK Norm (Qwen3 normalizes Q and K with a learned RMSNorm scalar per head)
	qNorm *mlx.Array // [num_heads * head_dim] — RMSNorm weight for Q
	kNorm *mlx.Array // [num_kv_heads * head_dim] — RMSNorm weight for K

	postNorm *mlx.Array // [hidden_size] — RMSNorm after attention, before FFN

	// FFN: SwiGLU (SiLU-gated)
	gateProj *mlx.Array // [hidden_size, intermediate_size]
	upProj   *mlx.Array // [hidden_size, intermediate_size]
	downProj *mlx.Array // [intermediate_size, hidden_size]
}

// numQKNormElements returns the size of the QK norm weight vectors. Qwen3
// applies RMSNorm to Q and K per-head: q_norm has shape [num_heads * head_dim]
// and k_norm has shape [num_kv_heads * head_dim].
func (c ModelConfig) numQKNormElements() (int, int) {
	return c.NumHeads * c.HeadDim, c.NumKVHeads * c.HeadDim
}

func (c ModelConfig) String() string {
	return fmt.Sprintf("Qwen3(hidden=%d, layers=%d, heads=%d, kv_heads=%d, head_dim=%d, vocab=%d)",
		c.HiddenSize, c.NumLayers, c.NumHeads, c.NumKVHeads, c.HeadDim, c.VocabSize)
}
