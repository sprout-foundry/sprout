//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	llm.RegisterArchitecture("qwen3_5_text", New)
	llm.RegisterArchitecture("qwen3_5_moe_text", New)
}

// Qwen35 implements the Qwen3.5 hybrid architecture: a 3:1 mix of Gated
// DeltaNet linear-attention layers and full attention. The decoder is wrapped
// in a multimodal shell (`qwen3_5` config) whose text_config carries
// model_type=qwen3_5_text and the hybrid hyperparameters. The full-attention
// layers use QK-norm, partial (25%) RoPE, and an output gate on the o_proj
// (attn_output_gate) — differences from plain qwen3.
type Qwen35 struct {
	cfg        llm.ModelConfig
	backend    tensor.Backend
	stream     tensor.Stream
	weights    *weights
	mtp        *mtpWeights // multi-token prediction head; nil when absent
	lastHidden tensor.Array // retained [1,1,H] main-model hidden at the last processed position

	// normPreAdded is true for mlx-community exports where sanitize() has
	// already added 1 to the RMSNorm weights. When true, rmsNormQwen35
	// uses plain multiplication instead of (1+w).
	normPreAdded bool
}

func New(cfg llm.ModelConfig, backend tensor.Backend) (llm.Architecture, error) {
	if cfg.LinearConvKernelDim == 0 {
		cfg.LinearConvKernelDim = 4
	}
	// Testing hook: GO_QUANTIZE=4|6|8 forces on-the-fly quantization of a
	// full-precision model (useful for verifying MTP / forward pass under
	// quantization without a pre-quantized export). Overrides config.json.
	if bits := os.Getenv("GO_QUANTIZE"); bits != "" {
		var n int
		if _, err := fmt.Sscanf(bits, "%d", &n); err == nil && n >= 2 && n <= 8 {
			cfg.Quantization = &llm.QuantConfig{
				GroupSize: 64,
				Bits:      n,
				Mode:      "affine",
			}
			log.Printf("qwen35: forcing %d-bit quantization at load", n)
		}
	}
	return &Qwen35{cfg: cfg, backend: backend}, nil
}

func (q *Qwen35) Config() llm.ModelConfig { return q.cfg }

func (q *Qwen35) SetStream(s tensor.Stream) { q.stream = s }

// weights holds every MLX array for the model. embed is the (possibly
// quantized) word embedding. When UseTiedEmbeddings is set, embed.Logits is
// the lm_head; otherwise lmHead holds the separate untied projection.
type weights struct {
	embed      *llm.Embedding
	lmHead     *llm.Linear // non-nil when embeddings are untied
	normWeight tensor.Array  // [hidden] — final RMSNorm
	layers     []layerWeights
}

// layerWeights are per-decoder-layer. Each layer has an MLP (shared between
// the two attention kinds) plus either a linear-attention block (DeltaNet) or
// a full-attention block. Which one is populated is decided per layer by
// full_attention_interval: layers with (idx+1) % interval == 0 are full.
type layerWeights struct {
	inputNorm tensor.Array // [hidden] — RMSNorm before attention
	postNorm  tensor.Array // [hidden] — RMSNorm before MLP

	linearAttn *gatedDeltaNet   // non-nil for linear layers
	selfAttn   *selfAttnWeights // non-nil for full layers

	// MLP — dense SwiGLU (gateProj/upProj/downProj) OR MoE (moe).
	// When moe is non-nil, the dense projections are nil.
	gateProj *llm.Linear // [in, intermediate]
	upProj   *llm.Linear
	downProj *llm.Linear
	moe      *sparseMoeBlock // non-nil for MoE layers
}

type selfAttnWeights struct {
	qProj *llm.Linear // [hidden, num_heads * head_dim * 2] (carries output gate)
	kProj *llm.Linear
	vProj *llm.Linear
	oProj *llm.Linear
	qNorm tensor.Array // [num_heads * head_dim]
	kNorm tensor.Array // [num_kv_heads * head_dim]
}

// isLinearLayer reports whether layerIdx uses DeltaNet linear attention.
// With full_attention_interval=4: layers 0,1,2 linear, layer 3 full, ...
func isLinearLayer(layerIdx, interval int) bool {
	return (layerIdx+1)%interval != 0
}

// InitWeights loads the safetensors file(s), auto-detecting the weight key
// prefix (raw HF `model.language_model.` vs mlx-community `language_model.model.`).
func (q *Qwen35) InitWeights(path string, s tensor.Stream) error {
	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}
	defer sf.Release()

	prefix := sf.DetectWeightPrefix([]string{"model.language_model.", "language_model.model."})
	if prefix == "" {
		return fmt.Errorf("qwen35: no recognized weight prefix in %s", path)
	}

	// mlx-community sanitize() pre-adds 1 to norm weights; raw HF does not.
	q.normPreAdded = prefix == "language_model.model."

	w := &weights{
		layers: make([]layerWeights, q.cfg.NumLayers),
	}

	w.embed, err = llm.LoadEmbedding(sf, prefix+"embed_tokens.weight", q.backend, s, q.cfg.Quantization)
	if err != nil {
		return fmt.Errorf("load embed_tokens: %w", err)
	}

	// Untied lm_head (9B+ models): the head is a separate projection outside
	// the language_model.model.* namespace. In the mlx-community layout it
	// lives at language_model.lm_head.*; in the raw-HF layout it can be
	// either model.language_model.lm_head.* (4B) or top-level lm_head.* (9B).
	if !q.cfg.UseTiedEmbeddings {
		candidates := []string{
			"lm_head.",                            // raw HF 9B (top-level)
			"model.language_model.lm_head.",       // raw HF 4B
			"language_model.lm_head.",             // mlx-community
		}
		var lmPrefix string
		for _, c := range candidates {
			if sf.Has(c + "weight") {
				lmPrefix = c
				break
			}
		}
		if lmPrefix == "" {
			return fmt.Errorf("qwen35: tie_word_embeddings=false but no lm_head.weight found (tried %v)", candidates)
		}
		w.lmHead, err = llm.LoadLinear(sf, lmPrefix+"weight", q.backend, s, q.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("load lm_head: %w", err)
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
			if err := dn.loadWeights(sf, p+".linear_attn", q.backend, s, q.cfg.Quantization); err != nil {
				return fmt.Errorf("layer %d linear_attn: %w", i, err)
			}
			lw.linearAttn = dn
		} else {
			sa := &selfAttnWeights{}
			sa.qProj, err = llm.LoadLinear(sf, p+".self_attn.q_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d q_proj: %w", i, err)
			}
			sa.kProj, err = llm.LoadLinear(sf, p+".self_attn.k_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d k_proj: %w", i, err)
			}
			sa.vProj, err = llm.LoadLinear(sf, p+".self_attn.v_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d v_proj: %w", i, err)
			}
			sa.oProj, err = llm.LoadLinear(sf, p+".self_attn.o_proj.weight", q.backend, s, q.cfg.Quantization)
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

		// MLP: dense SwiGLU or MoE sparse block.
		if q.cfg.NumExperts > 0 {
			// MoE layer
			moe := &sparseMoeBlock{
				numExperts:       q.cfg.NumExperts,
				numExpertsPerTok: q.cfg.NumExpertsPerTok,
				normTopkProb:     q.cfg.NormTopkProb,
			}
			if err := moe.loadWeights(sf, p, q.backend, s, q.cfg.Quantization); err != nil {
				return fmt.Errorf("layer %d moe: %w", i, err)
			}
			lw.moe = moe
		} else {
			// Dense SwiGLU
			lw.gateProj, err = llm.LoadLinear(sf, p+".mlp.gate_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d gate_proj: %w", i, err)
			}
			lw.upProj, err = llm.LoadLinear(sf, p+".mlp.up_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d up_proj: %w", i, err)
			}
			lw.downProj, err = llm.LoadLinear(sf, p+".mlp.down_proj.weight", q.backend, s, q.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d down_proj: %w", i, err)
			}
		}
	}

	q.weights = w

	// MTP head (multi-token prediction). Raw-HF Qwen3.5 exports carry mtp.*
	// tensors; mlx-community conversions strip them, so mtp stays nil there.
	q.mtp, err = loadMTPWeights(sf, q.backend, s, q.cfg.Quantization)
	if err != nil {
		return fmt.Errorf("load mtp: %w", err)
	}

	// Release the file blob from the Go heap so GC can collect it.
	sf.Release()
	runtime.GC()
	if q.mtp != nil {
		log.Printf("qwen35: %d layers loaded (prefix %q) + MTP head", q.cfg.NumLayers, prefix)
	} else {
		log.Printf("qwen35: %d layers loaded (prefix %q)", q.cfg.NumLayers, prefix)
	}
	return nil
}

func (q *Qwen35) FreeWeights() {
	if q.weights == nil {
		return
	}
	if q.lastHidden != nil {
		q.lastHidden.Free()
		q.lastHidden = nil
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
		if lw.gateProj != nil {
			lw.gateProj.Free()
		}
		if lw.upProj != nil {
			lw.upProj.Free()
		}
		if lw.downProj != nil {
			lw.downProj.Free()
		}
		if lw.moe != nil {
			lw.moe.free()
		}
	}
	if q.mtp != nil {
		q.mtp.Free()
		q.mtp = nil
	}
	q.weights = nil
}

func freeArr(a tensor.Array) {
	if a != nil {
		a.Free()
	}
}

// ----------------------------------------------------------------------------
// Forward passes
// ----------------------------------------------------------------------------

func (q *Qwen35) ForwardPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	return q.prefillAt(ids, seqLen, 0, cache)
}

// ForwardPrefillFrom prefills a delta sequence starting at an absolute
// position, extending an existing cache. RoPE offsets start at startPos, so
// a repeated prompt's shared prefix is not recomputed.
func (q *Qwen35) ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	return q.prefillAt(ids, seqLen, startPos, cache)
}

func (q *Qwen35) prefillAt(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := q.prefillInternal(ids, seqLen, startPos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return q.logitsToFloat32(logits)
}

func (q *Qwen35) prefillInternal(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	// Chunked prefill: process large sequences in segments to avoid
	// exceeding Metal's maximum buffer size (~9.5GB on 16GB machines).
	// Each chunk runs through all layers, attending to itself + cached K/V.
	const prefillChunkSize = 4096

	if seqLen <= prefillChunkSize || cache == nil {
		return q.prefillInternalChunk(ids, seqLen, startPos, cache)
	}

	s := q.stream
	idsShape := ids.Shape()

	// Process all but the last chunk as cache-building passes.
	// We don't need logits from these — just the KV cache extension.
	for processed := 0; processed+prefillChunkSize < seqLen; processed += prefillChunkSize {
		chunkLen := prefillChunkSize
		chunkIDs, err := q.backend.Slice(ids, []int{0, processed}, []int{idsShape[0], processed + chunkLen}, []int{1, 1}, s)
		if err != nil {
			return nil, fmt.Errorf("prefill chunk slice at %d: %w", processed, err)
		}
		_, err = q.prefillInternalChunk(chunkIDs, chunkLen, startPos+processed, cache)
		chunkIDs.Free()
		if err != nil {
			return nil, fmt.Errorf("prefill chunk at %d: %w", processed, err)
		}
	}

	// Process the last chunk normally — its logits are at the final position.
	lastChunkStart := ((seqLen - 1) / prefillChunkSize) * prefillChunkSize
	lastChunkLen := seqLen - lastChunkStart
	lastChunkIDs, err := q.backend.Slice(ids, []int{0, lastChunkStart}, []int{idsShape[0], seqLen}, []int{1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("last chunk slice: %w", err)
	}
	defer lastChunkIDs.Free()
	return q.prefillInternalChunk(lastChunkIDs, lastChunkLen, startPos+lastChunkStart, cache)
}

// prefillInternalChunk runs the full model over a single chunk of tokens.
func (q *Qwen35) prefillInternalChunk(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream

	h, err := q.weights.embed.Lookup(ids, q.backend, s)
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

	if err := q.setLastHidden(h, seqLen); err != nil {
		h.Free()
		return nil, err
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
func (q *Qwen35) DebugDumpPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache, dumpDir string) error {
	s := q.stream

	h, err := q.weights.embed.Lookup(ids, q.backend, s)
	if err != nil {
		return fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, s)
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
		if err := dumpArrayF32(h, fmt.Sprintf("%s/layer-%02d.bin", dumpDir, i), q.backend, s); err != nil {
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
	return dumpArrayF32(logits, dumpDir+"/prefill.logits.bin", q.backend, s)
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
func dumpArrayF32(a tensor.Array, path string, backend tensor.Backend, s tensor.Stream) error {
	var data []float32
	if a.Dtype() == tensor.Float32 {
		d, err := a.Float32Data()
		if err != nil {
			return fmt.Errorf("dump %s: %w", path, err)
		}
		data = d
	} else {
		f32, err := backend.AsType(a, tensor.Float32, s)
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

func (q *Qwen35) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream

	idData := []int64{int64(tokenID)}
	idsArr, err := q.backend.NewArrayFromInt64(idData, []int{1, 1})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := q.weights.embed.Lookup(idsArr, q.backend, s)
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

	if err := q.setLastHidden(h, 1); err != nil {
		h.Free()
		return nil, err
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

func (q *Qwen35) logitsToFloat32(logits tensor.Array) ([]float32, error) {
	return logits.Float32Data()
}

func (q *Qwen35) logitsToArgmax(logits tensor.Array) (int, error) {
	// The logits array is [1, 1, vocab]; the flattened argmax is exactly the
	// vocab argmax.
	idxArr, err := q.backend.ArgMax(logits, false, q.stream)
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

// setLastHidden retains the hidden state at the LAST position of h
// ([1, seqLen, H] -> [1, 1, H]) so the MTP head can chain a draft from the
// previous main-model position. Replaces any previously retained hidden.
func (q *Qwen35) setLastHidden(h tensor.Array, seqLen int) error {
	if q.lastHidden != nil {
		q.lastHidden.Free()
		q.lastHidden = nil
	}
	var last tensor.Array
	if seqLen > 1 {
		start := []int{0, seqLen - 1, 0}
		stop := []int{1, seqLen, q.cfg.HiddenSize}
		strides := []int{1, 1, 1}
		sliced, err := q.backend.Slice(h, start, stop, strides, q.stream)
		if err != nil {
			return fmt.Errorf("slice last hidden: %w", err)
		}
		last = sliced
	} else {
		last = q.backend.RetainArray(h)
	}
	q.lastHidden = last
	return nil
}

// LastHidden returns the retained main-model hidden state at the last
// processed position. The caller must not free it.
func (q *Qwen35) LastHidden() tensor.Array { return q.lastHidden }

// MTPAvailable reports whether the loaded weights include a multi-token
// prediction head (raw Qwen3.5 HF exports; mlx-community conversions strip
// mtp.* so they report false).
func (q *Qwen35) MTPAvailable() bool { return q.mtp != nil }

// MTPDraft produces k draft tokens for positions t+2..t+k+1 from the main
// model's hidden state at t (prevHidden) and the token at t+1 (nextToken).
// The chain reuses each MTP step's decoder-layer output as the next step's
// prevHidden (DeepSeek-V3 self-referential drafting). prevHidden must be a
// retained array owned by the caller; it is not freed here.
func (q *Qwen35) MTPDraft(prevHidden tensor.Array, nextToken int, k int) ([]int, error) {
	if q.mtp == nil {
		return nil, fmt.Errorf("mtp: head not loaded")
	}
	if prevHidden == nil {
		return nil, fmt.Errorf("mtp: nil prevHidden")
	}
	return q.mtp.draftChain(q, prevHidden, nextToken, k)
}

// ForwardPrefillArgmaxAll runs the main model over a delta sequence and
// returns the argmax token at EVERY position (not just the last). Used to
// verify MTP drafts: the model predicts position p+1 from position p, so the
// draft at p+1 is accepted iff argmax[p] == draft. The returned slice has
// length == seqLen. The KV cache is extended by all seqLen positions.
func (q *Qwen35) ForwardPrefillArgmaxAll(ids []int, startPos int, cache *llm.KVCache) ([]int, error) {
	seqLen := len(ids)
	idData := make([]int64, seqLen)
	for i, id := range ids {
		idData[i] = int64(id)
	}
	idsArr, err := q.backend.NewArrayFromInt64(idData, []int{1, seqLen})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := q.weights.embed.Lookup(idsArr, q.backend, q.stream)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, q.stream)
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

	if err := q.setLastHidden(h, seqLen); err != nil {
		h.Free()
		return nil, err
	}

	// Logits at every position: [1, seqLen, vocab].
	logits, err := q.computeLogits(h)
	if err != nil {
		h.Free()
		return nil, err
	}
	defer logits.Free()
	if err := q.stream.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}

	idxArr, err := q.backend.ArgMaxAxis(logits, 2, false, q.stream)
	if err != nil {
		return nil, fmt.Errorf("argmax axis: %w", err)
	}
	defer idxArr.Free()
	data, err := idxArr.Uint32Data()
	if err != nil {
		return nil, fmt.Errorf("read argmax: %w", err)
	}
	out := make([]int, seqLen)
	for i, v := range data {
		out[i] = int(v)
	}
	return out, nil
}

// ForwardDecodeMTP runs one MTP-assisted decode round at position pos.
// nextToken is the token at position pos (just emitted); the KV cache holds
// positions 0..pos-1. It drafts k tokens with the MTP head, verifies them in
// ONE main-model batched forward, and returns the tokens to emit: accepted
// drafts followed by the main model's own next prediction (never empty).
// After the round the cache holds positions 0..pos+len(out)-1 and the next
// round decodes out[len(out)-1] at position pos+len(out).
//
// Acceptance: the main model's argmax at position pos+i must equal draft i+1
// (DeepSeek-V3 self-speculative decoding). When fewer than k drafts are
// accepted, the cache (including fixed-size DeltaNet state) is rolled back
// to the pre-round snapshot and the accepted prefix is re-run through the
// main model, so rejected trailing K/V never pollute future attention.
func (q *Qwen35) ForwardDecodeMTP(nextToken int, pos int, cache *llm.KVCache, k int) ([]int, error) {
	if k <= 0 {
		return nil, fmt.Errorf("mtp: k must be > 0")
	}

	prevHidden := q.LastHidden()
	drafts, err := q.MTPDraft(prevHidden, nextToken, k)
	if err != nil {
		return nil, fmt.Errorf("mtp draft: %w", err)
	}

	snap := cache.SnapshotPrefix()

	ids := append([]int{nextToken}, drafts...)
	preds, err := q.ForwardPrefillArgmaxAll(ids, pos, cache)
	if err != nil {
		snap.Free()
		return nil, fmt.Errorf("mtp verify: %w", err)
	}

	accepted := 0
	for accepted < k && preds[accepted] == drafts[accepted] {
		accepted++
	}

	if accepted < k {
		// Rejected drafts left wrong K/V and DeltaNet state in the cache.
		// Restore the pre-verify snapshot and re-run only the accepted
		// prefix through the full model to rebuild correct cache state.
		if err := cache.RestorePrefix(snap); err != nil {
			snap.Free()
			return nil, fmt.Errorf("mtp rollback: %w", err)
		}
		snap.Free()
		prefix := append([]int{nextToken}, drafts[:accepted]...)
		idData := make([]int64, len(prefix))
		for i, id := range prefix {
			idData[i] = int64(id)
		}
		idsArr, err := q.backend.NewArrayFromInt64(idData, []int{1, len(prefix)})
		if err != nil {
			return nil, fmt.Errorf("mtp re-run ids: %w", err)
		}
		defer idsArr.Free()
		rerunLogits, err := q.prefillInternal(idsArr, len(prefix), pos, cache)
		if err != nil {
			return nil, fmt.Errorf("mtp re-run: %w", err)
		}
		rerunLogits.Free()
	} else {
		snap.Free()
	}

	out := make([]int, 0, accepted+1)
	out = append(out, drafts[:accepted]...)
	out = append(out, preds[accepted])
	return out, nil
}

func (q *Qwen35) computeLogits(h tensor.Array) (tensor.Array, error) {
	normed, err := q.rmsNormQwen35(h, q.weights.normWeight)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()
	return q.headOnlyLogits(normed)
}

// headOnlyLogits applies the vocabulary head to an already-normalized
// hidden state. Used by the main model (after its final RMSNorm) and by the
// MTP head (after its own mtp.norm — the main model's final norm must NOT
// be applied again).
func (q *Qwen35) headOnlyLogits(normed tensor.Array) (tensor.Array, error) {
	if q.weights.lmHead != nil {
		// Untied embeddings: separate lm_head projection.
		return q.weights.lmHead.Forward(normed, q.backend, q.stream)
	}
	return q.weights.embed.Logits(normed, q.backend, q.stream)
}

// makeOneTokenArray builds a [1, 1] int64 array for a single token id. The
// caller frees the result.
func (q *Qwen35) makeOneTokenArray(tokenID int) tensor.Array {
	arr, _ := q.backend.NewArrayFromInt64([]int64{int64(tokenID)}, []int{1, 1})
	return arr
}

// rmsNormQwen35 applies the Qwen3.5-weighted RMSNorm used by every
// rmsNormQwen35 applies RMSNorm with weight scaling. The formula depends on
// the model source:
//
//   - raw HF exports: rms_norm(x) * (1 + weight) — transformers stores the
//     norm weight as a residual offset, and the runtime adds 1.
//   - mlx-community exports: rms_norm(x) * weight — mlx-lm's sanitize() pre-
//     adds the 1 during conversion, so the weight is already the final
//     multiplier. Applying (1 + weight) here would double it.
//
// Detection: mlx-community uses prefix "language_model.model."; raw HF uses
// "model.language_model.".
func (q *Qwen35) rmsNormQwen35(x, weight tensor.Array) (tensor.Array, error) {
	s := q.stream
	normed, err := llm.RMSNorm(x, nil, q.cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("rms norm: %w", err)
	}
	defer normed.Free()

	if q.normPreAdded {
		// mlx-community: weight already includes the +1
		return q.backend.Multiply(normed, weight, s)
	}

	// raw HF: add 1 to weight
	ones, err := q.backend.NewArrayFromFloat32([]float32{1}, []int{1})
	if err != nil {
		return nil, fmt.Errorf("ones: %w", err)
	}
	defer ones.Free()
	scale, err := q.backend.Add(weight, ones, s)
	if err != nil {
		return nil, fmt.Errorf("1+w: %w", err)
	}
	defer scale.Free()

	return q.backend.Multiply(normed, scale, s)
}

func (q *Qwen35) computeLogitsLast(h tensor.Array, seqLen int) (tensor.Array, error) {
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

// forwardLayer dispatches per layer: DeltaNet linear attention for
// (layerIdx+1)%interval != 0, full attention otherwise. Both paths share the
// same pre/post RMSNorm and SwiGLU MLP.
func (q *Qwen35) forwardLayer(h tensor.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream
	lw := &q.weights.layers[layerIdx]

	normed, err := q.rmsNormQwen35(h, lw.inputNorm)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	var attnOut tensor.Array
	if lw.linearAttn != nil {
		attnOut, err = lw.linearAttn.forward(normed, cache, layerIdx, seqLen, q.backend, s)
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

	residual1, err := q.backend.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	normed2, err := q.rmsNormQwen35(residual1, lw.postNorm)
	if err != nil {
		return nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	// MLP: dispatch to MoE or dense SwiGLU
	var ffnOut tensor.Array
	if lw.moe != nil {
		ffnOut, err = lw.moe.forward(normed2, q)
	} else {
		ffnOut, err = q.swiglu(normed2, lw)
	}
	if err != nil {
		return nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	return q.backend.Add(residual1, ffnOut, s)
}

// fullAttention is Qwen3-style GQA with two qwen3.5 differences: the Q
// projection is 2× the head count (carrying the output gate in the second
// half) and RoPE rotates only partial_rotary_factor of the head dim.
func (q *Qwen35) fullAttention(h tensor.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
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
	qFull, err := sa.qProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer qFull.Free()

	qFull4, err := q.backend.Reshape(qFull, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape4: %w", err)
	}
	defer qFull4.Free()

	q2d, err := q.backend.Slice(qFull4, []int{0, 0, 0, 0}, []int{1, seqLen, cfg.NumHeads, headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("q slice: %w", err)
	}
	defer q2d.Free()

	gate4, err := q.backend.Slice(qFull4, []int{0, 0, 0, headDim}, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("q gate slice: %w", err)
	}
	defer gate4.Free()
	qGate, err := q.backend.Reshape(gate4, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q gate flatten: %w", err)
	}
	defer qGate.Free()

	k2d, err := sa.kProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := sa.vProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	qR, err := q.backend.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := q.backend.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}
	defer kR.Free()

	vR, err := q.backend.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}
	defer vR.Free()

	qNormed, err := q.rmsNormQwen35(qR, sa.qNorm)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR = qNormed

	kNormed, err := q.rmsNormQwen35(kR, sa.kNorm)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	defer kNormed.Free()
	kR = kNormed

	qT, err := q.backend.TransposeAxes(qR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("q transpose: %w", err)
	}
	defer qT.Free()

	kT, err := q.backend.TransposeAxes(kR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("k transpose: %w", err)
	}
	defer kT.Free()

	vT, err := q.backend.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("v transpose: %w", err)
	}
	defer vT.Free()

	// Partial RoPE: rotate only partialRotaryFactor of the head dim.
	ropeDims := int(float64(headDim) * float64(cfg.PartialRotaryFactor))
	qRot, err := llm.ApplyRoPEFast(qT, startPos, ropeDims, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := llm.ApplyRoPEFast(kT, startPos, ropeDims, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	var kForAttn, vForAttn tensor.Array
	if cache != nil && cache.IsInitialized(layerIdx) {
		cached, err := cache.Get(layerIdx)
		if err != nil {
			return nil, err
		}
		newK, err := q.backend.ConcatenateAxis([]tensor.Array{cached.K, kRot}, 2, s)
		if err != nil {
			return nil, fmt.Errorf("concat K: %w", err)
		}
		newV, err := q.backend.ConcatenateAxis([]tensor.Array{cached.V, vT}, 2, s)
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
		kRetained := q.backend.RetainArray(kRot)
		vRetained := q.backend.RetainArray(vT)
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
	kExp, err := llm.ExpandKVHeads(kForAttn, cfg.NumHeads, cfg.NumKVHeads, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("expand K heads: %w", err)
	}
	defer kExp.Free()

	vExp, err := llm.ExpandKVHeads(vForAttn, cfg.NumHeads, cfg.NumKVHeads, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("expand V heads: %w", err)
	}
	defer vExp.Free()

	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	scale := float32(1.0 / sqrt(float64(headDim)))
	ctx, err := q.backend.FastScaledDotProductAttention(qT, kExp, vExp, scale, maskMode, nil, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fused attention: %w", err)
	}
	defer ctx.Free()

	ctxT, err := q.backend.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := q.backend.Reshape(ctxT, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	// attn_output_gate: gate the attention output BEFORE o_proj (the gate is
	// num_heads*head_dim wide, matching the attention output, not hidden).
	gateSig, err := q.backend.Sigmoid(qGate, s)
	if err != nil {
		return nil, fmt.Errorf("gate sigmoid: %w", err)
	}
	defer gateSig.Free()

	gated, err := q.backend.Multiply(ctxFlat, gateSig, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return sa.oProj.Forward(gated, q.backend, s)
}

// swiglu is the SwiGLU MLP shared by both layer kinds.
func (q *Qwen35) swiglu(h tensor.Array, lw *layerWeights) (tensor.Array, error) {
	s := q.stream

	gate, err := lw.gateProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("gate proj: %w", err)
	}
	defer gate.Free()

	up, err := lw.upProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("up proj: %w", err)
	}
	defer up.Free()

	gateSilu, err := llm.SiLU(gate, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gateSilu.Free()

	gated, err := q.backend.Multiply(gateSilu, up, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return lw.downProj.Forward(gated, q.backend, s)
}
