//go:build darwin && arm64 && cgo

package qwen3

import (
	"fmt"
	"math"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func (q *Qwen3) attention(h tensor.Array, lw *layerWeights, layerIdx, seqLen, startPos int, cache *llm.KVCache) (tensor.Array, error) {
	s := q.stream
	cfg := q.cfg

	q2d, err := lw.qProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer q2d.Free()

	k2d, err := lw.kProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := lw.vProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	qR, err := q.backend.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := q.backend.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}

	vR, err := q.backend.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}

	qNormed, err := llm.RMSNorm(qR, lw.qNorm, cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR.Free()
	qR = qNormed

	kNormed, err := llm.RMSNorm(kR, lw.kNorm, cfg.RMSNormEPS, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	kR.Free()
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

	qRot, err := llm.ApplyRoPEFast(qT, startPos, cfg.HeadDim, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := llm.ApplyRoPEFast(kT, startPos, cfg.HeadDim, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	var kForAttn, vForAttn tensor.Array

	if cache != nil && cache.IsInitialized(layerIdx) {
		// AppendWindow writes one position into a geometrically grown
		// buffer instead of concatenating, so per-token cost does not
		// scale with sequence length. The views are caller-owned.
		kw, vw, err := cache.AppendWindow(layerIdx, q.backend.RetainArray(kRot), q.backend.RetainArray(vT))
		if err != nil {
			return nil, fmt.Errorf("cache append: %w", err)
		}
		defer kw.Free()
		defer vw.Free()
		kForAttn = kw
		vForAttn = vw
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

	// No ExpandKVHeads: MLX's SDPA handles GQA natively. Materializing the KV
	// heads would copy the whole cache (NumHeads/NumKVHeads)x per layer per
	// token, which dominates decode cost at long context.

	// Fused scaled dot-product attention: Q@K^T/scale + mask + softmax + @V
	// is a single Metal kernel via mlx_fast_scaled_dot_product_attention.
	// Mask mode: causal for prefill (seqLen>1), none for decode (single token
	// attending to the full cached prefix — the causal constraint is already
	// satisfied by construction).
	maskMode := ""
	if seqLen > 1 {
		maskMode = "causal"
	}
	scale := float32(1.0 / math.Sqrt(float64(cfg.HeadDim)))
	ctx, err := q.backend.FastScaledDotProductAttention(qRot, kForAttn, vForAttn, scale, maskMode, nil, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fused attention: %w", err)
	}
	defer ctx.Free()

	ctxT, err := q.backend.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := q.backend.Reshape(ctxT, []int{1, seqLen, cfg.NumHeads * cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	return lw.oProj.Forward(ctxFlat, q.backend, s)
}

// swiglu computes the MLP block: silu(h @ gate) * (h @ up) @ down.
// When the MLX compiled-graph path is available it runs the per-layer MLP as
// one compiled closure instead of ~5 eager kernel launches per token.
func (q *Qwen3) swiglu(h tensor.Array, lw *layerWeights, layerIdx int) (tensor.Array, error) {
	if useCompiledFFN() {
		if c := q.swigluClosure(layerIdx); c != nil {
			out, err := c.Apply([]*mlx.Array{h.(*mlx.Array)})
			if err != nil {
				return nil, fmt.Errorf("compiled ffn: %w", err)
			}
			if len(out) != 1 {
				for _, a := range out {
					a.Free()
				}
				return nil, fmt.Errorf("compiled ffn: expected 1 output, got %d", len(out))
			}
			return out[0], nil
		}
	}

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

// useCompiledFFN gates the compiled-graph MLP path. Measured on M1 Pro:
// per-op compiled MLP is ~26% SLOWER than eager (closure apply marshaling
// overhead exceeds fusion gains on a 5-op block dominated by 3 matmuls that
// cannot fuse). The full-layer compile that showed +17% in Python requires
// the functional preallocated-cache refactor, which is not yet wired. Keep
// the machinery available for experimentation via GO_COMPILED_FFN=1.
func useCompiledFFN() bool {
	return os.Getenv("GO_COMPILED_FFN") == "1"
}

// swigluClosure returns the compiled MLP closure for a layer, compiling it
// lazily on the inference thread when the per-call stream changes. Returns
// nil if compilation is unavailable (stub build) or fails — the eager path
// is always the fallback.
func (q *Qwen3) swigluClosure(layerIdx int) *mlx.Closure {
	if q.mlxSwigluStream != q.stream {
		for i, c := range q.mlxSwigluClosures {
			if c != nil {
				c.Free()
			}
			q.mlxSwigluClosures[i] = nil
		}
		q.mlxSwigluClosures = nil
		q.mlxSwigluStream = q.stream
	}
	if q.mlxSwigluClosures == nil {
		q.mlxSwigluClosures = make([]*mlx.Closure, q.cfg.NumLayers)
	}
	if q.mlxSwigluClosures[layerIdx] != nil {
		return q.mlxSwigluClosures[layerIdx]
	}

	s := q.stream
	lw := &q.weights.layers[layerIdx]
	fn := func(inputs []*mlx.Array) ([]*mlx.Array, error) {
		h := inputs[0]
		gate, err := lw.gateProj.Forward(h, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer gate.Free()
		up, err := lw.upProj.Forward(h, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer up.Free()
		gateSilu, err := llm.SiLU(gate, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer gateSilu.Free()
		gated, err := q.backend.Multiply(gateSilu, up, s)
		if err != nil {
			return nil, err
		}
		defer gated.Free()
		out, err := lw.downProj.Forward(gated, q.backend, s)
		if err != nil {
			return nil, err
		}
		return []*mlx.Array{out.(*mlx.Array)}, nil
	}

	plain, err := mlx.NewClosure(fn)
	if err != nil {
		return nil
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		plain.Free()
		return nil
	}
	// plain must stay registered: the first apply of compiled runs the
	// original body once on placeholder inputs to trace the graph. The
	// compiled closure owns a template ref and frees it when released.
	q.mlxSwigluClosures[layerIdx] = compiled
	return compiled
}
