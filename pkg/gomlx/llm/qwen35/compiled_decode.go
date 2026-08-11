//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// compiledDecode wraps the entire decode step (embed + 32 layers + logits +
// argmax) as a single MLX compiled closure. MLX fuses the elementwise ops
// (norms, silu, sigmoid, residuals) between matmuls into fewer Metal kernels,
// reducing per-token kernel launch overhead from ~320 to ~40.
//
// The closure takes cache arrays as inputs and returns updated arrays as
// outputs — no mutation of the KVCache during the compiled body. This is
// the "functional KV cache" pattern.
//
// Gate: SPROUT_COMPILED_DECODE=1

type compiledDecoder struct {
	closure *mlx.Closure
	// inputLayout tracks which closure inputs correspond to which cache arrays
	numFullAttnLayers int
	numDeltaNetLayers int
	numInputs         int // hidden + per-layer cache arrays
	numOutputs        int // logits + per-layer updated cache arrays
}

func useCompiledDecode() bool {
	return os.Getenv("SPROUT_COMPILED_DECODE") == "1"
}

// compileDecodeClosure builds and compiles the decode step closure.
// The closure captures all weight arrays from the Qwen35 struct.
// Only the token embedding and cache arrays are passed as inputs.
//
// Input layout (1 + numLayers*arraysPerLayer):
//
//	[0]: hidden state [1, 1, H] (embedding of current token)
//	For each full-attn layer: k_cache [1, numKVHeads, seq, headDim], v_cache [1, numKVHeads, seq, headDim]
//	For each DeltaNet layer: state [1, Hv, Dv, Dk], convState [1, convKernel-1, convDim]
//
// Output layout:
//
//	[0]: logits [1, 1, vocab] (for argmax)
//	For each full-attn layer: updated k_cache, v_cache (with new token appended)
//	For each DeltaNet layer: updated state, convState
func (q *Qwen35) compileDecodeClosure(cache *llm.KVCache) (*compiledDecoder, error) {
	cfg := q.cfg
	s := q.stream

	// Count inputs/outputs
	fullAttnLayers := 0
	deltaNetLayers := 0
	for i := 0; i < cfg.NumLayers; i++ {
		if (i+1)%cfg.FullAttentionInterval == 0 {
			fullAttnLayers++
		} else {
			deltaNetLayers++
		}
	}

	// inputs: 1 hidden + 2 per full-attn layer + 2 per DeltaNet layer
	numInputs := 1 + fullAttnLayers*2 + deltaNetLayers*2
	// outputs: 1 logits + same cache arrays
	numOutputs := 1 + fullAttnLayers*2 + deltaNetLayers*2

	// Build the closure body
	fn := func(inputs []*mlx.Array) ([]*mlx.Array, error) {
		// The first input is the hidden state
		h := tensor.Array(inputs[0])

		inputIdx := 1
		outputs := make([]*mlx.Array, 0, numOutputs)

		for i := 0; i < cfg.NumLayers; i++ {
			lw := &q.weights.layers[i]

			// input norm
			normed, err := q.rmsNormQwen35(h, lw.inputNorm)
			if err != nil {
				return nil, fmt.Errorf("layer %d input norm: %w", i, err)
			}

			// Forward the attention sub-layer (functional — no cache mutation)
			var layerOut tensor.Array
			if (i+1)%cfg.FullAttentionInterval == 0 {
				// Full attention: k_cache and v_cache are inputs
				kCache := tensor.Array(inputs[inputIdx])
				vCache := tensor.Array(inputs[inputIdx+1])

				var kNew, vNew tensor.Array
				layerOut, kNew, vNew, err = q.fullAttentionFunctional(normed, lw, i, 1, kCache, vCache)
				if err != nil {
					normed.Free()
					return nil, fmt.Errorf("layer %d attention: %w", i, err)
				}

				outputs = append(outputs, kNew.(*mlx.Array), vNew.(*mlx.Array))
				inputIdx += 2
			} else {
				// DeltaNet: state and convState are inputs
				state := tensor.Array(inputs[inputIdx])
				convState := tensor.Array(inputs[inputIdx+1])

				var stateNew, convStateNew tensor.Array
				layerOut, stateNew, convStateNew, err = q.deltaNetFunctional(normed, lw, i, 1, state, convState)
				if err != nil {
					normed.Free()
					return nil, fmt.Errorf("layer %d delta: %w", i, err)
				}

				outputs = append(outputs, stateNew.(*mlx.Array), convStateNew.(*mlx.Array))
				inputIdx += 2
			}
			normed.Free()

			// Residual add
			resid, err := q.backend.Add(h, layerOut, s)
			if err != nil {
				layerOut.Free()
				return nil, fmt.Errorf("layer %d residual: %w", i, err)
			}
			layerOut.Free()

			// Post norm
			postNormed, err := q.rmsNormQwen35(resid, lw.postNorm)
			if err != nil {
				resid.Free()
				return nil, fmt.Errorf("layer %d post norm: %w", i, err)
			}

			// MLP
			mlpOut, err := q.swiglu(postNormed, lw)
			if err != nil {
				postNormed.Free()
				resid.Free()
				return nil, fmt.Errorf("layer %d mlp: %w", i, err)
			}
			postNormed.Free()

			// Residual add
			h, err = q.backend.Add(resid, mlpOut, s)
			if err != nil {
				mlpOut.Free()
				resid.Free()
				return nil, fmt.Errorf("layer %d residual2: %w", i, err)
			}
			resid.Free()
			mlpOut.Free()
		}

		// Final norm + logits
		logits, err := q.computeLogits(h)
		if err != nil {
			return nil, fmt.Errorf("compute logits: %w", err)
		}
		outputs = append(outputs, logits.(*mlx.Array))

		return outputs, nil
	}

	plain, err := mlx.NewClosure(fn)
	if err != nil {
		return nil, fmt.Errorf("new closure: %w", err)
	}

	compiled, err := plain.Compile(true) // shapeless=true for growing K/V
	if err != nil {
		plain.Free()
		return nil, fmt.Errorf("compile: %w", err)
	}

	return &compiledDecoder{
		closure:           compiled,
		numFullAttnLayers: fullAttnLayers,
		numDeltaNetLayers: deltaNetLayers,
		numInputs:         numInputs,
		numOutputs:        numOutputs,
	}, nil
}

// fullAttentionFunctional is a pure version of fullAttention that takes K/V
// as inputs and returns updated K/V as outputs (no cache mutation).
func (q *Qwen35) fullAttentionFunctional(h tensor.Array, lw *layerWeights, layerIdx, seqLen int, kCache, vCache tensor.Array) (attnOut, kNew, vNew tensor.Array, err error) {
	s := q.stream
	cfg := q.cfg
	sa := lw.selfAttn
	headDim := cfg.HeadDim
	outDim := cfg.NumHeads * headDim

	// Q projection
	qFull, err := sa.qProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qFull.Free()

	qFull4, err := q.backend.Reshape(qFull, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qFull4.Free()

	q2d, err := q.backend.Slice(qFull4, []int{0, 0, 0, 0}, []int{1, seqLen, cfg.NumHeads, headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer q2d.Free()

	gate4, err := q.backend.Slice(qFull4, []int{0, 0, 0, headDim}, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer gate4.Free()
	qGate, err := q.backend.Reshape(gate4, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qGate.Free()

	// K/V projections
	k2d, err := sa.kProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer k2d.Free()
	v2d, err := sa.vProj.Forward(h, q.backend, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer v2d.Free()

	// Reshape for attention
	qR, err := q.backend.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, headDim}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qR.Free()
	kR, err := q.backend.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer kR.Free()
	vR, err := q.backend.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer vR.Free()

	// QK norms
	qNormed, err := q.rmsNormQwen35(qR, sa.qNorm)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qNormed.Free()
	kNormed, err := q.rmsNormQwen35(kR, sa.kNorm)
	if err != nil {
		return nil, nil, nil, err
	}
	defer kNormed.Free()

	// Transpose for attention
	qT, err := q.backend.TransposeAxes(qNormed, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qT.Free()
	kT, err := q.backend.TransposeAxes(kNormed, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer kT.Free()
	vT, err := q.backend.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer vT.Free()

	// RoPE
	ropeDims := int(float64(headDim) * float64(cfg.PartialRotaryFactor))
	qRot, err := llm.ApplyRoPEFast(qT, 0, ropeDims, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, nil, nil, err
	}
	defer qRot.Free()
	kRot, err := llm.ApplyRoPEFast(kT, 0, ropeDims, cfg.RopeTheta, q.backend, s)
	if err != nil {
		return nil, nil, nil, err
	}

	// Append to cache (functional: concat with input cache)
	kNew, err = q.backend.ConcatenateAxis([]tensor.Array{kCache, kRot}, 2, s)
	if err != nil {
		return nil, nil, nil, err
	}
	vNew, err = q.backend.ConcatenateAxis([]tensor.Array{vCache, vT}, 2, s)
	if err != nil {
		kNew.Free()
		return nil, nil, nil, err
	}

	// Expand K/V heads
	kExp, err := llm.ExpandKVHeads(kNew, cfg.NumHeads, cfg.NumKVHeads, q.backend, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer kExp.Free()
	vExp, err := llm.ExpandKVHeads(vNew, cfg.NumHeads, cfg.NumKVHeads, q.backend, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer vExp.Free()

	// SDPA
	scale := float32(1.0 / sqrt(float64(headDim)))
	ctx, err := q.backend.FastScaledDotProductAttention(qRot, kExp, vExp, scale, "", nil, nil, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer ctx.Free()

	// Output projection
	ctxT, err := q.backend.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer ctxT.Free()
	ctxFlat, err := q.backend.Reshape(ctxT, []int{1, seqLen, outDim}, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer ctxFlat.Free()

	gateSig, err := q.backend.Sigmoid(qGate, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer gateSig.Free()
	gated, err := q.backend.Multiply(ctxFlat, gateSig, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	defer gated.Free()

	out, err := sa.oProj.Forward(gated, q.backend, s)
	if err != nil {
		kNew.Free()
		vNew.Free()
		return nil, nil, nil, err
	}
	return out, kNew, vNew, nil
}

// deltaNetFunctional is a pure version of the DeltaNet forward that takes
// state/convState as inputs and returns updated values as outputs.
// For now, delegates to the existing eager code with cache read/write.
// TODO: make this truly functional when we verify the compiled closure works.
func (q *Qwen35) deltaNetFunctional(h tensor.Array, lw *layerWeights, layerIdx, seqLen int, state, convState tensor.Array) (attnOut, stateNew, convStateNew tensor.Array, err error) {
	// For the initial implementation, we'll skip compiled decode for DeltaNet
	// layers and just return the inputs unchanged. The compiled path will
	// only work for full-attention models (like Qwen3).
	// TODO: implement functional DeltaNet for Qwen3.5 compiled decode.
	return nil, state, convState, fmt.Errorf("compiled decode not yet supported for DeltaNet layers")
}

// forwardDecodeCompiled runs one decode step through the compiled closure.
// Gathers cache arrays from the KVCache, applies the closure, extracts argmax
// from the logits output, and updates the cache from the returned arrays.
func (q *Qwen35) forwardDecodeCompiled(tokenID int, pos int, cache *llm.KVCache) (int, error) {
	s := q.stream

	// Build the embedding for the current token
	idData := []int64{int64(tokenID)}
	idsArr, err := q.backend.NewArrayFromInt64(idData, []int{1, 1})
	if err != nil {
		return 0, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	h, err := q.weights.embed.Lookup(idsArr, q.backend, s)
	if err != nil {
		return 0, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, s)
	if err != nil {
		return 0, fmt.Errorf("squeeze embedding: %w", err)
	}

	// Gather cache inputs
	mlxInputs := []*mlx.Array{h.(*mlx.Array)}
	for i := 0; i < q.cfg.NumLayers; i++ {
		if (i+1)%q.cfg.FullAttentionInterval == 0 {
			// Full attention: pass K and V
			cached, err := cache.Get(i)
			if err != nil {
				return 0, err
			}
			if cached == nil || cached.K == nil {
				// First token — no cache yet. Create empty arrays.
				emptyK, err := q.backend.Zeros([]int{1, q.cfg.NumKVHeads, 0, q.cfg.HeadDim}, tensor.Float32, s)
				if err != nil {
					return 0, err
				}
				emptyV, _ := q.backend.Zeros([]int{1, q.cfg.NumKVHeads, 0, q.cfg.HeadDim}, tensor.Float32, s)
				mlxInputs = append(mlxInputs, emptyK.(*mlx.Array), emptyV.(*mlx.Array))
			} else {
				mlxInputs = append(mlxInputs, cached.K.(*mlx.Array), cached.V.(*mlx.Array))
			}
		}
		// DeltaNet layers not supported yet — the closure would have failed
		// at compile time, so we won't reach here.
	}

	// Apply the compiled closure
	outputs, err := q.compiledDecoder.closure.Apply(mlxInputs)
	if err != nil {
		return 0, fmt.Errorf("compiled decode apply: %w", err)
	}

	// Update the cache from outputs
	outIdx := 0
	for i := 0; i < q.cfg.NumLayers; i++ {
		if (i+1)%q.cfg.FullAttentionInterval == 0 {
			kNew := tensor.Array(outputs[outIdx])
			vNew := tensor.Array(outputs[outIdx+1])
			outIdx += 2

			// Retain copies for the cache (the closure's outputs are owned by the graph)
			kRetained := q.backend.RetainArray(kNew)
			vRetained := q.backend.RetainArray(vNew)
			if err := cache.Store(i, kRetained, vRetained); err != nil {
				return 0, fmt.Errorf("cache store layer %d: %w", i, err)
			}
		}
	}

	// Extract argmax from logits (last output)
	logits := tensor.Array(outputs[len(outputs)-1])
	defer logits.Free()
	return q.logitsToArgmax(logits)
}
