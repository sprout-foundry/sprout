//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// forwardDecodeLayerShim runs a single decoder layer via the C extension,
// replacing ~40 individual CGO calls with 1. Only used for single-token
// decode (seqLen==1). Prefill and delta-prefill still use the Go path.
//
// The C function handles: input norm, attention (Q/K/V proj, RoPE, cache
// concat, SDPA, O proj), post-attn norm + residual, MLP (gate/up/GeGLU/down
// + norms + residual), per-layer gating, layer scalar.
//
// Returns the output hidden state. The KV cache is updated in-place (the
// cache's K/V arrays are freed and replaced with the concatenated result).
func (g *Gemma4) forwardDecodeLayerShim(
	h tensor.Array,
	layerIdx int,
	pos int,
	cache *llm.KVCache,
	perLayerInput tensor.Array,
	intermediateKVs []struct{ k, v tensor.Array },
) (tensor.Array, error) {
	lw := &g.weights.layers[layerIdx]
	isFull := isFullAttention(layerIdx, g.cfg.SlidingWindowPattern)
	hasKV := g.hasKV(layerIdx)
	headDim := g.cfg.HeadDim
	if isFull && g.cfg.GlobalHeadDim > 0 {
		headDim = g.cfg.GlobalHeadDim
	}

	mlxStream := g.stream.(*mlx.Stream)
	mlxH := h.(*mlx.Array)

	var propFreqs *mlx.Array
	if isFull {
		propFreqs = g.propRoPEFreqs.(*mlx.Array)
	} else {
		propFreqs = mlxH // dummy, won't be used when is_full=0
	}

	var perLayerC *mlx.Array
	if perLayerInput != nil {
		perLayerC = perLayerInput.(*mlx.Array)
	}

	cfg := mlx.Gemma4LayerConfig{
		NumHeads:       g.cfg.NumHeads,
		NumKVHeads:     g.cfg.NumKVHeads,
		HeadDim:        headDim,
		SeqLen:         1,
		StartPos:       pos,
		IsFull:         isFull,
		RMSEps:         g.cfg.RMSNormEPS,
		HasPerLayer:    lw.perLayerInputGate != nil && perLayerInput != nil,
		HasLayerScalar: lw.layerScalar != nil,
	}

	groupSize := lw.qProj.QGroupSize()
	bits := lw.qProj.QBits()

	// Convert weight tensors to *mlx.Array handles
	toArr := func(a tensor.Array) *mlx.Array {
		if a == nil {
			return nil
		}
		return a.(*mlx.Array)
	}
	toLin := func(l *llm.Linear) mlx.LinearHandles {
		if l == nil {
			return mlx.LinearHandles{}
		}
		return mlx.LinearHandles{
			W:      toArr(l.QW()),
			Scales: toArr(l.QScales()),
			Biases: toArr(l.QBiases()),
		}
	}

	inputNorm := toArr(lw.inputNorm)
	postAttnNorm := toArr(lw.postAttnNorm)
	preFFNorm := toArr(lw.preFFNorm)
	postFFNorm := toArr(lw.postFFNorm)
	postPLINNorm := toArr(lw.postPerLayerInputNorm)
	layerScalar := toArr(lw.layerScalar)
	qNorm := toArr(lw.qNorm)
	kNorm := toArr(lw.kNorm)

	if hasKV {
		// Full KV layer: pass cache K/V to C, get concatenated result back
		var kCache, vCache *mlx.Array
		if cache.IsInitialized(layerIdx) {
			cached, _ := cache.Get(layerIdx)
			kCache = cached.K.(*mlx.Array)
			vCache = cached.V.(*mlx.Array)
		}

		outH, newK, newV, err := mlx.Gemma4KVLayer(
			mlxH, perLayerC, kCache, vCache, propFreqs,
			cfg, groupSize, bits,
			inputNorm, postAttnNorm, preFFNorm, postFFNorm,
			toLin(lw.qProj), toLin(lw.kProj), toLin(lw.vProj), toLin(lw.oProj),
			qNorm, kNorm,
			toLin(lw.gateProj), toLin(lw.upProj), toLin(lw.downProj),
			toLin(lw.perLayerInputGate), toLin(lw.perLayerProjection),
			postPLINNorm, layerScalar,
			mlxStream,
		)
		if err != nil {
			return nil, fmt.Errorf("kv_layer_shim %d: %w", layerIdx, err)
		}

		// Update cache: the C function returned concatenated K/V arrays.
		// Store them in the cache (takes ownership).
		if newK != nil && newV != nil {
			cache.Store(layerIdx, newK, newV)
		} else {
			// First call with no cache: the C function used kRot/vT directly.
			// We need to retain them for the cache.
			// Actually, when kCache is nil, the C function returns nil for
			// newK/newV, meaning the attention used kRot/vT in-place.
			// We need to get those arrays. But the C function freed them in
			// CLEANUP. This is a problem for the first decode step.
			//
			// For decode, the cache is always initialized (prefill ran first).
			// This path only triggers on first decode after prefill, where
			// the cache already has data from prefill's Store() call.
			// So newK/newV should always be non-nil here.
			return nil, fmt.Errorf("kv_layer_shim %d: cache returned nil K/V", layerIdx)
		}

		// Store for KV sharing
		cached, _ := cache.Get(layerIdx)
		kCopy := g.backend.RetainArray(cached.K)
		vCopy := g.backend.RetainArray(cached.V)
		intermediateKVs[layerIdx] = struct{ k, v tensor.Array }{kCopy, vCopy}

		return outH, nil
	}

	// KV-shared layer: reuse K/V from previous layer
	prevIdx := g.prevKVs[layerIdx]
	if prevIdx >= len(intermediateKVs) || intermediateKVs[prevIdx].k == nil {
		return nil, fmt.Errorf("layer %d: no shared KV from layer %d", layerIdx, prevIdx)
	}
	kForAttn := intermediateKVs[prevIdx].k.(*mlx.Array)
	vForAttn := intermediateKVs[prevIdx].v.(*mlx.Array)

	outH, err := mlx.Gemma4SharedKVLayer(
		mlxH, kForAttn, vForAttn, propFreqs, perLayerC,
		cfg, groupSize, bits,
		inputNorm, postAttnNorm, preFFNorm, postFFNorm,
		toLin(lw.qProj), toLin(lw.oProj),
		qNorm,
		toLin(lw.gateProj), toLin(lw.upProj), toLin(lw.downProj),
		toLin(lw.perLayerInputGate), toLin(lw.perLayerProjection),
		postPLINNorm, layerScalar,
		mlxStream,
	)
	if err != nil {
		return nil, fmt.Errorf("shared_kv_layer_shim %d: %w", layerIdx, err)
	}

	return outH, nil
}
