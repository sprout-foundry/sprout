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

	// mlxSwigluClosures are compiled per-layer MLP closures, created lazily
	// on the inference thread (MLX compiled graphs bind to the thread-local
	// stream used at trace time). Keyed by stream pointer so a fresh per-call
	// stream triggers recompilation.
	mlxSwigluClosures []*mlx.Closure
	mlxSwigluStream   *mlx.Stream
}

func New(cfg llm.ModelConfig) (llm.Architecture, error) {
	if cfg.HeadDim == 0 {
		cfg.HeadDim = cfg.HiddenSize / cfg.NumHeads
	}
	cfg.UseQKNorm = true
	return &Qwen3{cfg: cfg}, nil
}

type weights struct {
	embedTokens  *mlx.Array
	embedTokensT *mlx.Array // transposed [hidden, vocab], cached at load
	layers       []layerWeights
	normWeight   *mlx.Array
}

type layerWeights struct {
	inputNorm *mlx.Array
	// Projection weights are stored pre-transposed in [in, out] layout so the
	// forward pass can MatMul directly without per-call transposes.
	qProj    *mlx.Array // [hidden, num_heads*head_dim]
	kProj    *mlx.Array // [hidden, num_kv_heads*head_dim]
	vProj    *mlx.Array // [hidden, num_kv_heads*head_dim]
	oProj    *mlx.Array // [num_heads*head_dim, hidden]
	qNorm    *mlx.Array
	kNorm    *mlx.Array
	postNorm *mlx.Array
	gateProj *mlx.Array // [hidden, intermediate]
	upProj   *mlx.Array // [hidden, intermediate]
	downProj *mlx.Array // [intermediate, hidden]
}

func (q *Qwen3) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen3) SetStream(s *mlx.Stream) { q.stream = s }

// loadLinearT loads a weight from safetensors and returns it pre-transposed
// into [in, out] layout for direct MatMul in the forward pass. The transpose
// is evaluated immediately on the loading thread so the result is a concrete
// buffer — lazy transposes would bind to the loading thread's stream and fail
// with "no Stream in current thread" when first evaluated during generation
// on a different OS thread.
func loadLinearT(sf *llm.SafetensorsFile, name string, s *mlx.Stream) (*mlx.Array, error) {
	w, err := sf.Get(name, s)
	if err != nil {
		return nil, err
	}
	wT, err := mlx.Transpose(w, s)
	if err != nil {
		w.Free()
		return nil, fmt.Errorf("transpose %s: %w", name, err)
	}
	w.Free()
	if err := wT.Eval(); err != nil {
		wT.Free()
		return nil, fmt.Errorf("eval transpose %s: %w", name, err)
	}
	return wT, nil
}

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
	w.embedTokensT, err = mlx.Transpose(w.embedTokens, s)
	if err != nil {
		return fmt.Errorf("transpose embed_tokens: %w", err)
	}
	// Materialize on the loading thread (see loadLinearT).
	if err := w.embedTokensT.Eval(); err != nil {
		return fmt.Errorf("eval transpose embed_tokens: %w", err)
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
		if lw.qProj, err = loadLinearT(sf, p+".self_attn.q_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d q_proj: %w", i, err)
		}
		if lw.kProj, err = loadLinearT(sf, p+".self_attn.k_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d k_proj: %w", i, err)
		}
		if lw.vProj, err = loadLinearT(sf, p+".self_attn.v_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d v_proj: %w", i, err)
		}
		if lw.oProj, err = loadLinearT(sf, p+".self_attn.o_proj.weight", s); err != nil {
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
		if lw.gateProj, err = loadLinearT(sf, p+".mlp.gate_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d gate_proj: %w", i, err)
		}
		if lw.upProj, err = loadLinearT(sf, p+".mlp.up_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d up_proj: %w", i, err)
		}
		if lw.downProj, err = loadLinearT(sf, p+".mlp.down_proj.weight", s); err != nil {
			return fmt.Errorf("load layer %d down_proj: %w", i, err)
		}
	}

	q.weights = w
	// The MLX arrays own copies of their buffers; release the multi-hundred-MB
	// file blob from the Go heap so GC can collect it.
	sf.Release()
	log.Printf("qwen3: %d layers loaded", q.cfg.NumLayers)
	return nil
}

func (q *Qwen3) FreeWeights() {
	for i, c := range q.mlxSwigluClosures {
		if c != nil {
			c.Free()
		}
		q.mlxSwigluClosures[i] = nil
	}
	q.mlxSwigluClosures = nil
	q.mlxSwigluStream = nil

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
	logits, err := q.prefillInternal(ids, seqLen, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

// prefillInternal runs the forward pass over the full prompt, returning the
// raw (BF16) logits array for the final position.
func (q *Qwen3) prefillInternal(ids *mlx.Array, seqLen int, cache *llm.KVCache) (*mlx.Array, error) {
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

	logits, err := q.computeLogitsLast(h, seqLen)
	if err != nil {
		h.Free()
		return nil, err
	}
	if err := s.Synchronize(); err != nil {
		logits.Free()
		return nil, fmt.Errorf("synchronize: %w", err)
	}
	return logits, nil
}

// ForwardDecode processes a single token using the KV cache.
func (q *Qwen3) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := q.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

// ForwardDecodeArgmax runs a single-token decode step and returns the
// GPU-computed argmax token ID.
func (q *Qwen3) ForwardDecodeArgmax(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	logits, err := q.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return 0, err
	}
	defer logits.Free()
	return q.logitsToArgmax(logits)
}

// decodeInternal runs the forward pass for a single token, returning the raw
// (BF16) logits array.
func (q *Qwen3) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (*mlx.Array, error) {
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

	// No explicit sync here: the caller's data read (Float32Data or
	// Uint32Data in logitsToFloat32/logitsToArgmax) evaluates the lazy graph
	// and synchronizes once. An extra sync here would cost ~5-10ms per token.
	return q.computeLogits(h)
}

// logitsToFloat32 casts a BF16 logits array to FP32 and reads it into a Go slice.
func (q *Qwen3) logitsToFloat32(logits *mlx.Array) ([]float32, error) {
	s := q.stream
	logitsF32, err := mlx.AsType(logits, mlx.Float32, s)
	if err != nil {
		return nil, fmt.Errorf("cast logits: %w", err)
	}
	defer logitsF32.Free()
	return logitsF32.Float32Data()
}

// logitsToArgmax computes the argmax of a logits array on the GPU and returns
// the token ID. The logits array is [1, 1, vocab]; the flattened argmax is
// exactly the vocab argmax.
func (q *Qwen3) logitsToArgmax(logits *mlx.Array) (int, error) {
	idxArr, err := mlx.ArgMax(logits, false, q.stream)
	if err != nil {
		return 0, fmt.Errorf("argmax: %w", err)
	}
	defer idxArr.Free()
	// Uint32Data's Eval evaluates the argmax (and its logits input) and
	// synchronizes once — an explicit Synchronize here would add a second
	// ~5-10ms pipeline drain per token.
	data, err := idxArr.Uint32Data()
	if err != nil {
		return 0, fmt.Errorf("read argmax: %w", err)
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("argmax returned no data")
	}
	return int(data[0]), nil
}

func (q *Qwen3) computeLogits(h *mlx.Array) (*mlx.Array, error) {
	s := q.stream
	normed, err := llm.RMSNorm(h, q.weights.normWeight, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()

	return mlx.MatMul(normed, q.weights.embedTokensT, s)
}

// computeLogitsLast slices h to the final position, then projects to vocab.
// The prefill only needs the last token's logits to sample the first output.
func (q *Qwen3) computeLogitsLast(h *mlx.Array, seqLen int) (*mlx.Array, error) {
	s := q.stream
	if seqLen > 1 {
		start := []int{0, seqLen - 1, 0}
		stop := []int{1, seqLen, q.cfg.HiddenSize}
		strides := []int{1, 1, 1}
		sliced, err := mlx.Slice(h, start, stop, strides, s)
		if err != nil {
			return nil, fmt.Errorf("slice last position: %w", err)
		}
		defer sliced.Free()
		return q.computeLogits(sliced)
	}
	return q.computeLogits(h)
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

	ffnOut, err := q.swiglu(normed2, lw, layerIdx)
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
