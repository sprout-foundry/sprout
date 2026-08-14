//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

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
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	llm.RegisterArchitecture("qwen3", New)
}

type Qwen3 struct {
	cfg     llm.ModelConfig
	backend tensor.Backend
	stream  tensor.Stream
	weights *weights
}

func New(cfg llm.ModelConfig, backend tensor.Backend) (llm.Architecture, error) {
	if cfg.HeadDim == 0 {
		cfg.HeadDim = cfg.HiddenSize / cfg.NumHeads
	}
	cfg.UseQKNorm = true
	// Testing hook: GO_QUANTIZE=4|6|8 forces on-the-fly quantization of a
	// full-precision model (useful before downloading a pre-quantized one).
	// Overrides any quantization section in config.json.
	if bits := os.Getenv("GO_QUANTIZE"); bits != "" {
		var n int
		if _, err := fmt.Sscanf(bits, "%d", &n); err == nil && n >= 2 && n <= 8 {
			cfg.Quantization = &llm.QuantConfig{
				GroupSize: 64,
				Bits:      n,
				Mode:      "affine",
			}
			log.Printf("qwen3: forcing %d-bit quantization at load", n)
		}
	}
	return &Qwen3{cfg: cfg, backend: backend}, nil
}

type weights struct {
	embedTokens  tensor.Array
	embedTokensT tensor.Array // transposed [hidden, vocab], cached at load
	layers       []layerWeights
	normWeight   tensor.Array
}

type layerWeights struct {
	inputNorm tensor.Array
	// Projections dispatch between full-precision and quantized matmul in
	// Forward (see linear.go). Quantized weights stay in [out, in] PyTorch
	// layout; full precision is pre-transposed [in, out].
	qProj    *linear // [hidden, num_heads*head_dim]
	kProj    *linear // [hidden, num_kv_heads*head_dim]
	vProj    *linear // [hidden, num_kv_heads*head_dim]
	oProj    *linear // [num_heads*head_dim, hidden]
	qNorm    tensor.Array
	kNorm    tensor.Array
	postNorm tensor.Array
	gateProj *linear // [hidden, intermediate]
	upProj   *linear // [hidden, intermediate]
	downProj *linear // [intermediate, hidden]
}

func (q *Qwen3) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen3) SetStream(s tensor.Stream) { q.stream = s }

// loadLinearT loads a weight from safetensors and returns it pre-transposed
// into [in, out] layout for direct MatMul in the forward pass. The transpose
// is evaluated immediately on the loading thread so the result is a concrete
// buffer — lazy transposes would bind to the loading thread's stream and fail
// with "no Stream in current thread" when first evaluated during generation
// on a different OS thread.
func loadLinearT(sf *llm.SafetensorsFile, name string, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	w, err := sf.Get(name, b, s)
	if err != nil {
		return nil, err
	}
	wT, err := b.Transpose(w, s)
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

func (q *Qwen3) InitWeights(path string, s tensor.Stream) error {
	q.stream = s

	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}

	w := &weights{layers: make([]layerWeights, q.cfg.NumLayers)}

	// Load embedding. When the model has quantized embeddings (scales/biases
	// present), use LoadEmbedding which handles dequantization for backends
	// that lack native quantization (e.g. GGML CPU).
	if q.cfg.Quantization != nil && sf.Has("model.embed_tokens.scales") {
		emb, err := llm.LoadEmbedding(sf, "model.embed_tokens.weight", q.backend, s, q.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("load embed_tokens: %w", err)
		}
		w.embedTokens = emb.W()
		w.embedTokensT = emb.WT()
	} else {
		w.embedTokens, err = sf.Get("model.embed_tokens.weight", q.backend, s)
		if err != nil {
			return fmt.Errorf("load embed_tokens: %w", err)
		}
		w.embedTokensT, err = q.backend.Transpose(w.embedTokens, s)
		if err != nil {
			return fmt.Errorf("transpose embed_tokens: %w", err)
		}
		if err := w.embedTokensT.Eval(); err != nil {
			return fmt.Errorf("eval transpose embed_tokens: %w", err)
		}
	}
	w.normWeight, err = sf.Get("model.norm.weight", q.backend, s)
	if err != nil {
		return fmt.Errorf("load final norm: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		lw := &w.layers[i]
		p := fmt.Sprintf("model.layers.%d", i)
		quant := q.cfg.Quantization

		lw.inputNorm, err = sf.Get(p+".input_layernorm.weight", q.backend, s)
		if err != nil {
			return fmt.Errorf("load layer %d input_norm: %w", i, err)
		}
		if lw.qProj, err = loadLinear(sf, p+".self_attn.q_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d q_proj: %w", i, err)
		}
		if lw.kProj, err = loadLinear(sf, p+".self_attn.k_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d k_proj: %w", i, err)
		}
		if lw.vProj, err = loadLinear(sf, p+".self_attn.v_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d v_proj: %w", i, err)
		}
		if lw.oProj, err = loadLinear(sf, p+".self_attn.o_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d o_proj: %w", i, err)
		}
		lw.qNorm, err = sf.Get(p+".self_attn.q_norm.weight", q.backend, s)
		if err != nil {
			return fmt.Errorf("load layer %d q_norm: %w", i, err)
		}
		lw.kNorm, err = sf.Get(p+".self_attn.k_norm.weight", q.backend, s)
		if err != nil {
			return fmt.Errorf("load layer %d k_norm: %w", i, err)
		}
		lw.postNorm, err = sf.Get(p+".post_attention_layernorm.weight", q.backend, s)
		if err != nil {
			return fmt.Errorf("load layer %d post_norm: %w", i, err)
		}
		if lw.gateProj, err = loadLinear(sf, p+".mlp.gate_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d gate_proj: %w", i, err)
		}
		if lw.upProj, err = loadLinear(sf, p+".mlp.up_proj.weight", q.backend, s, quant); err != nil {
			return fmt.Errorf("load layer %d up_proj: %w", i, err)
		}
		if lw.downProj, err = loadLinear(sf, p+".mlp.down_proj.weight", q.backend, s, quant); err != nil {
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
	freeSwigluClosures(q)

	if q.weights == nil {
		return
	}
	q.weights.embedTokens.Free()
	q.weights.normWeight.Free()
	for i := range q.weights.layers {
		q.weights.layers[i].inputNorm.Free()
		q.weights.layers[i].qProj.Free()
		q.weights.layers[i].kProj.Free()
		q.weights.layers[i].vProj.Free()
		q.weights.layers[i].oProj.Free()
		q.weights.layers[i].qNorm.Free()
		q.weights.layers[i].kNorm.Free()
		q.weights.layers[i].postNorm.Free()
		q.weights.layers[i].gateProj.Free()
		q.weights.layers[i].upProj.Free()
		q.weights.layers[i].downProj.Free()
	}
}

// ForwardPrefill processes the full prompt sequence and returns last-position logits.
func (q *Qwen3) ForwardPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	return q.prefillAt(ids, seqLen, 0, cache)
}

// ForwardPrefillFrom prefills a delta sequence starting at an absolute
// position, extending an existing cache. RoPE offsets start at startPos, so
// a repeated prompt's shared prefix is not recomputed.
func (q *Qwen3) ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	return q.prefillAt(ids, seqLen, startPos, cache)
}

func (q *Qwen3) prefillAt(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := q.prefillInternal(ids, seqLen, startPos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

// prefillInternal runs the forward pass over the prompt (or a delta at
// startPos), returning the raw (BF16) logits array for the final position.
func (q *Qwen3) prefillInternal(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream

	h, err := llm.GatherAxis(q.weights.embedTokens, ids, 0, []int{1, q.cfg.HiddenSize}, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		out, err := q.forwardLayer(h, i, seqLen, startPos, cache)
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
func (q *Qwen3) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream

	idData := []int64{int64(tokenID)}
	idsArr, err := q.backend.NewArrayFromInt64(idData, []int{1, 1})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := llm.GatherAxis(q.weights.embedTokens, idsArr, 0, []int{1, q.cfg.HiddenSize}, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, s)
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
func (q *Qwen3) logitsToFloat32(logits tensor.Array) ([]float32, error) {
	s := q.stream
	logitsF32, err := q.backend.AsType(logits, tensor.Float32, s)
	if err != nil {
		return nil, fmt.Errorf("cast logits: %w", err)
	}
	defer logitsF32.Free()
	return logitsF32.Float32Data()
}

// logitsToArgmax computes the argmax of a logits array on the GPU and returns
// the token ID. The logits array is [1, 1, vocab]; the flattened argmax is
// exactly the vocab argmax.
func (q *Qwen3) logitsToArgmax(logits tensor.Array) (int, error) {
	idxArr, err := q.backend.ArgMax(logits, false, q.stream)
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

func (q *Qwen3) computeLogits(h tensor.Array) (tensor.Array, error) {
	s := q.stream
	normed, err := llm.RMSNorm(h, q.weights.normWeight, q.cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()

	// Q4_0 embeddings have no transposed copy — MatMul handles the
	// [out, in] layout natively via ggml_mul_mat.
	wT := q.weights.embedTokensT
	if wT == nil {
		wT = q.weights.embedTokens
	}
	return q.backend.MatMul(normed, wT, s)
}

// computeLogitsLast slices h to the final position, then projects to vocab.
// The prefill only needs the last token's logits to sample the first output.
func (q *Qwen3) computeLogitsLast(h tensor.Array, seqLen int) (tensor.Array, error) {
	s := q.stream
	if seqLen > 1 {
		start := []int{0, seqLen - 1, 0}
		stop := []int{1, seqLen, q.cfg.HiddenSize}
		strides := []int{1, 1, 1}
		sliced, err := q.backend.Slice(h, start, stop, strides, s)
		if err != nil {
			return nil, fmt.Errorf("slice last position: %w", err)
		}
		defer sliced.Free()
		return q.computeLogits(sliced)
	}
	return q.computeLogits(h)
}

func (q *Qwen3) forwardLayer(h tensor.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream
	lw := &q.weights.layers[layerIdx]

	normed, err := llm.RMSNorm(h, lw.inputNorm, q.cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	attnOut, err := q.attention(normed, lw, layerIdx, seqLen, startPos, cache)
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}
	defer attnOut.Free()

	residual1, err := q.backend.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	normed2, err := llm.RMSNorm(residual1, lw.postNorm, q.cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	ffnOut, err := q.swiglu(normed2, lw, layerIdx)
	if err != nil {
		return nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	return q.backend.Add(residual1, ffnOut, s)
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
