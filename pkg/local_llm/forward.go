//go:build darwin && arm64 && cgo && mlx

package local_llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// forwardPrefill runs the full forward pass over a prompt of seq_len tokens.
// Returns logits [1, seq_len, vocab_size]. No KV cache — this processes the
// entire sequence in one pass.
func (m *Model) forwardPrefill(ids *mlx.Array, seqLen int) (*mlx.Array, error) {
	s := m.stream

	// Embedding lookup: ids [1, seq] → gather from [vocab, hidden]
	// GatherAxis with axis=0, sliceSizes=[1, hidden] gives [1, seq, hidden]
	// after squeeze of the gathered dim.
	h, err := mlx.GatherAxis(m.weights.embedTokens, ids, 0, []int{1, m.cfg.HiddenSize}, s)
	if err != nil {
		return nil, fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()

	// GatherAxis produces [1, seq, 1, hidden] — squeeze the index dim (axis 2)
	h, err = mlx.SqueezeAxis(h, 2, s)
	if err != nil {
		return nil, fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < m.cfg.NumLayers; i++ {
		out, err := m.forwardLayer(h, i, seqLen, 0)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	normed, err := rmsNorm(h, m.weights.normWeight, m.cfg.RMSNormEPS, s)
	if err != nil {
		h.Free()
		return nil, fmt.Errorf("final norm: %w", err)
	}
	defer normed.Free()

	embedT, err := mlx.Transpose(m.weights.embedTokens, s)
	if err != nil {
		h.Free()
		return nil, fmt.Errorf("transpose embed: %w", err)
	}
	defer embedT.Free()

	logits, err := mlx.MatMul(normed, embedT, s)
	if err != nil {
		h.Free()
		return nil, fmt.Errorf("lm_head: %w", err)
	}
	return logits, nil
}

// forwardLayer runs one transformer layer (attention + FFN with residual connections).
// layerIdx selects the weights. seqLen is the total sequence length processed so far
// (including cached tokens). startPos is the offset where new tokens begin.
func (m *Model) forwardLayer(h *mlx.Array, layerIdx, seqLen, startPos int) (*mlx.Array, error) {
	s := m.stream
	lw := &m.weights.layers[layerIdx]

	// Pre-attention RMSNorm
	normed, err := rmsNorm(h, lw.inputNorm, m.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("input norm: %w", err)
	}
	defer normed.Free()

	// Self-attention with GQA
	attnOut, err := m.attention(normed, lw, seqLen, startPos)
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}
	defer attnOut.Free()

	// Residual connection
	residual1, err := mlx.Add(h, attnOut, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual1.Free()

	// Post-attention RMSNorm
	normed2, err := rmsNorm(residual1, lw.postNorm, m.cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("post norm: %w", err)
	}
	defer normed2.Free()

	// SwiGLU FFN
	ffnOut, err := m.swiglu(normed2, lw)
	if err != nil {
		return nil, fmt.Errorf("ffn: %w", err)
	}
	defer ffnOut.Free()

	// Residual connection
	return mlx.Add(residual1, ffnOut, s)
}

// attention computes grouped-query attention with QK norm and RoPE.
//
// Input: h [1, seq, hidden]
// Output: [1, seq, hidden]
//
// GQA: num_heads Q heads share num_kv_heads K/V heads via repeat.
func (m *Model) attention(h *mlx.Array, lw *layerWeights, seqLen, startPos int) (*mlx.Array, error) {
	s := m.stream
	cfg := m.cfg

	q, err := linearNoBias(h, lw.qProj, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer q.Free()

	k, err := linearNoBias(h, lw.kProj, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer k.Free()

	v, err := linearNoBias(h, lw.vProj, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v.Free()

	// Reshape Q/K/V to [1, seq, heads, head_dim]
	qR, err := mlx.Reshape(q, []int{1, seqLen, cfg.NumHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer qR.Free()

	kR, err := mlx.Reshape(k, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}
	defer kR.Free()

	vR, err := mlx.Reshape(v, []int{1, seqLen, cfg.NumKVHeads, cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}
	defer vR.Free()

	// QK Norm: apply RMSNorm to Q and K along head_dim (last axis)
	qNormed, err := rmsNorm(qR, lw.qNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("q norm: %w", err)
	}
	defer qNormed.Free()
	qR.Free()
	qR = qNormed

	kNormed, err := rmsNorm(kR, lw.kNorm, cfg.RMSNormEPS, s)
	if err != nil {
		return nil, fmt.Errorf("k norm: %w", err)
	}
	defer kNormed.Free()
	kR.Free()
	kR = kNormed

	// Apply RoPE to Q and K
	// Transpose to [1, heads, seq, head_dim] for RoPE application
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

	qRot, err := applyRoPE(qT, startPos, cfg.HeadDim, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("q rope: %w", err)
	}
	defer qRot.Free()

	kRot, err := applyRoPE(kT, startPos, cfg.HeadDim, cfg.RopeTheta, s)
	if err != nil {
		return nil, fmt.Errorf("k rope: %w", err)
	}
	defer kRot.Free()

	// Expand KV heads to match Q heads (GQA)
	// k/v: [1, num_kv_heads, seq, head_dim] → [1, num_heads, seq, head_dim]
	kExp, err := expandKVHeads(kRot, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("k expand: %w", err)
	}
	defer kExp.Free()

	vT, err := mlx.TransposeAxes(vR, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("v transpose: %w", err)
	}
	defer vT.Free()

	vExp, err := expandKVHeads(vT, cfg.NumHeads, cfg.NumKVHeads, s)
	if err != nil {
		return nil, fmt.Errorf("v expand: %w", err)
	}
	defer vExp.Free()

	// Attention scores: Q @ K^T / sqrt(head_dim)
	// [1, num_heads, seq, head_dim] @ [1, num_heads, head_dim, seq]
	kT2, err := mlx.TransposeAxes(kExp, []int{0, 1, 3, 2}, s)
	if err != nil {
		return nil, fmt.Errorf("k^T: %w", err)
	}
	defer kT2.Free()

	scores, err := mlx.MatMul(qRot, kT2, s)
	if err != nil {
		return nil, fmt.Errorf("scores: %w", err)
	}
	defer scores.Free()

	scale := float32(1.0 / math.Sqrt(float64(cfg.HeadDim)))
	scaleArr, err := mlx.NewArrayFromFloat32([]float32{scale}, []int{1})
	if err != nil {
		return nil, err
	}
	defer scaleArr.Free()

	scaled, err := mlx.Multiply(scores, scaleArr, s)
	if err != nil {
		return nil, fmt.Errorf("scale scores: %w", err)
	}
	defer scaled.Free()

	// Causal mask: add -inf to upper triangle
	masked, err := applyCausalMask(scaled, seqLen, startPos, s)
	if err != nil {
		return nil, fmt.Errorf("causal mask: %w", err)
	}
	defer masked.Free()

	// Softmax over keys (last axis)
	probs, err := mlx.SoftmaxAxis(masked, 3, s)
	if err != nil {
		return nil, fmt.Errorf("softmax: %w", err)
	}
	defer probs.Free()

	// Context: probs @ V → [1, num_heads, seq, head_dim]
	ctx, err := mlx.MatMul(probs, vExp, s)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	defer ctx.Free()

	// Transpose back to [1, seq, num_heads, head_dim] and reshape to [1, seq, hidden]
	ctxT, err := mlx.TransposeAxes(ctx, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctxT.Free()

	ctxFlat, err := mlx.Reshape(ctxT, []int{1, seqLen, cfg.NumHeads * cfg.HeadDim}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape: %w", err)
	}
	defer ctxFlat.Free()

	return linearNoBias(ctxFlat, lw.oProj, s)
}

// swiglu computes the SwiGLU FFN: down(silu(gate(x)) * up(x))
func (m *Model) swiglu(h *mlx.Array, lw *layerWeights) (*mlx.Array, error) {
	s := m.stream

	gate, err := linearNoBias(h, lw.gateProj, s)
	if err != nil {
		return nil, fmt.Errorf("gate proj: %w", err)
	}
	defer gate.Free()

	up, err := linearNoBias(h, lw.upProj, s)
	if err != nil {
		return nil, fmt.Errorf("up proj: %w", err)
	}
	defer up.Free()

	// SiLU(gate) = gate * sigmoid(gate)
	gateSilu, err := silu(gate, s)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gateSilu.Free()

	gated, err := mlx.Multiply(gateSilu, up, s)
	if err != nil {
		return nil, fmt.Errorf("gate multiply: %w", err)
	}
	defer gated.Free()

	return linearNoBias(gated, lw.downProj, s)
}

