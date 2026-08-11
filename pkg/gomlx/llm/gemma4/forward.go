//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func (g *Gemma4) ForwardPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	logits, err := g.forwardInternal(ids, seqLen, 0, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	// Slice to last position: [1, seqLen, vocab] -> [1, 1, vocab]
	last, err := g.backend.Slice(logits, []int{0, seqLen - 1, 0}, []int{1, seqLen, g.cfg.VocabSize}, []int{1, 1, 1}, g.stream)
	if err != nil { return nil, fmt.Errorf("slice last logits: %w", err) }
	defer last.Free()
	return last.Float32Data()
}

func (g *Gemma4) ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := g.forwardInternal(ids, seqLen, startPos, cache)
	if err != nil { return nil, err }
	defer logits.Free()
	last, err := g.backend.Slice(logits, []int{0, seqLen - 1, 0}, []int{1, seqLen, g.cfg.VocabSize}, []int{1, 1, 1}, g.stream)
	if err != nil { return nil, fmt.Errorf("slice last logits: %w", err) }
	defer last.Free()
	return last.Float32Data()
}

func (g *Gemma4) forwardInternal(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := g.stream

	// Embed and scale
	h, err := g.weights.embed.Lookup(ids, g.backend, s)
	if err != nil { return nil, fmt.Errorf("embedding: %w", err) }
	defer h.Free()
	h, err = g.backend.SqueezeAxis(h, 2, s)
	if err != nil { return nil, fmt.Errorf("squeeze: %w", err) }
	// Apply embed scale (uses pre-allocated array to avoid per-call CGO overhead)
	h, err = g.backend.Multiply(h, g.scaleEmbedArr, s)
	if err != nil { return nil, fmt.Errorf("embed scale: %w", err) }
	defer h.Free()

	// Debug: dump embedding for comparison with mlx-lm
	if os.Getenv("GEMMA4_DEBUG") != "" {
		dumpArrayF32(h, "embed_scaled", g.backend, s)
	}

	// Per-layer input embeddings
	var perLayerInputs []tensor.Array
	if g.cfg.HiddenSizePerLayerInput > 0 {
		pli, err := g.computePerLayerInputs(ids, h)
		if err != nil { return nil, fmt.Errorf("per-layer inputs: %w", err) }
		// Split into per-layer slices
		for i := 0; i < g.cfg.NumLayers; i++ {
			// pli is [B, S, NumLayers, HiddenSizePerLayerInput]
			sliced, err := g.backend.Slice(pli, []int{0, 0, i, 0}, []int{1, seqLen, i+1, g.cfg.HiddenSizePerLayerInput}, []int{1, 1, 1, 1}, s)
			if err != nil { return nil, fmt.Errorf("slice per-layer %d: %w", i, err) }
			squeezed, err := g.backend.SqueezeAxis(sliced, 2, s)
			if err != nil { sliced.Free(); return nil, fmt.Errorf("squeeze per-layer %d: %w", i, err) }
			perLayerInputs = append(perLayerInputs, squeezed)
		}
		pli.Free()
		defer func() {
			for _, a := range perLayerInputs { a.Free() }
		}()
	}

	// Run decoder layers
	// Track KV for sharing
	intermediateKVs := make([]struct{ k, v tensor.Array }, g.cfg.NumLayers)
	defer func() {
		for _, kv := range intermediateKVs {
			if kv.k != nil { kv.k.Free() }
			if kv.k != nil { kv.v.Free() }
		}
	}()

	for i := 0; i < g.cfg.NumLayers; i++ {
		out, _, err := g.forwardLayer(h, i, seqLen, startPos, cache, perLayerInputs, intermediateKVs)
		if err != nil { return nil, fmt.Errorf("layer %d: %w", i, err) }
		h.Free()
		h = out
		if os.Getenv("GEMMA4_DEBUG") != "" && (i == 0 || i == 4 || i == 14) {
			dumpArrayF32(h, fmt.Sprintf("layer_%d_out", i), g.backend, s)
		}
	}

	// Final norm + logits
	normed, err := g.gemmaRMSNorm(h, g.weights.norm)
	if err != nil { return nil, fmt.Errorf("final norm: %w", err) }
	defer normed.Free()
	logits, err := g.computeLogits(normed)
	if err != nil { return nil, err }
	if os.Getenv("GEMMA4_DEBUG") != "" {
		lastPos := seqLen - 1
		sliced, _ := g.backend.Slice(logits, []int{0, lastPos, 0}, []int{1, lastPos + 1, g.cfg.VocabSize}, []int{1, 1, 1}, s)
		argmax, _ := g.backend.ArgMaxAxis(sliced, 2, false, s)
		sliced.Free()
		argmaxData, _ := argmax.Uint32Data()
		fmt.Fprintf(os.Stderr, "[gemma4] prefill argmax[%d]: %d (expected 236778)\n", lastPos, argmaxData[0])
		argmax.Free()
	}
	return logits, nil
}

func (g *Gemma4) computePerLayerInputs(ids, h tensor.Array) (tensor.Array, error) {
	s := g.stream
	// embed_tokens_per_layer(ids) * scale
	pli, err := g.weights.embedPerLayer.Lookup(ids, g.backend, s)
	if err != nil { return nil, err }
	pli, err = g.backend.SqueezeAxis(pli, 2, s)
	if err != nil { pli.Free(); return nil, err }
	pli, err = g.backend.Multiply(pli, g.scaleEmbedPerLayerArr, s)
	if err != nil { return nil, err }
	// Reshape to [B, S, NumLayers, PerLayerDim]
	pli, err = g.backend.Reshape(pli, []int{1, pli.Shape()[1], g.cfg.NumLayers, g.cfg.HiddenSizePerLayerInput}, s)
	if err != nil { return nil, err }

	// project: per_layer_model_projection(h) * projScale -> norm
	proj, err := g.weights.perLayerProj.Forward(h, g.backend, s)
	if err != nil { return nil, err }
	defer proj.Free()
	proj, err = g.backend.Multiply(proj, g.scalePerLayerProjectionArr, s)
	if err != nil { return nil, err }
	proj, err = g.backend.Reshape(proj, []int{1, h.Shape()[1], g.cfg.NumLayers, g.cfg.HiddenSizePerLayerInput}, s)
	if err != nil { return nil, err }
	proj, err = llm.RMSNorm(proj, g.weights.perLayerProjNorm, g.cfg.RMSNormEPS, g.backend, s)
	if err != nil { return nil, err }

	// (proj + pli) * perLayerInputScale
	summed, err := g.backend.Add(proj, pli, s)
	if err != nil { return nil, err }
	pli.Free()
	defer summed.Free()
	return g.backend.Multiply(summed, g.scalePerLayerInputArr, s)
}

func (g *Gemma4) forwardLayer(h tensor.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache, perLayerInputs []tensor.Array, intermediateKVs []struct{ k, v tensor.Array }) (tensor.Array, tensor.Array, error) {
	s := g.stream
	lw := &g.weights.layers[layerIdx]
	isFull := isFullAttention(layerIdx, g.cfg.SlidingWindowPattern)
	hasKV := g.hasKV(layerIdx)
	headDim := g.cfg.HeadDim
	if isFull && g.cfg.GlobalHeadDim > 0 {
		headDim = g.cfg.GlobalHeadDim
	}

	// Input norm
	normed, err := g.gemmaRMSNorm(h, lw.inputNorm)
	if err != nil { return nil, nil, fmt.Errorf("input norm: %w", err) }
	defer normed.Free()

	// Attention
	attnOut, _, err := g.attention(normed, lw, layerIdx, isFull, hasKV, headDim, seqLen, startPos, cache, intermediateKVs)
	if err != nil { return nil, nil, fmt.Errorf("attention: %w", err) }
	defer attnOut.Free()

	// Post-attention norm + residual
	attnNormed, err := g.gemmaRMSNorm(attnOut, lw.postAttnNorm)
	if err != nil { return nil, nil, fmt.Errorf("post-attn norm: %w", err) }
	defer attnNormed.Free()
	residual, err := g.backend.Add(h, attnNormed, s)
	if err != nil { return nil, nil, fmt.Errorf("attn residual: %w", err) }
	defer residual.Free()

	// MLP
	ffNormed, err := g.gemmaRMSNorm(residual, lw.preFFNorm)
	if err != nil { return nil, nil, fmt.Errorf("pre-ff norm: %w", err) }
	defer ffNormed.Free()
	ffOut, err := g.mlp(ffNormed, lw)
	if err != nil { return nil, nil, fmt.Errorf("mlp: %w", err) }
	defer ffOut.Free()
	ffNormed2, err := g.gemmaRMSNorm(ffOut, lw.postFFNorm)
	if err != nil { return nil, nil, fmt.Errorf("post-ff norm: %w", err) }
	defer ffNormed2.Free()
	h2, err := g.backend.Add(residual, ffNormed2, s)
	if err != nil { return nil, nil, fmt.Errorf("ff residual: %w", err) }
	defer h2.Free()

	// Per-layer input gating
	if lw.perLayerInputGate != nil && len(perLayerInputs) > layerIdx {
		perLayerInput := perLayerInputs[layerIdx]
		gate, err := lw.perLayerInputGate.Forward(h2, g.backend, s)
		if err != nil { return nil, nil, fmt.Errorf("per-layer gate: %w", err) }
		defer gate.Free()
		// GELU via C shim (1 CGO call instead of ~15)
		mlxGate := gate.(*mlx.Array)
		mlxStream := s.(*mlx.Stream)
		if geluOut, err := mlx.Gemma4GELU(mlxGate, mlxStream); err == nil {
			gate = geluOut
		} else {
			gate, err = geluApprox(gate, g.backend, s)
			if err != nil { return nil, nil, fmt.Errorf("per-layer gelu: %w", err) }
		}
		defer gate.Free()
		gate, err = g.backend.Multiply(gate, perLayerInput, s)
		if err != nil { return nil, nil, fmt.Errorf("per-layer mul: %w", err) }
		defer gate.Free()
		gate, err = lw.perLayerProjection.Forward(gate, g.backend, s)
		if err != nil { return nil, nil, fmt.Errorf("per-layer proj: %w", err) }
		defer gate.Free()
		gate, err = g.gemmaRMSNorm(gate, lw.postPerLayerInputNorm)
		if err != nil { return nil, nil, fmt.Errorf("per-layer norm: %w", err) }
		defer gate.Free()
		h2, err = g.backend.Add(h2, gate, s)
		if err != nil { return nil, nil, fmt.Errorf("per-layer residual: %w", err) }
		// h2 is already deferred; need to update the reference
	}

	// Layer scalar
	if lw.layerScalar != nil {
		h2, err = g.backend.Multiply(h2, lw.layerScalar, s)
		if err != nil { return nil, nil, fmt.Errorf("layer scalar: %w", err) }
	}

	out := h2
	g.backend.RetainArray(out)
	return out, nil, nil
}

func (g *Gemma4) attention(h tensor.Array, lw *layerWeights, layerIdx int, isFull, hasKV bool, headDim, seqLen, startPos int, cache *llm.KVCache, intermediateKVs []struct{ k, v tensor.Array }) (tensor.Array, tensor.Array, error) {
	s := g.stream
	cfg := g.cfg
	numHeads := cfg.NumHeads
	numKVHeads := cfg.NumKVHeads
	if cfg.AttentionKEqV && !isFull {
		numKVHeads = cfg.NumKVHeads // normal
	}

	// Q projection
	q, err := lw.qProj.Forward(h, g.backend, s)
	if err != nil { return nil, nil, err }
	defer q.Free()
	qR, err := g.backend.Reshape(q, []int{1, seqLen, numHeads, headDim}, s)
	if err != nil { return nil, nil, err }
	defer qR.Free()
	// Q norm
	qNormed, err := llm.RMSNorm(qR, lw.qNorm, cfg.RMSNormEPS, g.backend, s)
	if err != nil { return nil, nil, err }
	defer qNormed.Free()

	var kForAttn, vForAttn tensor.Array
	// KV sharing tracked via intermediateKVs slice

	if hasKV {
		// Compute K, V
		k2d, err := lw.kProj.Forward(h, g.backend, s)
		if err != nil { return nil, nil, err }
		defer k2d.Free()
		v2d, err := lw.vProj.Forward(h, g.backend, s)
		if err != nil { return nil, nil, err }
		defer v2d.Free()

		kR, err := g.backend.Reshape(k2d, []int{1, seqLen, numKVHeads, headDim}, s)
		if err != nil { return nil, nil, err }
		vR, err := g.backend.Reshape(v2d, []int{1, seqLen, numKVHeads, headDim}, s)
		if err != nil { return nil, nil, err }

		// K norm (with weight), V norm (no scale)
		kNormed, err := llm.RMSNorm(kR, lw.kNorm, cfg.RMSNormEPS, g.backend, s)
		if err != nil { vR.Free(); return nil, nil, err }
		kR.Free()
		defer kNormed.Free()

		vNormed, err := rmsNormNoScale(vR, cfg.RMSNormEPS, g.backend, s)
		if err != nil { return nil, nil, err }
		vR.Free()
		defer vNormed.Free()

		// Transpose to [B, H, S, D]
		kT, err := g.backend.TransposeAxes(kNormed, []int{0, 2, 1, 3}, s)
		if err != nil { return nil, nil, err }
		defer kT.Free()
		vT, err := g.backend.TransposeAxes(vNormed, []int{0, 2, 1, 3}, s)
		if err != nil { return nil, nil, err }
		defer vT.Free()

		// RoPE
		kRot, err := g.applyRoPE(kT, isFull, startPos, headDim)
		if err != nil { return nil, nil, err }
		defer kRot.Free()
		vT2 := vT
		g.backend.RetainArray(vT2)

		// Cache update:
		// - Initialized + any seqLen: Append (concat) — handles both
		//   single-token decode and multi-token delta prefill
		// - Not initialized: Store (first prefill)
		if cache != nil && cache.IsInitialized(layerIdx) {
			if err := cache.Append(layerIdx, kRot, vT2); err != nil {
				return nil, nil, fmt.Errorf("cache append: %w", err)
			}
			cached, _ := cache.Get(layerIdx)
			kForAttn = cached.K
			vForAttn = cached.V
		} else if cache != nil {
			kForAttn = kRot
			vForAttn = vT2
			kRetained := g.backend.RetainArray(kRot)
			vRetained := g.backend.RetainArray(vT2)
			cache.Store(layerIdx, kRetained, vRetained)
		} else {
			kForAttn = kRot
			vForAttn = vT2
		}

		// Store for sharing
		kCopy := g.backend.RetainArray(kForAttn)
		vCopy := g.backend.RetainArray(vForAttn)
		intermediateKVs[layerIdx] = struct{ k, v tensor.Array }{kCopy, vCopy}
	} else {
		// KV-shared layer: reuse from previous layer
		prevIdx := g.prevKVs[layerIdx]
		if prevIdx >= len(intermediateKVs) || intermediateKVs[prevIdx].k == nil {
			return nil, nil, fmt.Errorf("layer %d: no shared KV from layer %d", layerIdx, prevIdx)
		}
		kForAttn = intermediateKVs[prevIdx].k
		vForAttn = intermediateKVs[prevIdx].v
	}

	// Q transpose + RoPE
	qT, err := g.backend.TransposeAxes(qNormed, []int{0, 2, 1, 3}, s)
	if err != nil { return nil, nil, err }
	defer qT.Free()
	qRot, err := g.applyRoPE(qT, isFull, startPos, headDim)
	if err != nil { return nil, nil, err }
	defer qRot.Free()

	// Attention — scale=1.0 for Gemma4
	// No ExpandKVHeads needed: MLX's SDPA natively handles GQA (numKVHeads < numHeads).
	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	ctx, err := g.backend.FastScaledDotProductAttention(qRot, kForAttn, vForAttn, 1.0, maskMode, nil, nil, s)
	if err != nil { return nil, nil, err }
	defer ctx.Free()

	// Output projection
	ctxT, err := g.backend.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil { return nil, nil, err }
	defer ctxT.Free()
	outDim := numHeads * headDim
	ctxFlat, err := g.backend.Reshape(ctxT, []int{1, seqLen, outDim}, s)
	if err != nil { return nil, nil, err }
	defer ctxFlat.Free()
	oOut, err := lw.oProj.Forward(ctxFlat, g.backend, s)
	if err != nil { return nil, nil, err }
	return oOut, nil, nil
}

func (g *Gemma4) applyRoPE(x tensor.Array, isFull bool, offset, headDim int) (tensor.Array, error) {
	s := g.stream
	if isFull {
		return g.backend.FastRoPE(x, headDim, false, 0, 1.0, offset, g.propRoPEFreqs, s)
	}
	return llm.ApplyRoPEFast(x, offset, headDim, 10000.0, g.backend, s)
}

func (g *Gemma4) mlp(h tensor.Array, lw *layerWeights) (tensor.Array, error) {
	s := g.stream
	gate, err := lw.gateProj.Forward(h, g.backend, s)
	if err != nil { return nil, err }
	defer gate.Free()
	up, err := lw.upProj.Forward(h, g.backend, s)
	if err != nil { return nil, err }
	defer up.Free()
	// GeGLU via C shim (1 CGO call instead of ~15)
	mlxGate := gate.(*mlx.Array)
	mlxUp := up.(*mlx.Array)
	mlxStream := s.(*mlx.Stream)
	gegluOut, err := mlx.Gemma4GeGLU(mlxGate, mlxUp, mlxStream)
	if err != nil {
		// Fall back to eager path
		gateAct, err := geluApprox(gate, g.backend, s)
		if err != nil { return nil, err }
		defer gateAct.Free()
		mul, err := g.backend.Multiply(gateAct, up, s)
		if err != nil { return nil, err }
		defer mul.Free()
		return lw.downProj.Forward(mul, g.backend, s)
	}
	defer gegluOut.Free()
	return lw.downProj.Forward(gegluOut, g.backend, s)
}

func (g *Gemma4) computeLogits(h tensor.Array) (tensor.Array, error) {
	s := g.stream
	logits, err := g.weights.embed.Logits(h, g.backend, s)
	if err != nil { return nil, fmt.Errorf("logits: %w", err) }
	if g.cfg.FinalLogitSoftcap > 0 {
		scaled, err := g.backend.Multiply(logits, g.scaleInvSoftcap, s)
		if err != nil { logits.Free(); return nil, err }
		logits.Free()
		tanh, err := g.backend.Tanh(scaled, s)
		scaled.Free()
		if err != nil { return nil, err }
		out, err := g.backend.Multiply(tanh, g.scaleSoftcap, s)
		tanh.Free()
		if err != nil { return nil, err }
		return out, nil
	}
	return logits, nil
}

// Decode methods

func (g *Gemma4) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
	if os.Getenv("GEMMA4_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[gemma4] ForwardDecode tokenID=%d pos=%d\n", tokenID, pos)
	}
	logits, err := g.decodeInternal(tokenID, pos, cache)
	if err != nil { return nil, err }
	defer logits.Free()
	return logits.Float32Data()
}

func (g *Gemma4) ForwardDecodeArgmax(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	logits, err := g.decodeInternal(tokenID, pos, cache)
	if err != nil { return 0, err }
	defer logits.Free()
	idxArr, err := g.backend.ArgMax(logits, false, g.stream)
	if err != nil { return 0, err }
	defer idxArr.Free()
	data, err := idxArr.Uint32Data()
	if err != nil { return 0, err }
	if len(data) == 0 { return 0, fmt.Errorf("argmax empty") }
	return int(data[0]), nil
}

// ForwardPrefillArgmaxAll runs the model over a short sequence of token IDs
// and returns the argmax token at EVERY position. Used for prompt-lookup
// speculative decoding to batch-verify candidate tokens in one forward pass.
func (g *Gemma4) ForwardPrefillArgmaxAll(ids []int, startPos int, cache *llm.KVCache) ([]int, error) {
	seqLen := len(ids)
	idData := make([]int64, seqLen)
	for i, id := range ids {
		idData[i] = int64(id)
	}
	idsArr, err := g.backend.NewArrayFromInt64(idData, []int{1, seqLen})
	if err != nil { return nil, fmt.Errorf("create ids: %w", err) }
	defer idsArr.Free()

	logits, err := g.forwardInternal(idsArr, seqLen, startPos, cache)
	if err != nil { return nil, err }
	defer logits.Free()
	if err := g.stream.Synchronize(); err != nil { return nil, fmt.Errorf("synchronize: %w", err) }

	idxArr, err := g.backend.ArgMaxAxis(logits, 2, false, g.stream)
	if err != nil { return nil, fmt.Errorf("argmax axis: %w", err) }
	defer idxArr.Free()
	data, err := idxArr.Uint32Data()
	if err != nil { return nil, fmt.Errorf("read argmax: %w", err) }
	if len(data) != seqLen {
		return nil, fmt.Errorf("argmax length mismatch: got %d, want %d", len(data), seqLen)
	}
	result := make([]int, seqLen)
	for i, v := range data {
		result[i] = int(v)
	}
	return result, nil
}

func (g *Gemma4) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (tensor.Array, error) {
	s := g.stream
	idsArr, err := g.backend.NewArrayFromInt64([]int64{int64(tokenID)}, []int{1, 1})
	if err != nil { return nil, err }
	defer idsArr.Free()

	h, err := g.weights.embed.Lookup(idsArr, g.backend, s)
	if err != nil { return nil, err }
	if os.Getenv("GEMMA4_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[gemma4] decode_embed_raw shape=%v ids=%v\n", h.Shape(), []int64{int64(tokenID)})
		dumpArrayF32(h, "decode_embed_raw_pre_scale", g.backend, s)
	}
	defer h.Free()
	h, err = g.backend.SqueezeAxis(h, 2, s)
	if err != nil { return nil, err }
	h, err = g.backend.Multiply(h, g.scaleEmbedArr, s)
	if err != nil { return nil, err }
	defer h.Free()

	if os.Getenv("GEMMA4_DEBUG") != "" {
		dumpArrayF32(h, "decode_embed", g.backend, s)
	}

	// Per-layer inputs for decode
	var perLayerInputs []tensor.Array
	if g.cfg.HiddenSizePerLayerInput > 0 {
		pli, err := g.computePerLayerInputs(idsArr, h)
		if err != nil { return nil, err }
		defer pli.Free()
		for i := 0; i < g.cfg.NumLayers; i++ {
			sliced, err := g.backend.Slice(pli, []int{0, 0, i, 0}, []int{1, 1, i+1, g.cfg.HiddenSizePerLayerInput}, []int{1, 1, 1, 1}, s)
			if err != nil { return nil, err }
			squeezed, err := g.backend.SqueezeAxis(sliced, 2, s)
			if err != nil { sliced.Free(); return nil, err }
			perLayerInputs = append(perLayerInputs, squeezed)
		}
		defer func() { for _, a := range perLayerInputs { a.Free() } }()
	}

	intermediateKVs := make([]struct{ k, v tensor.Array }, g.cfg.NumLayers)
	defer func() {
		for _, kv := range intermediateKVs {
			if kv.k != nil { kv.k.Free() }
			if kv.v != nil { kv.v.Free() }
		}
	}()

	for i := 0; i < g.cfg.NumLayers; i++ {
		out, _, err := g.forwardLayer(h, i, 1, pos, cache, perLayerInputs, intermediateKVs)
		if err != nil { return nil, fmt.Errorf("layer %d: %w", i, err) }
		h.Free()
		h = out
		if os.Getenv("GEMMA4_DEBUG") != "" && i < 6 {
			dumpArrayF32(h, fmt.Sprintf("decode_layer_%d", i), g.backend, s)
			// Check for NaN
			if data, err := h.Float32Data(); err == nil {
				nanCount := 0
				for _, v := range data { if v != v { nanCount++ } }
				if nanCount > 0 {
					fmt.Fprintf(os.Stderr, "[gemma4] decode_layer_%d: %d NaN values!\n", i, nanCount)
				}
			}
		}
	}

	normed, err := g.gemmaRMSNorm(h, g.weights.norm)
	if err != nil { return nil, err }
	defer normed.Free()
	return g.computeLogits(normed)
}
