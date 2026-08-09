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
	"github.com/sprout-foundry/sprout/pkg/tensor"
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
	// InitWeights loads model weights from a safetensors file into tensor arrays.
	// Called once at model load time.
	InitWeights(path string, s tensor.Stream) error

	// SetStream sets the tensor stream used for subsequent forward passes.
	// Called before ForwardPrefill/ForwardDecode.
	SetStream(s tensor.Stream)

	// ForwardPrefill runs the forward pass over a full sequence of tokens.
	// Returns the logits for the last position. The KV cache (if non-nil) is
	// populated with keys/values from this pass for use in subsequent decode steps.
	ForwardPrefill(ids tensor.Array, seqLen int, cache *KVCache) ([]float32, error)

	// ForwardPrefillFrom runs the forward pass over a delta sequence that
	// extends an already-populated KV cache. startPos is the absolute
	// position of the FIRST token in ids (which determines RoPE offsets).
	// The cache must already contain startPos tokens. Used by prefix
	// caching to skip re-prefilling a shared prompt on repeated requests.
	ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *KVCache) ([]float32, error)

	// ForwardDecode runs the forward pass for a single token at the given
	// absolute position. Uses the KV cache to avoid recomputing past tokens.
	// Returns logits [vocabSize] for that position.
	ForwardDecode(tokenID int, pos int, cache *KVCache) ([]float32, error)

	// FreeWeights releases all tensor arrays held by the architecture.
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

// MTPArchitecture is an optional refinement implemented by architectures
// with a multi-token prediction head (e.g. raw Qwen3.5 HF exports carry
// mtp.* tensors; mlx-community conversions strip them). It enables
// self-speculative decoding: a cheap 1-layer MTP head drafts k tokens, the
// main model verifies them in ONE batched forward (~1 decode cost), and the
// longest accepted prefix is emitted. Throughput win for large models where
// the main model dominates per-token cost.
type MTPArchitecture interface {
	GreedyArchitecture

	// MTPAvailable reports whether the loaded weights include an MTP head.
	MTPAvailable() bool

	// ForwardDecodeMTP runs one MTP-assisted decode round at position pos.
	// nextToken is the token at position pos (just emitted); the KV cache
	// holds positions 0..pos-1. It drafts k tokens with the MTP head,
	// verifies them in ONE main-model batched forward, and returns the
	// tokens to emit: accepted drafts followed by the main model's own next
	// prediction (never empty). After the round the cache holds positions
	// 0..pos+len(out)-1 and the next round decodes out[len(out)-1] at
	// position pos+len(out).
	ForwardDecodeMTP(nextToken int, pos int, cache *KVCache, k int) ([]int, error)
}

// ModelConfig holds architecture-independent configuration values. Every
// decoder-only transformer shares these fields; architecture-specific values
// (e.g. QK norm presence, bias presence) are encoded as booleans that the
// Architecture implementation reads.
type ModelConfig struct {
	Arch              string // Architecture identifier: "qwen3", "qwen3_5_text", etc.
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
	UseQKNorm         bool         // Qwen3-style per-head Q/K RMSNorm
	UseAttentionBias  bool         // Llama uses bias on QKV projections
	UseTiedEmbeddings bool         // lm_head shares weights with embed_tokens
	MaxPosition       int          // Maximum context length
	Quantization      *QuantConfig // nil = full precision

	// Hybrid linear-attention fields (Qwen3.5 / Qwen3-Next style). Zero for
	// pure full-attention models like qwen3.
	FullAttentionInterval int      // every Nth layer is full attention; others are linear (3:1 → 4)
	LinearNumKeyHeads     int      // DeltaNet key heads
	LinearNumValueHeads   int      // DeltaNet value heads
	LinearKeyHeadDim      int      // DeltaNet key head dim
	LinearValueHeadDim    int      // DeltaNet value head dim
	LinearConvKernelDim   int      // DeltaNet conv kernel width
	AttnOutputGate        bool     // o_proj(out * sigmoid(gate)) — gated attention output
	PartialRotaryFactor   float32  // fraction of head dim rotated by RoPE (0.25 for qwen3_5)
	MRopeSection          []int    // mRoPE section sizes [T, H, W]
	MRopeInterleaved      bool     // mRoPE interleaved vs half-split
	MTPNumHiddenLayers    int      // multi-token prediction layers
	MTPUseDedicatedEmbeds bool     // MTP uses its own embedding table when true
	NumExperts            int      // MoE: total number of experts (0 = dense model)
	NumExpertsPerTok      int      // MoE: active experts per token
	MoEIntermediateSize   int      // MoE: intermediate size per expert
	SharedExpertInterSize int      // MoE: shared expert intermediate size
	NormTopkProb          bool     // MoE: normalize top-k probabilities
	WeightPrefix          string   // safetensors key prefix
	LayerTypes            []string // optional per-layer type override
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
	InputNorm tensor.Array // [hidden_size] — RMSNorm/LayerNorm before attention
	QProj     tensor.Array // [hidden_size, num_heads * head_dim] or [num_heads*head_dim, hidden_size]
	KProj     tensor.Array
	VProj     tensor.Array
	OProj     tensor.Array
	QNorm     tensor.Array // [head_dim] — optional, Qwen3-style per-head norm
	KNorm     tensor.Array // [head_dim] — optional, Qwen3-style per-head norm
	PostNorm  tensor.Array // [hidden_size] — norm after attention, before FFN
	GateProj  tensor.Array // [intermediate_size, hidden_size]
	UpProj    tensor.Array // [intermediate_size, hidden_size]
	DownProj  tensor.Array // [hidden_size, intermediate_size]
	// Optional biases
	QBias tensor.Array
	KBias tensor.Array
	VBias tensor.Array
	OBias tensor.Array
}
