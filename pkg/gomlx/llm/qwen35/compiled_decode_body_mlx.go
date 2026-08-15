//go:build darwin && arm64 && cgo

package qwen35

import (
	"fmt"
	"log"
	"math"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// compiledDecodeBody is the closure body: one full decode step over all
// layers with the cache state as explicit inputs/outputs. It runs exactly
// once (the trace); every later token replays the compiled graph. It must
// not read any array's concrete data — a forced evaluation inside the trace
// would bake placeholder values into the graph.
func (q *Qwen35) compiledDecodeBody(cd *compiledDecode, in []*mlx.Array) ([]*mlx.Array, error) {
	if debugCompiledDecode() {
		cd.traceCount++
		log.Printf("qwen35: compiled body execution #%d (replay means #1 only)", cd.traceCount)
	}
	s := q.stream
	b := q.backend

	ids := tensor.Array(in[0])
	pos := tensor.Array(in[1]) // [1] int32

	h, err := q.weights.embed.Lookup(ids, b, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	h, err = b.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze embedding: %w", err)
	}

	outputs := make([]*mlx.Array, 0, 2*q.cfg.NumLayers+1)
	stateIdx := 2
	for i := 0; i < q.cfg.NumLayers; i++ {
		out, upd, err := q.compiledLayer(h, i, pos, in[stateIdx:stateIdx+2], cd)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
		outputs = append(outputs, upd...)
		stateIdx += 2
	}

	logits, err := q.computeLogits(h)
	if err != nil {
		return nil, err
	}
	return append(outputs, logits.(*mlx.Array)), nil
}

// compiledLayer runs one decoder layer with explicit state I/O. stateIn is
// this layer's (K,V) or (State,ConvState) input pair; the returned slices
// are the updated pair in the same order. h is consumed: the caller frees
// the returned output when done chaining.
func (q *Qwen35) compiledLayer(h tensor.Array, layerIdx int, pos tensor.Array, stateIn []*mlx.Array, cd *compiledDecode) (tensor.Array, []*mlx.Array, error) {
	s := q.stream
	b := q.backend
	lw := &q.weights.layers[layerIdx]

	normed, err := q.rmsNormQwen35(h, lw.inputNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	var attnOut tensor.Array
	var updates []*mlx.Array
	if lw.linearAttn != nil {
		attnOut, updates, err = q.compiledDeltaLayer(normed, lw.linearAttn, stateIn)
		if err != nil {
			return nil, nil, fmt.Errorf("linear attention: %w", err)
		}
	} else {
		attnOut, updates, err = q.compiledFullAttentionLayer(normed, lw, pos, stateIn, cd)
		if err != nil {
			return nil, nil, fmt.Errorf("full attention: %w", err)
		}
	}
	defer attnOut.Free()

	residual1, err := b.Add(h, attnOut, s)
	if err != nil {
		return nil, nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	normed2, err := q.rmsNormQwen35(residual1, lw.postNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	var ffnOut tensor.Array
	if lw.moe != nil {
		ffnOut, err = lw.moe.forward(normed2, q)
	} else {
		ffnOut, err = q.swiglu(normed2, lw)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	out, err := b.Add(residual1, ffnOut, s)
	if err != nil {
		return nil, nil, fmt.Errorf("ffn residual: %w", err)
	}
	return out, updates, nil
}

// compiledFullAttentionLayer mirrors fullAttention's decode shape (seqLen=1)
// with the K/V buffer as an explicit input and the updated buffer as an
// explicit output. The new token's row is scattered in-graph at pos; the
// additive mask admits keys 0..pos and hides the zero padding beyond.
func (q *Qwen35) compiledFullAttentionLayer(h tensor.Array, lw *layerWeights, pos tensor.Array, stateIn []*mlx.Array, cd *compiledDecode) (tensor.Array, []*mlx.Array, error) {
	s := q.stream
	b := q.backend
	cfg := q.cfg
	sa := lw.selfAttn

	kBuf := tensor.Array(stateIn[0])
	vBuf := tensor.Array(stateIn[1])

	headDim := cfg.HeadDim
	outDim := cfg.NumHeads * headDim
	seqLen := 1
	kvCap := cd.capacity

	qFull, err := sa.qProj.Forward(h, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q proj: %w", err)
	}
	defer qFull.Free()

	qFull4, err := b.Reshape(qFull, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape4: %w", err)
	}
	defer qFull4.Free()

	q2d, err := b.Slice(qFull4, []int{0, 0, 0, 0}, []int{1, seqLen, cfg.NumHeads, headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q slice: %w", err)
	}
	defer q2d.Free()

	gate4, err := b.Slice(qFull4, []int{0, 0, 0, headDim}, []int{1, seqLen, cfg.NumHeads, 2 * headDim}, []int{1, 1, 1, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q gate slice: %w", err)
	}
	defer gate4.Free()
	qGate, err := b.Reshape(gate4, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q gate flatten: %w", err)
	}
	defer qGate.Free()

	k2d, err := sa.kProj.Forward(h, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k proj: %w", err)
	}
	defer k2d.Free()

	v2d, err := sa.vProj.Forward(h, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("v proj: %w", err)
	}
	defer v2d.Free()

	qR, err := b.Reshape(q2d, []int{1, seqLen, cfg.NumHeads, headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := b.Reshape(k2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k reshape: %w", err)
	}
	defer kR.Free()

	vR, err := b.Reshape(v2d, []int{1, seqLen, cfg.NumKVHeads, headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("v reshape: %w", err)
	}
	defer vR.Free()

	qNormed, err := q.rmsNormQwen35(qR, sa.qNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()

	kNormed, err := q.rmsNormQwen35(kR, sa.kNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("k norm: %w", err)
	}
	defer kNormed.Free()

	qT, err := b.TransposeAxes(qNormed, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q transpose: %w", err)
	}
	defer qT.Free()

	kT, err := b.TransposeAxes(kNormed, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k transpose: %w", err)
	}
	defer kT.Free()

	vT, err := b.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("v transpose: %w", err)
	}
	defer vT.Free()

	// Dynamic-offset RoPE — bit-identical to the eager static-offset path
	// (TestSpikeRoPEDynamicParity).
	ropeDims := int(float64(headDim) * float64(cfg.PartialRotaryFactor))
	qRot, err := mlx.FastRoPEDynamic(qT.(*mlx.Array), ropeDims, false, cfg.RopeTheta, 1.0, pos.(*mlx.Array), nil, cd.streamM)
	if err != nil {
		return nil, nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := mlx.FastRoPEDynamic(kT.(*mlx.Array), ropeDims, false, cfg.RopeTheta, 1.0, pos.(*mlx.Array), nil, cd.streamM)
	if err != nil {
		return nil, nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	// In-graph scatter: buf' = Where(arange(C) == pos, row, buf), shape
	// [1,Hkv,C,D] — constant. eq4 broadcasts the [C] predicate over heads
	// and head dim; the [1,Hkv,1,D] row broadcasts over the capacity axis,
	// writing exactly position pos. The updated buffers are the layer's
	// state outputs (single materialization; no separate host-side write).
	rg, err := b.Arange(0, float64(kvCap), 1, tensor.Int32, s)
	if err != nil {
		return nil, nil, fmt.Errorf("arange: %w", err)
	}
	defer rg.Free()
	eq, err := mlx.Equal(rg.(*mlx.Array), pos.(*mlx.Array), cd.streamM)
	if err != nil {
		return nil, nil, fmt.Errorf("equal: %w", err)
	}
	defer eq.Free()
	eq4, err := b.Reshape(eq, []int{1, 1, kvCap, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("reshape eq: %w", err)
	}
	defer eq4.Free()
	kUpd, err := b.Where(eq4, kRot, kBuf, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k scatter: %w", err)
	}
	vUpd, err := b.Where(eq4, vT, vBuf, s)
	if err != nil {
		kUpd.Free()
		return nil, nil, fmt.Errorf("v scatter: %w", err)
	}

	// Attention mask: positions <= pos valid (append-then-attend semantics:
	// the scattered row IS this token's key). Additive (0 / -inf) because
	// the fast-SDPA C++ path treats array masks as additive. Bitwise-
	// identical to exact-length unmasked SDPA
	// (TestSpikeSDPAPaddedBufferParity).
	le, err := mlx.LessEqual(rg.(*mlx.Array), pos.(*mlx.Array), cd.streamM)
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("less_equal: %w", err)
	}
	defer le.Free()
	zero, err := b.NewArrayFromFloat32([]float32{0}, []int{1})
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("zero: %w", err)
	}
	defer zero.Free()
	negInf, err := b.NewArrayFromFloat32([]float32{float32(math.Inf(-1))}, []int{1})
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("negInf: %w", err)
	}
	defer negInf.Free()
	maskVals, err := b.Where(le, zero, negInf, s)
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("where mask: %w", err)
	}
	defer maskVals.Free()
	// Cast the mask to q's dtype (bf16 for quantized models): an fp32 mask
	// would promote the scores to fp32 and round differently than the eager
	// path's bf16 attention — enough to flip near-tie argmax tokens
	// (matches ApplyCausalMask's own bf16 cast).
	maskCast, err := b.AsType(maskVals, qRot.Dtype(), s)
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("mask cast: %w", err)
	}
	defer maskCast.Free()
	mask4, err := b.Reshape(maskCast, []int{1, 1, 1, kvCap}, s)
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("reshape mask: %w", err)
	}
	defer mask4.Free()

	scale := float32(1.0 / math.Sqrt(float64(headDim)))
	ctx, err := b.FastScaledDotProductAttention(qRot, kUpd, vUpd, scale, "array", mask4, nil, s)
	if err != nil {
		kUpd.Free()
		vUpd.Free()
		return nil, nil, fmt.Errorf("fused attention: %w", err)
	}
	defer ctx.Free()

	ctxT, err := b.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := b.Reshape(ctxT, []int{1, seqLen, outDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	gateSig, err := b.Sigmoid(qGate, s)
	if err != nil {
		return nil, nil, fmt.Errorf("gate sigmoid: %w", err)
	}
	defer gateSig.Free()

	gated, err := b.Multiply(ctxFlat, gateSig, s)
	if err != nil {
		return nil, nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	out, err := sa.oProj.Forward(gated, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("o proj: %w", err)
	}
	return out, []*mlx.Array{kUpd.(*mlx.Array), vUpd.(*mlx.Array)}, nil
}
