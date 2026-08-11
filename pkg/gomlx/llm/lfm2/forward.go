//go:build darwin && arm64 && cgo && mlx

package lfm2

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// ForwardPrefill runs the forward pass over a full sequence of tokens.
func (l *LFM2) ForwardPrefill(ids tensor.Array, seqLen int, cache *llm.KVCache) ([]float32, error) {
	logits, err := l.forwardInternal(ids, seqLen, 0, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	last, err := l.backend.Slice(logits, []int{0, seqLen - 1, 0}, []int{1, seqLen, l.cfg.VocabSize}, []int{1, 1, 1}, l.stream)
	if err != nil {
		return nil, fmt.Errorf("slice last logits: %w", err)
	}
	defer last.Free()
	return last.Float32Data()
}

func (l *LFM2) ForwardPrefillFrom(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := l.forwardInternal(ids, seqLen, startPos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	last, err := l.backend.Slice(logits, []int{0, seqLen - 1, 0}, []int{1, seqLen, l.cfg.VocabSize}, []int{1, 1, 1}, l.stream)
	if err != nil {
		return nil, fmt.Errorf("slice last logits: %w", err)
	}
	defer last.Free()
	return last.Float32Data()
}

func (l *LFM2) ForwardDecode(tokenID int, pos int, cache *llm.KVCache) ([]float32, error) {
	logits, err := l.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return nil, err
	}
	defer logits.Free()
	return logits.Float32Data()
}

func (l *LFM2) ForwardDecodeArgmax(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	logits, err := l.decodeInternal(tokenID, pos, cache)
	if err != nil {
		return 0, err
	}
	defer logits.Free()
	idxArr, err := l.backend.ArgMax(logits, false, l.stream)
	if err != nil {
		return 0, err
	}
	defer idxArr.Free()
	data, err := idxArr.Uint32Data()
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("argmax empty")
	}
	return int(data[0]), nil
}

// forwardInternal runs the model over a sequence (prefill or delta-prefill).
func (l *LFM2) forwardInternal(ids tensor.Array, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := l.stream

	h, err := l.weights.embed.Lookup(ids, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	defer h.Free()
	h, err = l.backend.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze: %w", err)
	}
	defer h.Free()

	for i := 0; i < l.cfg.NumLayers; i++ {
		out, err := l.forwardLayer(h, i, seqLen, startPos, cache)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	normed, err := l.rmsNorm(h, l.weights.embeddingNorm)
	if err != nil {
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()
	return l.weights.embed.Logits(normed, l.backend, s)
}

// decodeInternal runs the model for a single token.
func (l *LFM2) decodeInternal(tokenID int, pos int, cache *llm.KVCache) (tensor.Array, error) {
	s := l.stream

	idsArr, err := l.backend.NewArrayFromInt64([]int64{int64(tokenID)}, []int{1, 1})
	if err != nil {
		return nil, err
	}
	defer idsArr.Free()

	h, err := l.weights.embed.Lookup(idsArr, l.backend, s)
	if err != nil {
		return nil, err
	}
	defer h.Free()
	h, err = l.backend.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, err
	}
	defer h.Free()

	for i := 0; i < l.cfg.NumLayers; i++ {
		out, err := l.forwardLayer(h, i, 1, pos, cache)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	normed, err := l.rmsNorm(h, l.weights.embeddingNorm)
	if err != nil {
		return nil, err
	}
	defer normed.Free()
	return l.weights.embed.Logits(normed, l.backend, s)
}

// forwardLayer runs a single decoder layer (conv or attention + FFN).
func (l *LFM2) forwardLayer(h tensor.Array, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := l.stream
	lw := &l.weights.layers[layerIdx]

	// Pre-norm
	normed, err := l.rmsNorm(h, lw.operatorNorm)
	if err != nil {
		return nil, fmt.Errorf("operator norm: %w", err)
	}
	defer normed.Free()

	var attnOut tensor.Array
	if l.isAttnLayer[layerIdx] {
		attnOut, err = l.attention(normed, lw, layerIdx, seqLen, startPos, cache)
	} else {
		attnOut, err = l.shortConv(normed, lw, layerIdx, seqLen, startPos, cache)
	}
	if err != nil {
		return nil, fmt.Errorf("attention/conv: %w", err)
	}
	defer attnOut.Free()

	// Residual: h + attnOut
	residual, err := l.backend.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("residual: %w", err)
	}
	defer residual.Free()

	// FFN
	ffNormed, err := l.rmsNorm(residual, lw.ffnNorm)
	if err != nil {
		return nil, fmt.Errorf("ffn norm: %w", err)
	}
	defer ffNormed.Free()
	ffOut, err := l.mlp(ffNormed, lw)
	if err != nil {
		return nil, fmt.Errorf("mlp: %w", err)
	}
	defer ffOut.Free()

	return l.backend.Add(residual, ffOut, s)
}

// attention runs the GQA full-attention layer.
func (l *LFM2) attention(h tensor.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := l.stream
	cfg := l.cfg

	q2d, err := lw.qProj.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer q2d.Free()

	k2d, err := lw.kProj.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := lw.vProj.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	qR, err := l.backend.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, l.headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := l.backend.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, l.headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}

	vR, err := l.backend.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, l.headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}

	// Q/K per-head RMSNorm
	qNormed, err := llm.RMSNorm(qR, lw.qNorm, cfg.RMSNormEPS, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR.Free()
	qR = qNormed

	kNormed, err := llm.RMSNorm(kR, lw.kNorm, cfg.RMSNormEPS, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	kR.Free()
	kR = kNormed

	// Transpose to [B, heads, seq, head_dim]
	qT, err := l.backend.TransposeAxes(qR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("q transpose: %w", err)
	}
	defer qT.Free()

	kT, err := l.backend.TransposeAxes(kR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("k transpose: %w", err)
	}
	defer kT.Free()

	vT, err := l.backend.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("v transpose: %w", err)
	}
	defer vT.Free()

	// RoPE
	qRot, err := llm.ApplyRoPEFast(qT, startPos, l.headDim, cfg.RopeTheta, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := llm.ApplyRoPEFast(kT, startPos, l.headDim, cfg.RopeTheta, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	// KV cache
	var kForAttn, vForAttn tensor.Array
	if cache != nil && cache.IsInitialized(layerIdx) {
		cached, err := cache.Get(layerIdx)
		if err != nil {
			return nil, err
		}
		newK, err := l.backend.ConcatenateAxis([]tensor.Array{cached.K, kRot}, 2, s)
		if err != nil {
			return nil, fmt.Errorf("concat K: %w", err)
		}
		newV, err := l.backend.ConcatenateAxis([]tensor.Array{cached.V, vT}, 2, s)
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
		kRetained := l.backend.RetainArray(kRot)
		vRetained := l.backend.RetainArray(vT)
		if err := cache.Store(layerIdx, kRetained, vRetained); err != nil {
			kRetained.Free()
			vRetained.Free()
			return nil, fmt.Errorf("cache store: %w", err)
		}
	} else {
		kForAttn = kRot
		vForAttn = vT
	}

	// Expand GQA KV heads
	kExp, err := llm.ExpandKVHeads(kForAttn, cfg.NumHeads, cfg.NumKVHeads, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k expand: %w", err)
	}
	defer kExp.Free()

	vExp, err := llm.ExpandKVHeads(vForAttn, cfg.NumHeads, cfg.NumKVHeads, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("v expand: %w", err)
	}
	defer vExp.Free()

	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	ctx, err := l.backend.FastScaledDotProductAttention(qRot, kExp, vExp, l.attnScale, maskMode, nil, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fused attention: %w", err)
	}
	defer ctx.Free()

	ctxT, err := l.backend.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := l.backend.Reshape(ctxT, []int{1, seqLen, cfg.NumHeads * l.headDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	return lw.oProj.Forward(ctxFlat, l.backend, s)
}

// shortConv runs the short-convolution layer.
//
//	in_proj(x) → split into B, C, x2
//	Bx = B * x2  (gated)
//	conv_out = Conv1d(pad_left(Bx))  (depthwise, kernel=3)
//	y = C * conv_out
//	out_proj(y)
func (l *LFM2) shortConv(h tensor.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := l.stream
	hidden := l.cfg.HiddenSize
	kernelSize := 3

	// in_proj: [B, L, hidden] → [B, L, 3*hidden]
	bcx, err := lw.inProj.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("in_proj: %w", err)
	}
	defer bcx.Free()

	// Split along last dim into B, C, x2 (each [B, L, hidden])
	// Slice [0:hidden], [hidden:2*hidden], [2*hidden:3*hidden]
	bSlice, err := l.backend.Slice(bcx, []int{0, 0, 0}, []int{1, seqLen, hidden}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("slice B: %w", err)
	}
	defer bSlice.Free()

	cSlice, err := l.backend.Slice(bcx, []int{0, 0, hidden}, []int{1, seqLen, 2 * hidden}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("slice C: %w", err)
	}
	defer cSlice.Free()

	xSlice, err := l.backend.Slice(bcx, []int{0, 0, 2 * hidden}, []int{1, seqLen, 3 * hidden}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("slice x: %w", err)
	}
	defer xSlice.Free()

	// Bx = B * x2 (gated)
	bx, err := l.backend.Multiply(bSlice, xSlice, s)
	if err != nil {
		return nil, fmt.Errorf("B*x: %w", err)
	}
	defer bx.Free()

	// Conv state management + padding
	var convInput tensor.Array
	if cache != nil {
		_, convState, err := cache.GetState(layerIdx)
		if err != nil {
			return nil, fmt.Errorf("get conv state: %w", err)
		}
		if convState != nil {
			// Prepend conv state: [1, kernel-1, hidden] + [1, L, hidden]
			convInput, err = l.backend.ConcatenateAxis([]tensor.Array{convState, bx}, 1, s)
			if err != nil {
				return nil, fmt.Errorf("concat conv state: %w", err)
			}
		} else {
			// First call — left-pad with zeros
			zeros, err := l.backend.Zeros([]int{1, kernelSize - 1, hidden}, tensor.Float32, s)
			if err != nil {
				return nil, fmt.Errorf("zeros: %w", err)
			}
			convInput, err = l.backend.ConcatenateAxis([]tensor.Array{zeros, bx}, 1, s)
			if err != nil {
				return nil, fmt.Errorf("concat zeros: %w", err)
			}
			zeros.Free()
		}

		// Update conv state: keep last (kernel-1) rows of convInput
		convInputShape := convInput.Shape()
		newState, err := l.backend.Slice(convInput,
			[]int{0, convInputShape[1] - (kernelSize - 1), 0},
			[]int{1, convInputShape[1], hidden},
			[]int{1, 1, 1}, s)
		if err != nil {
			return nil, fmt.Errorf("slice conv state: %w", err)
		}
		// Store updated state
		stateRetained := l.backend.RetainArray(newState)
		if err := cache.StoreState(layerIdx, nil, stateRetained); err != nil {
			stateRetained.Free()
			return nil, fmt.Errorf("store conv state: %w", err)
		}
		newState.Free()
	} else {
		// No cache — left-pad with zeros
		zeros, err := l.backend.Zeros([]int{1, kernelSize - 1, hidden}, tensor.Float32, s)
		if err != nil {
			return nil, fmt.Errorf("zeros: %w", err)
		}
		convInput, err = l.backend.ConcatenateAxis([]tensor.Array{zeros, bx}, 1, s)
		if err != nil {
			return nil, fmt.Errorf("concat zeros: %w", err)
		}
		zeros.Free()
	}
	defer convInput.Free()

	// Depthwise Conv1d: groups=hidden, weight=[hidden, kernel, 1]
	// padding=0 because we already left-padded convInput manually.
	convOut, err := l.backend.Conv1D(convInput, lw.convWeight, 1, 0, 1, hidden, s)
	if err != nil {
		return nil, fmt.Errorf("conv1d: %w", err)
	}
	defer convOut.Free()

	// Slice convOut back to original seqLen (remove padding from front)
	convOutShape := convOut.Shape()
	if convOutShape[1] > seqLen {
		convOut, err = l.backend.Slice(convOut,
			[]int{0, convOutShape[1] - seqLen, 0},
			[]int{1, convOutShape[1], hidden},
			[]int{1, 1, 1}, s)
		if err != nil {
			return nil, fmt.Errorf("slice conv out: %w", err)
		}
	}

	// y = C * conv_out (gating)
	y, err := l.backend.Multiply(cSlice, convOut, s)
	if err != nil {
		return nil, fmt.Errorf("C*conv: %w", err)
	}
	defer y.Free()

	// out_proj
	return lw.outProj.Forward(y, l.backend, s)
}

// mlp runs the SwiGLU feed-forward block.
func (l *LFM2) mlp(h tensor.Array, lw *layerWeights) (tensor.Array, error) {
	s := l.stream

	gate, err := lw.w1.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("ff w1: %w", err)
	}
	defer gate.Free()

	up, err := lw.w3.Forward(h, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("ff w3: %w", err)
	}
	defer up.Free()

	gateSilu, err := llm.SiLU(gate, l.backend, s)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gateSilu.Free()

	gated, err := l.backend.Multiply(gateSilu, up, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return lw.w2.Forward(gated, l.backend, s)
}
