//go:build darwin && arm64 && cgo && mlx

// Package qwen3 implements the Qwen3 transformer architecture for the gomlx
// LLM engine. It provides the forward pass (prefill + cached decode), weight
// loading, and RoPE/RMSNorm/GQA computation specific to Qwen3.
//
// Qwen3 architecture:
//   - Decoder-only transformer
//   - Grouped-Query Attention (GQA) with per-head QK RMSNorm
//   - Rotary Position Embeddings (RoPE, non-interleaved/half-split)
//   - SwiGLU feed-forward network
//   - RMSNorm (no bias on linear layers)
//   - Tied word embeddings (lm_head shares embed_tokens weight)
package qwen3

import (
	"fmt"
	"log"
	"math"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

func init() {
	llm.RegisterArchitecture("qwen3", New)
}

// Qwen3 implements the llm.Architecture interface for Qwen3 models.
type Qwen3 struct {
	cfg     llm.ModelConfig
	stream  *mlx.Stream
	weights *weights
}

// New creates a Qwen3 architecture instance from a ModelConfig.
func New(cfg llm.ModelConfig) (llm.Architecture, error) {
	if cfg.HeadDim == 0 {
		cfg.HeadDim = cfg.HiddenSize / cfg.NumHeads
	}
	cfg.UseQKNorm = true
	return &Qwen3{cfg: cfg}, nil
}

// qwen3Weights holds all model weights as MLX arrays.
type weights struct {
	embedTokens *mlx.Array // [vocab_size, hidden_size]
	layers      []layerWeights
	normWeight  *mlx.Array // [hidden_size] — final RMSNorm
}

type layerWeights struct {
	inputNorm *mlx.Array // [hidden_size]
	qProj     *mlx.Array // [num_heads * head_dim, hidden_size]
	kProj     *mlx.Array // [num_kv_heads * head_dim, hidden_size]
	vProj     *mlx.Array // [num_kv_heads * head_dim, hidden_size]
	oProj     *mlx.Array // [hidden_size, num_heads * head_dim]
	qNorm     *mlx.Array // [head_dim] — per-head RMSNorm
	kNorm     *mlx.Array // [head_dim] — per-head RMSNorm
	postNorm  *mlx.Array // [hidden_size]
	gateProj  *mlx.Array // [intermediate_size, hidden_size]
	upProj    *mlx.Array // [intermediate_size, hidden_size]
	downProj  *mlx.Array // [hidden_size, intermediate_size]
}

func (q *Qwen3) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen3) SetStream(s *mlx.Stream) { q.stream = s }

func (q *Qwen3) InitWeights(path string, s *mlx.Stream) error {
	q.stream = s

	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}

	w := &weights{
		layers: make([]layerWeights, q.cfg.NumLayers),
	}

	w.embedTokens, err = sf.Get("model.embed_tokens.weight", s)
	if err != nil {
		return fmt.Errorf("load embed_tokens: %w", err)
	}
	w.normWeight, err = sf.Get("model.norm.weight", s)
	if err != nil {
		return fmt.Errorf("load final norm: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		lw := &w.layers[i]
		p := fmt.Sprintf("model.layers.%d", i)

		lw.inputNorm, err = sf.Get(p+".input_layernorm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d input_norm: %w", i, err)
		}
		lw.qProj, err = sf.Get(p + ".self_attn.q_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d q_proj: %w", i, err)
		}
		lw.kProj, err = sf.Get(p + ".self_attn.k_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d k_proj: %w", i, err)
		}
		lw.vProj, err = sf.Get(p + ".self_attn.v_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d v_proj: %w", i, err)
		}
		lw.oProj, err = sf.Get(p + ".self_attn.o_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d o_proj: %w", i, err)
		}
		lw.qNorm, err = sf.Get(p + ".self_attn.q_norm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d q_norm: %w", i, err)
		}
		lw.kNorm, err = sf.Get(p + ".self_attn.k_norm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d k_norm: %w", i, err)
		}
		lw.postNorm, err = sf.Get(p + ".post_attention_layernorm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d post_norm: %w", i, err)
		}
		lw.gateProj, err = sf.Get(p + ".mlp.gate_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d gate_proj: %w", i, err)
		}
		lw.upProj, err = sf.Get(p + ".mlp.up_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d up_proj: %w", i, err)
		}
		lw.downProj, err = sf.Get(p + ".mlp.down_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d down_proj: %w", i, err)
		}
	}

	q.weights = w
	log.Printf("qwen3: %d layers loaded", q.cfg.NumLayers)
	return nil
}

func (q *Qwen3) FreeWeights() {
	if q.weights == nil {
		return
	}
	freeArr(q.weights.embedTokens)
	freeArr(q.weights.normWeight)
	for i := range q.weights.layers {
		freeArr(q.weights.layers[i].inputNorm)
		freeArr(q.weights.layers[i].qProj)
		freeArr(q.weights.layers[i].kProj)
		freeArr(q.weights.layers[i].vProj)
		freeArr(q.weights.layers[i].oProj)
		freeArr(q.weights.layers[i].qNorm)
		freeArr(q.weights.layers[i].kNorm)
		freeArr(q.weights.layers[i].postNorm)
		freeArr(q.weights.layers[i].gateProj)
		freeArr(q.weights.layers[i].upProj)
		freeArr(q.weights.layers[i].downProj)
	}
}

func freeArr(a *mlx.Array) {
	if a != nil {
		a.Free()
	}
}

// materialize creates an independent copy of an MLX array by reading its
// data and creating a new array. This is necessary for KV caching because
// MLX arrays are lazy — without materialization, cached arrays reference
// intermediate computation results that get freed when the function returns.
func materialize(a *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	data, err := a.Float32Data()
	if err != nil {
		return nil, err
	}
	return mlx.NewArrayFromFloat32(data, a.Shape())
}

// deepCopy is an alias for materialize.
func deepCopy(a *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	return materialize(a, s)
}

// ForwardPrefill processes the full prompt sequence and returns last-position logits.
func (q *Qwen3) ForwardPrefill(ids *mlx.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	s := q.stream

	h, err := llm.GatherAxis(q.weights.embedTokens, ids, 0, []int{1, q.cfg.HiddenSize}, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = mlx.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		out, err := q.forwardLayer(h, i, seqLen, 0, cache)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	// Final norm + lm_head
	logits, err := q.computeLogits(h)
	if err != nil {
		h.Free()
		return nil, err
	}
	defer logits.Free()

	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}

	data, err := logits.Float32Data()
	if err != nil {
		return nil, fmt.Errorf("read logits: %w", err)
	}

	// Extract last position
	vocabSize := q.cfg.VocabSize
	lastPos := (seqLen - 1) * vocabSize
	result := make([]float32, vocabSize)
	copy(result, data[lastPos:lastPos+vocabSize])
	return result, nil
}

// ForwardDecode processes a single token using the KV cache.
func (q *Qwen3) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
	s := q.stream

	idData := []int64{int64(tokenID)}
	idsArr, err := mlx.NewArrayFromInt64(idData, []int{1, 1})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := llm.GatherAxis(q.weights.embedTokens, idsArr, 0, []int{1, q.cfg.HiddenSize}, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = mlx.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		out, err := q.forwardLayer(h, i, 1, pos, cache)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	logits, err := q.computeLogits(h)
	if err != nil {
		h.Free()
		return nil, err
	}
	defer logits.Free()

	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}

	return logits.Float32Data()
}

// computeLogits applies final RMSNorm then projects to vocabulary via tied embeddings.
func (q *Qwen3) computeLogits(h *mlx.Array) (*mlx.Array, error) {
	s := q.stream
	normed, err := llm.RMSNorm(h, q.weights.normWeight, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()

	embedT, err := mlx.Transpose(q.weights.embedTokens, s)
	if err != nil {
		return nil, fmt.Errorf("transpose embed: %w", err)
	}
	defer embedT.Free()

	return mlx.MatMul(normed, embedT, s)
}

// forwardLayer runs one transformer layer with residual connections.
// seqLen is the number of tokens in THIS forward pass (1 during cached decode).
// startPos is the absolute position offset for RoPE.
// cache (if non-nil) stores K/V and provides cached values for attention.
func (q *Qwen3) forwardLayer(h *mlx.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream
	lw := &q.weights.layers[layerIdx]

	// Pre-attention RMSNorm
	normed, err := llm.RMSNorm(h, lw.inputNorm, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	// Self-attention
	attnOut, err := q.attention(normed, lw, layerIdx, seqLen, startPos, cache)
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}
	defer attnOut.Free()

	// Residual
	residual1, err := mlx.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	// Post-attention norm
	normed2, err := llm.RMSNorm(residual1, lw.postNorm, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	// SwiGLU FFN
	ffnOut, err := q.swiglu(normed2, lw)
	if err != nil {
		return nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	return mlx.Add(residual1, ffnOut, s)
}

// attention computes grouped-query attention with QK norm and RoPE.
func (q *Qwen3) attention(h *mlx.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream
	cfg := q.cfg

	q2d, err := llm.LinearNoBias(h, lw.qProj, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer q2d.Free()

	k2d, err := llm.LinearNoBias(h, lw.kProj, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := llm.LinearNoBias(h, lw.vProj, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	// Reshape Q/K/V to [1, seq, heads, head_dim]
	qR, err := mlx.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := mlx.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}

	vR, err := mlx.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}

	// QK Norm: RMSNorm over head_dim (last axis)
	qNormed, err := llm.RMSNorm(qR, lw.qNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR.Free()
	qR = qNormed

	kNormed, err := llm.RMSNorm(kR, lw.kNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	kR.Free()
	kR = kNormed

	// Transpose to [1, heads, seq, head_dim] for RoPE + attention
	qT, err := mlx.TransposeAxes(qR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("q transpose: %w", err)
	}
	defer qT.Free()

	kT, err := mlx.TransposeAxes(kR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("k transpose: %w", err)
	}
	defer kT.Free()

	vT, err := mlx.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("v transpose: %w", err)
	}
	defer vT.Free()

	// Apply RoPE
	qRot, err := llm.ApplyRoPE(qT, startPos, cfg.HeadDim, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := llm.ApplyRoPE(kT, startPos, cfg.HeadDim, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	// Build V for caching. We need vT's values after RoPE isn't applied to V.
	// V doesn't get RoPE, so vT is already correct.

	// KV Cache: store (prefill) or append (decode) K and V
	var kForAttn, vForAttn *mlx.Array
	if cache != nil {
		// Materialize K and V before caching — MLX arrays are lazy and the
		// intermediates they reference will be freed by deferred calls.
		if err := kRot.Eval(); err != nil {
			return nil, fmt.Errorf("eval kRot: %w", err)
		}
		if err := vT.Eval(); err != nil {
			return nil, fmt.Errorf("eval vT: %w", err)
		}
		if err := s.Synchronize(); err != nil {
			return nil, fmt.Errorf("sync: %w", err)
		}

		if !cache.IsInitialized(layerIdx) {
			// Prefill: store. Use the materialized arrays directly — they
			// are independent after Eval because MLX creates new buffers.
			// Cancel the deferred frees by nil-checking in a wrapper.
			kCopy, err := materialize(kRot, s)
			if err != nil {
				return nil, fmt.Errorf("materialize kRot: %w", err)
			}
			vCopy, err := materialize(vT, s)
			if err != nil {
				kCopy.Free()
				return nil, fmt.Errorf("materialize vT: %w", err)
			}
			if err := cache.Store(layerIdx, kCopy, vCopy); err != nil {
				return nil, fmt.Errorf("cache store: %w", err)
			}
		} else {
			// Decode: append to cache.
			kCopy, err := materialize(kRot, s)
			if err != nil {
				return nil, fmt.Errorf("materialize kRot: %w", err)
			}
			vCopy, err := materialize(vT, s)
			if err != nil {
				kCopy.Free()
				return nil, fmt.Errorf("materialize vT: %w", err)
			}
			if err := cache.Append(layerIdx, kCopy, vCopy); err != nil {
				return nil, fmt.Errorf("cache append: %w", err)
			}
		}

		cached, err := cache.Get(layerIdx)
		if err != nil {
			return nil, err
		}
		kForAttn = cached.K
		vForAttn = cached.V
	} else {
		kForAttn = kRot
		vForAttn = vT
	}

	// Expand KV heads for GQA
	kExp, err := llm.ExpandKVHeads(kForAttn, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("k expand: %w", err)
	}
	defer kExp.Free()

	vExp, err := llm.ExpandKVHeads(vForAttn, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("v expand: %w", err)
	}
	defer vExp.Free()

	// Attention scores: Q @ K^T / sqrt(head_dim)
	kT2, err := mlx.TransposeAxes(kExp, []int{0, 1, 3, 2}, s)
	if err != nil {
		return nil, fmt.Errorf("k^T: %w", err)
	}
	defer kT2.Free()

	scores, err := mlx.MatMul(qRot, kT2, s)
	if err != nil {
		return nil, fmt.Errorf("scores: %w", err)
	}
	defer scores.Free()

	scale := float32(1.0 / math.Sqrt(float64(cfg.HeadDim)))
	scaleArr, err := mlx.NewArrayFromFloat32([]float32{scale}, []int{1})
	if err != nil {
		return nil, err
	}
	defer scaleArr.Free()

	scaled, err := mlx.Multiply(scores, scaleArr, s)
	if err != nil {
		return nil, fmt.Errorf("scale scores: %w", err)
	}

	// Causal mask (only needed during prefill when seqLen > 1)
	if seqLen > 1 {
		cachedLen := 0
		if cache != nil {
			cachedLen = cache.CachedLen() - seqLen
		}
		masked, err := llm.ApplyCausalMask(scaled, seqLen, startPos, cachedLen, s)
		if err != nil {
			scaled.Free()
			return nil, fmt.Errorf("causal mask: %w", err)
		}
		scaled.Free()
		scaled = masked
	}
	defer scaled.Free()

	// Softmax
	probs, err := mlx.SoftmaxAxis(scaled, 3, s)
	if err != nil {
		return nil, fmt.Errorf("softmax: %w", err)
	}
	defer probs.Free()

	// Context: probs @ V
	ctx, err := mlx.MatMul(probs, vExp, s)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	defer ctx.Free()

	// Transpose + reshape back to [1, seq, hidden]
	ctxT, err := mlx.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := mlx.Reshape(ctxT, []int{1, seqLen, cfg.NumHeads * cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	return llm.LinearNoBias(ctxFlat, lw.oProj, s)
}

// swiglu computes the SwiGLU FFN: down(silu(gate(x)) * up(x))
func (q *Qwen3) swiglu(h *mlx.Array, lw *layerWeights) (*mlx.Array, error) {
	s := q.stream

	gate, err := llm.LinearNoBias(h, lw.gateProj, s)
	if err != nil {
		return nil, fmt.Errorf("gate proj: %w", err)
	}
	defer gate.Free()

	up, err := llm.LinearNoBias(h, lw.upProj, s)
	if err != nil {
		return nil, fmt.Errorf("up proj: %w", err)
	}
	defer up.Free()

	gateSilu, err := llm.SiLU(gate, s)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gateSilu.Free()

	gated, err := mlx.Multiply(gateSilu, up, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return llm.LinearNoBias(gated, lw.downProj, s)
}

// lockOSThread is a helper that ensures inference runs on a locked OS thread
// because MLX streams are thread-local.
func (q *Qwen3) lockOSThread() {
	runtime.LockOSThread()
}
