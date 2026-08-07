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

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

func init() {
	llm.RegisterArchitecture("qwen3", New)
}

type Qwen3 struct {
	cfg     llm.ModelConfig
	stream  *mlx.Stream
	weights *weights
}

func New(cfg llm.ModelConfig) (llm.Architecture, error) {
	if cfg.HeadDim == 0 {
		cfg.HeadDim = cfg.HiddenSize / cfg.NumHeads
	}
	cfg.UseQKNorm = true
	return &Qwen3{cfg: cfg}, nil
}

type weights struct {
	embedTokens *mlx.Array
	layers      []layerWeights
	normWeight  *mlx.Array
}

type layerWeights struct {
	inputNorm *mlx.Array
	qProj     *mlx.Array
	kProj     *mlx.Array
	vProj     *mlx.Array
	oProj     *mlx.Array
	qNorm     *mlx.Array
	kNorm     *mlx.Array
	postNorm  *mlx.Array
	gateProj  *mlx.Array
	upProj    *mlx.Array
	downProj  *mlx.Array
}

func (q *Qwen3) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen3) SetStream(s *mlx.Stream) { q.stream = s }

func (q *Qwen3) InitWeights(path string, s *mlx.Stream) error {
	q.stream = s

	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}

	w := &weights{layers: make([]layerWeights, q.cfg.NumLayers)}

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
		lw.qProj, err = sf.Get(p+".self_attn.q_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d q_proj: %w", i, err)
		}
		lw.kProj, err = sf.Get(p+".self_attn.k_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d k_proj: %w", i, err)
		}
		lw.vProj, err = sf.Get(p+".self_attn.v_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d v_proj: %w", i, err)
		}
		lw.oProj, err = sf.Get(p+".self_attn.o_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d o_proj: %w", i, err)
		}
		lw.qNorm, err = sf.Get(p+".self_attn.q_norm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d q_norm: %w", i, err)
		}
		lw.kNorm, err = sf.Get(p+".self_attn.k_norm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d k_norm: %w", i, err)
		}
		lw.postNorm, err = sf.Get(p+".post_attention_layernorm.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d post_norm: %w", i, err)
		}
		lw.gateProj, err = sf.Get(p+".mlp.gate_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d gate_proj: %w", i, err)
		}
		lw.upProj, err = sf.Get(p+".mlp.up_proj.weight", s)
		if err != nil {
			return fmt.Errorf("load layer %d up_proj: %w", i, err)
		}
		lw.downProj, err = sf.Get(p+".mlp.down_proj.weight", s)
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

	logits, err := q.computeLogits(h)
	if err != nil {
		h.Free()
		return nil, err
	}
	defer logits.Free()

	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}

	// Cast logits to FP32 for CPU-side sampling
	logitsF32, err := mlx.AsType(logits, mlx.Float32, s)
	if err != nil {
		return nil, fmt.Errorf("cast logits: %w", err)
	}
	defer logitsF32.Free()

	data, err := logitsF32.Float32Data()
	if err != nil {
		return nil, fmt.Errorf("read logits: %w", err)
	}

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

	// Cast logits to FP32 for CPU-side sampling
	logitsF32, err := mlx.AsType(logits, mlx.Float32, s)
	if err != nil {
		return nil, fmt.Errorf("cast logits: %w", err)
	}
	defer logitsF32.Free()

	return logitsF32.Float32Data()
}

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

func (q *Qwen3) forwardLayer(h *mlx.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream
	lw := &q.weights.layers[layerIdx]

	normed, err := llm.RMSNorm(h, lw.inputNorm, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	attnOut, err := q.attention(normed, lw, layerIdx, seqLen, startPos, cache)
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}
	defer attnOut.Free()

	residual1, err := mlx.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	normed2, err := llm.RMSNorm(residual1, lw.postNorm, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	ffnOut, err := q.swiglu(normed2, lw)
	if err != nil {
		return nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	return mlx.Add(residual1, ffnOut, s)
}

// attention computes grouped-query attention with QK norm and RoPE.
//
// KV cache strategy:
//   - Prefill (cache non-nil, layer not initialized): compute attention from
//     live K/V, then store retained copies in cache for future decode steps.
//     RetainArray increments MLX's C refcount without forcing evaluation —
//     calling Eval/Float32Data mid-pass corrupts the lazy computation graph.
//   - Decode (cache non-nil, layer initialized): concatenate new K/V to cache
//     via MLX ConcatenateAxis (lazy), use full cache for attention.
//   - No cache: compute attention from live K/V only.
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

	var kForAttn, vForAttn *mlx.Array

	if cache != nil && cache.IsInitialized(layerIdx) {
		// Decode: append new K/V to cache via lazy ConcatenateAxis
		cached, err := cache.Get(layerIdx)
		if err != nil {
			return nil, err
		}

		newK, err := mlx.ConcatenateAxis([]*mlx.Array{cached.K, kRot}, 2, s)
		if err != nil {
			return nil, fmt.Errorf("concat K: %w", err)
		}
		newV, err := mlx.ConcatenateAxis([]*mlx.Array{cached.V, vT}, 2, s)
		if err != nil {
			newK.Free()
			return nil, fmt.Errorf("concat V: %w", err)
		}

		cached.K.Free()
		cached.V.Free()
		cached.K = newK
		cached.V = newV

		kForAttn = newK
		vForAttn = newV

	} else if cache != nil {
		// Prefill: store retained copies of live K/V for future decode.
		// RetainArray increments the C refcount without forcing evaluation,
		// which would corrupt the lazy computation graph.
		kForAttn = kRot
		vForAttn = vT

		kRetained := mlx.RetainArray(kRot)
		vRetained := mlx.RetainArray(vT)
		if err := cache.Store(layerIdx, kRetained, vRetained); err != nil {
			kRetained.Free()
			vRetained.Free()
			return nil, fmt.Errorf("cache store: %w", err)
		}

	} else {
		kForAttn = kRot
		vForAttn = vT
	}

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
	scaleBF16, err := mlx.AsType(scaleArr, mlx.BFloat16, s)
	if err != nil {
		return nil, err
	}
	defer scaleBF16.Free()

	scaled, err := mlx.Multiply(scores, scaleBF16, s)
	if err != nil {
		return nil, fmt.Errorf("scale scores: %w", err)
	}

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

	probs, err := mlx.SoftmaxAxis(scaled, 3, s)
	if err != nil {
		return nil, fmt.Errorf("softmax: %w", err)
	}
	defer probs.Free()

	ctx, err := mlx.MatMul(probs, vExp, s)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	defer ctx.Free()

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
