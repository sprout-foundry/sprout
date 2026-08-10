//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func (g *Gemma4) ForwardPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	logits, err := g.forwardInternal(ids, seqLen, 0, cache)
	if err != nil { return nil, err }
	defer logits.Free()
	return logits.Float32Data()
}

func (g *Gemma4) ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := g.forwardInternal(ids, seqLen, startPos, cache)
	if err != nil { return nil, err }
	defer logits.Free()
	return logits.Float32Data()
}

func (g *Gemma4) forwardInternal(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := g.stream

	// Embed and scale
	h, err := g.weights.embed.Lookup(ids, g.backend, s)
	if err != nil { return nil, fmt.Errorf("embedding: %w", err) }
	defer h.Free()
	h, err = g.backend.SqueezeAxis(h, 2, s)
	if err != nil { return nil, fmt.Errorf("squeeze: %w", err) }
	// Apply embed scale
	scaleArr, err := g.backend.NewArrayFromFloat32([]float32{g.embedScale}, []int{1})
	if err != nil { return nil, err }
	h, err = g.backend.Multiply(h, scaleArr, s)
	scaleArr.Free()
	if err != nil { return nil, fmt.Errorf("embed scale: %w", err) }
	defer h.Free()

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
	}

	// Final norm + logits
	normed, err := g.gemmaRMSNorm(h, g.weights.norm)
	if err != nil { return nil, fmt.Errorf("final norm: %w", err) }
	defer normed.Free()
	return g.computeLogits(normed)
}

func (g *Gemma4) computePerLayerInputs(ids, h tensor.Array) (tensor.Array, error) {
	s := g.stream
	// embed_tokens_per_layer(ids) * scale
	pli, err := g.weights.embedPerLayer.Lookup(ids, g.backend, s)
	if err != nil { return nil, err }
	pli, err = g.backend.SqueezeAxis(pli, 2, s)
	if err != nil { pli.Free(); return nil, err }
	scaleArr, err := g.backend.NewArrayFromFloat32([]float32{g.embedPerLayerScale}, []int{1})
	if err != nil { pli.Free(); return nil, err }
	pli, err = g.backend.Multiply(pli, scaleArr, s)
	scaleArr.Free()
	if err != nil { return nil, err }
	// Reshape to [B, S, NumLayers, PerLayerDim]
	pli, err = g.backend.Reshape(pli, []int{1, pli.Shape()[1], g.cfg.NumLayers, g.cfg.HiddenSizePerLayerInput}, s)
	if err != nil { return nil, err }

	// project: per_layer_model_projection(h) * projScale -> norm
	proj, err := g.weights.perLayerProj.Forward(h, g.backend, s)
	if err != nil { return nil, err }
	defer proj.Free()
	projScale, err := g.backend.NewArrayFromFloat32([]float32{g.perLayerProjectionScale}, []int{1})
	if err != nil { return nil, err }
	defer projScale.Free()
	proj, err = g.backend.Multiply(proj, projScale, s)
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
	ilScale, err := g.backend.NewArrayFromFloat32([]float32{g.perLayerInputScale}, []int{1})
	if err != nil { return nil, err }
	defer ilScale.Free()
	return g.backend.Multiply(summed, ilScale, s)
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
		gate, err = geluApprox(gate, g.backend, s)
		if err != nil { return nil, nil, fmt.Errorf("per-layer gelu: %w", err) }
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

		// Cache update
		if cache != nil && cache.IsInitialized(layerIdx) {
			cached, err := cache.Get(layerIdx)
			if err != nil { return nil, nil, err }
			newK, err := g.backend.ConcatenateAxis([]tensor.Array{cached.K, kRot}, 2, s)
			if err != nil { return nil, nil, err }
			newV, err := g.backend.ConcatenateAxis([]tensor.Array{cached.V, vT2}, 2, s)
			if err != nil { newK.Free(); return nil, nil, err }
			cached.K.Free()
			cached.V.Free()
			cached.K = newK
			cached.V = newV
			kForAttn = newK
			vForAttn = newV
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

	// Expand KV heads
	kExp, err := llm.ExpandKVHeads(kForAttn, numHeads, numKVHeads, g.backend, s)
	if err != nil { return nil, nil, err }
	defer kExp.Free()
	vExp, err := llm.ExpandKVHeads(vForAttn, numHeads, numKVHeads, g.backend, s)
	if err != nil { return nil, nil, err }
	defer vExp.Free()

	// Attention — scale=1.0 for Gemma4
	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	ctx, err := g.backend.FastScaledDotProductAttention(qRot, kExp, vExp, 1.0, maskMode, nil, nil, s)
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
		// Full attention: proportional RoPE with partial_rotary_factor=0.25, rope_theta=1000000
		rotatedDims := int(float64(headDim) * 0.25)
		return llm.ApplyRoPEFast(x, offset, rotatedDims, 1000000.0, g.backend, s)
	}
	// Sliding attention: standard RoPE with full rotation, rope_theta=10000
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
	// GeGLU: gelu_approx(gate) * up
	gateAct, err := geluApprox(gate, g.backend, s)
	if err != nil { return nil, err }
	defer gateAct.Free()
	mul, err := g.backend.Multiply(gateAct, up, s)
	if err != nil { return nil, err }
	defer mul.Free()
	return lw.downProj.Forward(mul, g.backend, s)
}

func (g *Gemma4) computeLogits(h tensor.Array) (tensor.Array, error) {
	s := g.stream
	logits, err := g.weights.embed.Logits(h, g.backend, s)
	if err != nil { return nil, fmt.Errorf("logits: %w", err) }
	if g.cfg.FinalLogitSoftcap > 0 {
		softcap := float32(g.cfg.FinalLogitSoftcap)
		invSoftcap, err := s2(1.0/softcap, g.backend)
		if err != nil { logits.Free(); return nil, err }
		defer invSoftcap.Free()
		scaled, err := g.backend.Multiply(logits, invSoftcap, s)
		if err != nil { logits.Free(); return nil, err }
		logits.Free()
		tanh, err := g.backend.Tanh(scaled, s)
		scaled.Free()
		if err != nil { return nil, err }
		softcapArr, err := s2(softcap, g.backend)
		if err != nil { tanh.Free(); return nil, err }
		defer softcapArr.Free()
		out, err := g.backend.Multiply(tanh, softcapArr, s)
		tanh.Free()
		if err != nil { return nil, err }
		return out, nil
	}
	return logits, nil
}

func s2(v float32, b tensor.Backend) (tensor.Array, error) {
	return b.NewArrayFromFloat32([]float32{v}, []int{1})
}

// Decode methods

func (g *Gemma4) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
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

func (g *Gemma4) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (tensor.Array, error) {
	s := g.stream
	idsArr, err := g.backend.NewArrayFromInt64([]int64{int64(tokenID)}, []int{1, 1})
	if err != nil { return nil, err }
	defer idsArr.Free()

	h, err := g.weights.embed.Lookup(idsArr, g.backend, s)
	if err != nil { return nil, err }
	defer h.Free()
	h, err = g.backend.SqueezeAxis(h, 2, s)
	if err != nil { return nil, err }
	scaleArr, _ := g.backend.NewArrayFromFloat32([]float32{g.embedScale}, []int{1})
	defer scaleArr.Free()
	h, err = g.backend.Multiply(h, scaleArr, s)
	if err != nil { return nil, err }
	defer h.Free()

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
	}

	normed, err := g.gemmaRMSNorm(h, g.weights.norm)
	if err != nil { return nil, err }
	defer normed.Free()
	return g.computeLogits(normed)
}
