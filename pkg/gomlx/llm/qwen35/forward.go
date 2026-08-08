//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

func init() {
	llm.RegisterArchitecture("qwen3_5_text", New)
}

// Qwen35 implements the Qwen3.5 hybrid architecture: a 3:1 mix of Gated
// DeltaNet linear-attention layers and full attention. The decoder is wrapped
// in a multimodal shell (`qwen3_5` config) whose text_config carries
// model_type=qwen3_5_text and the hybrid hyperparameters. The full-attention
// layers use QK-norm, partial (25%) RoPE, and an output gate on the o_proj
// (attn_output_gate) — differences from plain qwen3.
type Qwen35 struct {
	cfg     llm.ModelConfig
	stream  *mlx.Stream
	weights *weights
}

func New(cfg llm.ModelConfig) (llm.Architecture, error) {
	if cfg.LinearConvKernelDim == 0 {
		cfg.LinearConvKernelDim = 4
	}
	return &Qwen35{cfg: cfg}, nil
}

func (q *Qwen35) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen35) SetStream(s *mlx.Stream) { q.stream = s }

// weights holds every MLX array for the model. embed is the (possibly
// quantized) word embedding. When UseTiedEmbeddings is set, embed.Logits is
// the lm_head; otherwise lmHead holds the separate untied projection.
type weights struct {
	embed      *llm.Embedding
	lmHead     *llm.Linear // non-nil when embeddings are untied
	normWeight *mlx.Array  // [hidden] — final RMSNorm
	layers     []layerWeights
}

// layerWeights are per-decoder-layer. Each layer has an MLP (shared between
// the two attention kinds) plus either a linear-attention block (DeltaNet) or
// a full-attention block. Which one is populated is decided per layer by
// full_attention_interval: layers with (idx+1) % interval == 0 are full.
type layerWeights struct {
	inputNorm *mlx.Array // [hidden] — RMSNorm before attention
	postNorm  *mlx.Array // [hidden] — RMSNorm before MLP

	linearAttn *gatedDeltaNet // non-nil for linear layers
	selfAttn   *selfAttnWeights // non-nil for full layers

	// MLP (SwiGLU) — shared by both layer kinds.
	gateProj *llm.Linear // [in, intermediate]
	upProj   *llm.Linear
	downProj *llm.Linear
}

type selfAttnWeights struct {
	qProj *llm.Linear // [hidden, num_heads * head_dim * 2] (carries output gate)
	kProj *llm.Linear
	vProj *llm.Linear
	oProj *llm.Linear
	qNorm *mlx.Array // [num_heads * head_dim]
	kNorm *mlx.Array // [num_kv_heads * head_dim]
}

// isLinearLayer reports whether layerIdx uses DeltaNet linear attention.
// With full_attention_interval=4: layers 0,1,2 linear, layer 3 full, ...
func isLinearLayer(layerIdx, interval int) bool {
	return (layerIdx+1)%interval != 0
}

// InitWeights loads the safetensors file(s), auto-detecting the weight key
// prefix (raw HF `model.language_model.` vs mlx-community `language_model.model.`).
func (q *Qwen35) InitWeights(path string, s *mlx.Stream) error {
	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}
	defer sf.Release()

	prefix := sf.DetectWeightPrefix([]string{"model.language_model.", "language_model.model."})
	if prefix == "" {
		return fmt.Errorf("qwen35: no recognized weight prefix in %s", path)
	}

	w := &weights{
		layers: make([]layerWeights, q.cfg.NumLayers),
	}

	w.embed, err = llm.LoadEmbedding(sf, prefix+"embed_tokens.weight", s, q.cfg.Quantization)
	if err != nil {
		return fmt.Errorf("load embed_tokens: %w", err)
	}

	// Untied lm_head (9B+ models): the head is a separate projection outside
	// the language_model.model.* namespace. In the mlx-community layout it
	// lives at language_model.lm_head.*; in the raw-HF layout it is
	// model.language_model.lm_head.*.
	if !q.cfg.UseTiedEmbeddings {
		lmPrefix := "language_model.lm_head."
		if strings.HasPrefix(prefix, "model.") {
			lmPrefix = "model.language_model.lm_head."
		}
		if sf.Has(lmPrefix + "weight") {
			w.lmHead, err = llm.LoadLinear(sf, lmPrefix+"weight", s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("load lm_head: %w", err)
			}
		} else {
			return fmt.Errorf("qwen35: tie_word_embeddings=false but no %slm_head.weight found", lmPrefix)
		}
	}

	w.normWeight, err = sf.Get(prefix+"norm.weight", s)
	if err != nil {
		return fmt.Errorf("load final norm: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		p := fmt.Sprintf("%slayers.%d", prefix, i)
		lw := &w.layers[i]

		lw.inputNorm, err = sf.Get(p+".input_layernorm.weight", s)
		if err != nil {
			return fmt.Errorf("layer %d input norm: %w", i, err)
		}
		lw.postNorm, err = sf.Get(p+".post_attention_layernorm.weight", s)
		if err != nil {
			return fmt.Errorf("layer %d post norm: %w", i, err)
		}

		if isLinearLayer(i, q.cfg.FullAttentionInterval) {
			dn := newGatedDeltaNet(q.cfg)
			if err := dn.loadWeights(sf, p+".linear_attn", s, q.cfg.Quantization); err != nil {
				return fmt.Errorf("layer %d linear_attn: %w", i, err)
			}
			lw.linearAttn = dn
		} else {
			sa := &selfAttnWeights{}
			sa.qProj, err = llm.LoadLinear(sf, p+".self_attn.q_proj", s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d q_proj: %w", i, err)
			}
			sa.kProj, err = llm.LoadLinear(sf, p+".self_attn.k_proj", s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d k_proj: %w", i, err)
			}
			sa.vProj, err = llm.LoadLinear(sf, p+".self_attn.v_proj", s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d v_proj: %w", i, err)
			}
			sa.oProj, err = llm.LoadLinear(sf, p+".self_attn.o_proj", s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d o_proj: %w", i, err)
			}
			sa.qNorm, err = sf.Get(p+".self_attn.q_norm.weight", s)
			if err != nil {
				return fmt.Errorf("layer %d q_norm: %w", i, err)
			}
			sa.kNorm, err = sf.Get(p+".self_attn.k_norm.weight", s)
			if err != nil {
				return fmt.Errorf("layer %d k_norm: %w", i, err)
			}
			lw.selfAttn = sa
		}

		// MLP (shared by both layer kinds).
		lw.gateProj, err = llm.LoadLinear(sf, p+".mlp.gate_proj", s, q.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d gate_proj: %w", i, err)
		}
		lw.upProj, err = llm.LoadLinear(sf, p+".mlp.up_proj", s, q.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d up_proj: %w", i, err)
		}
		lw.downProj, err = llm.LoadLinear(sf, p+".mlp.down_proj", s, q.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d down_proj: %w", i, err)
		}
	}

	q.weights = w

	// Release the file blob from the Go heap so GC can collect it.
	sf.Release()
	runtime.GC()
	log.Printf("qwen35: %d layers loaded (prefix %q)", q.cfg.NumLayers, prefix)
	return nil
}

func (q *Qwen35) FreeWeights() {
	if q.weights == nil {
		return
	}
	q.weights.embed.Free()
	if q.weights.lmHead != nil {
		q.weights.lmHead.Free()
	}
	freeArr(q.weights.normWeight)
	for i := range q.weights.layers {
		lw := &q.weights.layers[i]
		freeArr(lw.inputNorm)
		freeArr(lw.postNorm)
		if lw.linearAttn != nil {
			lw.linearAttn.free()
		}
		if lw.selfAttn != nil {
			lw.selfAttn.qProj.Free()
			lw.selfAttn.kProj.Free()
			lw.selfAttn.vProj.Free()
			lw.selfAttn.oProj.Free()
			freeArr(lw.selfAttn.qNorm)
			freeArr(lw.selfAttn.kNorm)
		}
		lw.gateProj.Free()
		lw.upProj.Free()
		lw.downProj.Free()
	}
	q.weights = nil
}

func freeArr(a *mlx.Array) {
	if a != nil {
		a.Free()
	}
}

// ----------------------------------------------------------------------------
// Forward passes
// ----------------------------------------------------------------------------

func (q *Qwen35) ForwardPrefill(ids *mlx.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	logits, err := q.prefillInternal(ids, seqLen, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

func (q *Qwen35) prefillInternal(ids *mlx.Array, seqLen int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream

	h, err := q.weights.embed.Lookup(ids, s)
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

// DebugDumpPrefill runs the prefill forward and writes each layer's hidden
// state (post-MLP residual, [1, seqLen, hidden], f32) to dumpDir/layer-NN.bin
// plus dumpDir/prefill.logits.bin. Used only by the parity harness to compare
// against mlx-lm layer by layer. Call on the locked inference thread.
func (q *Qwen35) DebugDumpPrefill(ids *mlx.Array, seqLen int, cache *llm.KVCache, dumpDir string) error {
	s := q.stream

	h, err := q.weights.embed.Lookup(ids, s)
	if err != nil {
		return fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = mlx.SqueezeAxis(h, 2, s)
	if err != nil {
		return fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		out, err := q.forwardLayer(h, i, seqLen, 0, cache)
		if err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
		if err := dumpArrayF32(h, fmt.Sprintf("%s/layer-%02d.bin", dumpDir, i), s); err != nil {
			return err
		}
	}

	logits, err := q.computeLogitsLast(h, seqLen)
	if err != nil {
		h.Free()
		return err
	}
	defer logits.Free()
	if err := s.Synchronize(); err != nil {
		return fmt.Errorf("synchronize: %w", err)
	}
	return dumpArrayF32(logits, dumpDir+"/prefill.logits.bin", s)
}

func (q *Qwen35) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := q.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

// dumpArrayF32 writes an array's Float32 data to path (evaluates it first).
// For BF16 arrays the cast to Float32 happens through AsType on stream s.
func dumpArrayF32(a *mlx.Array, path string, s *mlx.Stream) error {
	var data []float32
	if a.Dtype() == mlx.Float32 {
		d, err := a.Float32Data()
		if err != nil {
			return fmt.Errorf("dump %s: %w", path, err)
		}
		data = d
	} else {
		f32, err := mlx.AsType(a, mlx.Float32, s)
		if err != nil {
			return fmt.Errorf("cast %s: %w", path, err)
		}
		defer f32.Free()
		d, err := f32.Float32Data()
		if err != nil {
			return fmt.Errorf("dump cast %s: %w", path, err)
		}
		data = d
	}
	buf := make([]byte, len(data)*4)
	for i, v := range data {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return os.WriteFile(path, buf, 0o644)
}

func (q *Qwen35) ForwardDecodeArgmax(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	logits, err := q.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return 0, err
	}
	defer logits.Free()
	return q.logitsToArgmax(logits)
}

func (q *Qwen35) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream

	idData := []int64{int64(tokenID)}
	idsArr, err := mlx.NewArrayFromInt64(idData, []int{1, 1})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := q.weights.embed.Lookup(idsArr, s)
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
	if err := s.Synchronize(); err != nil {
		logits.Free()
		return nil, fmt.Errorf("synchronize: %w", err)
	}
	return logits, nil
}

func (q *Qwen35) logitsToFloat32(logits *mlx.Array) ([]float32, error) {
	return logits.Float32Data()
}

func (q *Qwen35) logitsToArgmax(logits *mlx.Array) (int, error) {
	// The logits array is [1, 1, vocab]; the flattened argmax is exactly the
	// vocab argmax.
	idxArr, err := mlx.ArgMax(logits, false, q.stream)
	if err != nil {
		return 0, fmt.Errorf("argmax: %w", err)
	}
	defer idxArr.Free()
	data, err := idxArr.Uint32Data()
	if err != nil {
		return 0, fmt.Errorf("read argmax: %w", err)
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("argmax returned no data")
	}
	return int(data[0]), nil
}

func (q *Qwen35) computeLogits(h *mlx.Array) (*mlx.Array, error) {
	s := q.stream
	normed, err := llm.RMSNorm(h, q.weights.normWeight, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()

	if q.weights.lmHead != nil {
		// Untied embeddings: separate lm_head projection.
		return q.weights.lmHead.Forward(normed, s)
	}
	return q.weights.embed.Logits(normed, s)
}

func (q *Qwen35) computeLogitsLast(h *mlx.Array, seqLen int) (*mlx.Array, error) {
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

// forwardLayer dispatches per layer: DeltaNet linear attention for
// (layerIdx+1)%interval != 0, full attention otherwise. Both paths share the
// same pre/post RMSNorm and SwiGLU MLP.
func (q *Qwen35) forwardLayer(h *mlx.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream
	lw := &q.weights.layers[layerIdx]

	normed, err := llm.RMSNorm(h, lw.inputNorm, q.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	var attnOut *mlx.Array
	if lw.linearAttn != nil {
		attnOut, err = lw.linearAttn.forward(normed, cache, layerIdx, seqLen, s)
		if err != nil {
			return nil, fmt.Errorf("linear attention: %w", err)
		}
	} else {
		attnOut, err = q.fullAttention(normed, lw, layerIdx, seqLen, startPos, cache)
		if err != nil {
			return nil, fmt.Errorf("full attention: %w", err)
		}
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

// fullAttention is Qwen3-style GQA with two qwen3.5 differences: the Q
// projection is 2× the head count (carrying the output gate in the second
// half) and RoPE rotates only partial_rotary_factor of the head dim.
func (q *Qwen35) fullAttention(h *mlx.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (*mlx.Array, error) {
	s := q.stream
	cfg := q.cfg
	sa := lw.selfAttn

	headDim := cfg.HeadDim
	outDim := cfg.NumHeads * headDim

	// Q projection outputs 2*outDim per head block (num_heads, 2*head_dim).
	// The reference splits the LAST axis of the reshaped [B,L,H,-1] tensor:
	// the first head_dim of each head's block is the query, the second half
	// is the output gate. A flat [0:outDim]/[outDim:2*outDim] split would
	// interleave whole heads into the wrong halves — reshape first, then
	// slice the last axis, then flatten the gate back.
	qFull, err := sa.qProj.Forward(h, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer qFull.Free()

	qFull4, err := mlx.Reshape(qFull, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape4: %w", err)
	}
	defer qFull4.Free()

	q2d, err := mlx.Slice(qFull4, []int{0, 0, 0, 0}, []int{1, seqLen, cfg.NumHeads, headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("q slice: %w", err)
	}
	defer q2d.Free()

	gate4, err := mlx.Slice(qFull4, []int{0, 0, 0, headDim}, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("q gate slice: %w", err)
	}
	defer gate4.Free()
	qGate, err := mlx.Reshape(gate4, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q gate flatten: %w", err)
	}
	defer qGate.Free()

	k2d, err := sa.kProj.Forward(h, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := sa.vProj.Forward(h, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	qR, err := mlx.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := mlx.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}
	defer kR.Free()

	vR, err := mlx.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}
	defer vR.Free()

	qNormed, err := llm.RMSNorm(qR, sa.qNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR = qNormed

	kNormed, err := llm.RMSNorm(kR, sa.kNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	defer kNormed.Free()
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

	// Partial RoPE: rotate only partialRotaryFactor of the head dim.
	ropeDims := int(float64(headDim) * float64(cfg.PartialRotaryFactor))
	qRot, err := llm.ApplyRoPEFast(qT, startPos, ropeDims, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := llm.ApplyRoPEFast(kT, startPos, ropeDims, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	var kForAttn, vForAttn *mlx.Array
	if cache != nil && cache.IsInitialized(layerIdx) {
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

	// Expand K/V to num_heads and run fused SDPA.
	kExp, err := llm.ExpandKVHeads(kForAttn, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("expand K heads: %w", err)
	}
	defer kExp.Free()

	vExp, err := llm.ExpandKVHeads(vForAttn, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("expand V heads: %w", err)
	}
	defer vExp.Free()

	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	scale := float32(1.0 / sqrt(float64(headDim)))
	ctx, err := mlx.FastScaledDotProductAttention(qT, kExp, vExp, scale, maskMode, nil, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fused attention: %w", err)
	}
	defer ctx.Free()

	ctxT, err := mlx.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := mlx.Reshape(ctxT, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	// attn_output_gate: gate the attention output BEFORE o_proj (the gate is
	// num_heads*head_dim wide, matching the attention output, not hidden).
	gateSig, err := mlx.Sigmoid(qGate, s)
	if err != nil {
		return nil, fmt.Errorf("gate sigmoid: %w", err)
	}
	defer gateSig.Free()

	gated, err := mlx.Multiply(ctxFlat, gateSig, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return sa.oProj.Forward(gated, s)
}

// swiglu is the SwiGLU MLP shared by both layer kinds.
func (q *Qwen35) swiglu(h *mlx.Array, lw *layerWeights) (*mlx.Array, error) {
	s := q.stream

	gate, err := lw.gateProj.Forward(h, s)
	if err != nil {
		return nil, fmt.Errorf("gate proj: %w", err)
	}
	defer gate.Free()

	up, err := lw.upProj.Forward(h, s)
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

	return lw.downProj.Forward(gated, s)
}
