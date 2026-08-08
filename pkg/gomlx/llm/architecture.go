//go:build darwin && arm64 && cgo && mlx

// Package llm provides local LLM inference via MLX on Apple Silicon.
// It implements the full transformer forward pass in Go via CGO — no Python,
// no llama.cpp, no external runtime.
//
// The package is designed for multi-architecture support via the Architecture
// interface. Each model family (Qwen3, Llama, etc.) implements the interface
// and registers itself for config-based dispatch.
package llm

import (
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// Architecture implements the forward pass and weight management for a
// specific model family (e.g. Qwen3, Llama). The engine (Model) drives the
// generation loop, caching, and sampling; the Architecture handles the
// per-layer computation that differs between model families.
//
// This separation means adding a new architecture requires only:
//  1. Implementing this interface
//  2. Adding a config loader
//  3. Registering in the architecture registry
//
// The forward pass methods receive a KVCache so the architecture can choose
// to use or ignore caching. Architectures that don't support caching (or during
// prefill) receive a nil cache.
type Architecture interface {
	// InitWeights loads model weights from a safetensors file into MLX arrays.
	// Called once at model load time.
	InitWeights(path string, s *mlx.Stream) error

	// SetStream sets the MLX stream used for subsequent forward passes.
	// Called before ForwardPrefill/ForwardDecode. MLX streams are
	// thread-local, so this must be called on the same OS thread that
	// runs the forward pass.
	SetStream(s *mlx.Stream)

	// ForwardPrefill runs the forward pass over a full sequence of tokens.
	// Returns the logits for the last position. The KV cache (if non-nil) is
	// populated with keys/values from this pass for use in subsequent decode steps.
	ForwardPrefill(ids *mlx.Array, seqLen int, cache *KVCache) ([]float32, error)

	// ForwardPrefillFrom runs the forward pass over a delta sequence that
	// extends an already-populated KV cache. startPos is the absolute
	// position of the FIRST token in ids (which determines RoPE offsets).
	// The cache must already contain startPos tokens. Used by prefix
	// caching to skip re-prefilling a shared prompt on repeated requests.
	ForwardPrefillFrom(ids *mlx.Array, seqLen, startPos int, cache *KVCache) ([]float32, error)

	// ForwardDecode runs the forward pass for a single token at the given
	// absolute position. Uses the KV cache to avoid recomputing past tokens.
	// Returns logits [vocabSize] for that position.
	ForwardDecode(tokenID int, pos int, cache *KVCache) ([]float32, error)

	// FreeWeights releases all MLX arrays held by the architecture.
	FreeWeights()

	// Config returns the model's configuration.
	Config() ModelConfig
}

// GreedyArchitecture is an optional refinement implemented by architectures
// that can sample the next token greedily on the GPU. When a Model uses
// Temperature <= 0, it prefers this path to avoid transferring the full
// [vocabSize] logits vector to the CPU on every decode step — a major
// throughput win for large-vocab models. Falls back to the standard
// Architecture methods when the architecture doesn't implement it.
type GreedyArchitecture interface {
	Architecture

	// ForwardDecodeArgmax runs a single-token decode step and returns the
	// argmax token ID, computed on the GPU.
	ForwardDecodeArgmax(tokenID int, pos int, cache *KVCache) (int, error)
}

// ModelConfig holds architecture-independent configuration values. Every
// decoder-only transformer shares these fields; architecture-specific values
// (e.g. QK norm presence, bias presence) are encoded as booleans that the
// Architecture implementation reads.
type ModelConfig struct {
	Arch              string  // Architecture identifier: "qwen3", "qwen3_5_text", etc.
	VocabSize         int
	HiddenSize        int
	IntermediateSize  int
	NumLayers         int
	NumHeads          int
	NumKVHeads        int
	HeadDim           int
	RMSNormEPS        float32
	RopeTheta         float64
	BOSTokenID        int
	EOSTokenID        int
	UseQKNorm         bool   // Qwen3-style per-head Q/K RMSNorm
	UseAttentionBias  bool   // Llama uses bias on QKV projections
	UseTiedEmbeddings bool   // lm_head shares weights with embed_tokens
	MaxPosition       int    // Maximum context length
	Quantization      *QuantConfig // nil = full precision

	// Hybrid linear-attention fields (Qwen3.5 / Qwen3-Next style). Zero for
	// pure full-attention models like qwen3.
	FullAttentionInterval int     // every Nth layer is full attention; others are linear (3:1 → 4)
	LinearNumKeyHeads     int     // DeltaNet key heads
	LinearNumValueHeads   int     // DeltaNet value heads
	LinearKeyHeadDim      int     // DeltaNet key head dim
	LinearValueHeadDim    int     // DeltaNet value head dim
	LinearConvKernelDim   int     // DeltaNet conv kernel width
	AttnOutputGate        bool    // o_proj(out * sigmoid(gate)) — gated attention output
	PartialRotaryFactor   float32 // fraction of head dim rotated by RoPE (0.25 for qwen3_5)
	MRopeSection          []int   // mRoPE section sizes [T, H, W]
	MRopeInterleaved      bool    // mRoPE interleaved vs half-split
	MTPNumHiddenLayers    int     // multi-token prediction layers (ignored for plain generation)
	WeightPrefix          string  // safetensors key prefix ("" or "model." for qwen3, "model.language_model." for qwen3_5)
	LayerTypes            []string // optional per-layer type override; nil = derive from FullAttentionInterval
}

// HybridLinearAttn reports whether the model has DeltaNet linear-attention
// layers (Qwen3.5-style hybrid).
func (c ModelConfig) HybridLinearAttn() bool {
	return c.FullAttentionInterval > 0 && c.LinearNumKeyHeads > 0
}

// QuantConfig describes weight quantization for a model. It is set from the
// `quantization` section of config.json on pre-quantized MLX models (e.g.
// mlx-community/Qwen3-0.6B-4bit) or forced at load time to quantize a full
// model on the fly.
type QuantConfig struct {
	GroupSize int
	Bits      int
	Mode      string
}

// ModelWeights is the base set of weights common to all transformer decoders.
// Architecture-specific weights (e.g. QK norm) are stored in the architecture
// implementation's own weight structs.
type DecoderLayerWeights struct {
	InputNorm *mlx.Array // [hidden_size] — RMSNorm/LayerNorm before attention
	QProj     *mlx.Array // [hidden_size, num_heads * head_dim] or [num_heads*head_dim, hidden_size]
	KProj     *mlx.Array
	VProj     *mlx.Array
	OProj     *mlx.Array
	QNorm     *mlx.Array // [head_dim] — optional, Qwen3-style per-head norm
	KNorm     *mlx.Array // [head_dim] — optional, Qwen3-style per-head norm
	PostNorm  *mlx.Array // [hidden_size] — norm after attention, before FFN
	GateProj  *mlx.Array // [intermediate_size, hidden_size]
	UpProj    *mlx.Array // [intermediate_size, hidden_size]
	DownProj  *mlx.Array // [hidden_size, intermediate_size]
	// Optional biases
	QBias *mlx.Array
	KBias *mlx.Array
	VBias *mlx.Array
	OBias *mlx.Array
}
